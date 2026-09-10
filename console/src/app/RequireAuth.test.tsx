import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { AppRoutes } from "./routes";
import { RequireAuth } from "./RequireAuth";
import { AuthProvider } from "../lib/auth/AuthProvider";
import { AuthError } from "../lib/auth/oidc";
import { clearToken, storeToken } from "../lib/auth/tokens";

/**
 * The silent renewal is stubbed, and only the silent renewal.
 *
 * It opens a hidden iframe against a real issuer and waits ten seconds for a
 * postMessage that jsdom will never deliver. Leaving it real would make every
 * test here a ten-second wait ending in the same answer — and would test the
 * iframe rather than the guard, which is what these tests are about.
 *
 * `renewalOutcome` lets one test assert the thing that matters about renewal
 * at this layer: that a failure degrades to a sign-in prompt rather than to a
 * blank screen (P1-21 DoD item 4).
 */
let renewalOutcome: () => Promise<void> = () =>
  Promise.reject(new AuthError("login_required", null));

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return {
    ...actual,
    renewSilently: () => renewalOutcome(),
  };
});

/**
 * The route guard (P1-21 DoD item 5).
 *
 * The claim is that a route the user's claims do not permit is **genuinely
 * unreachable by typing its URL**, not merely missing from the navigation. So
 * these tests navigate directly to the URL — hiding a nav item would pass a
 * test that clicked the nav item and prove nothing.
 *
 * None of this is a security control, and the tests say so where it matters:
 * the API enforces every permission independently (docs/PLAN/08). What this
 * prevents is a console that renders half a screen and then fails.
 */

const encode = (payload: Record<string, unknown>) =>
  `header.${btoa(JSON.stringify(payload)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "")}.signature`;

function signIn(roles: string[]) {
  storeToken(encode({ sub: "user-1", org_id: "org-1", roles, exp: 2_000_000_000 }), 3600);
}

/**
 * A fresh client per render, with retries off.
 *
 * Retries would make a failing query take three attempts before the component
 * settles, so a test asserting an error state would wait for a timeout rather
 * than an assertion — and a shared client would leak one test's cache into the
 * next, which is how a guard test starts passing because of what ran before it.
 */
function renderAt(path: string) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[path]}>
        <AuthProvider>
          <AppRoutes />
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  clearToken();
  sessionStorage.clear();

  // The console refuses to start without knowing which application it is, and
  // that refusal is deliberate (a console that cannot name itself cannot log
  // in). Supplying it here so these tests exercise the guard rather than the
  // configuration check, which has its own test below.
  vi.stubEnv("VITE_AUTH_CLIENT_ID", "console-test-client");
  vi.stubEnv("VITE_AUTH_ISSUER", "https://auth.example.test");

  // No session, unless a test says otherwise.
  renewalOutcome = () => Promise.reject(new AuthError("login_required", null));
});

describe("a route that needs a role", () => {
  it("is unreachable by typing its URL without that role", async () => {
    signIn(["PROJECT_OWNER"]); // reserved by P1-17 and granting nothing yet

    renderAt("/audit-log");

    expect(await screen.findByText(/do not have access/i)).toBeInTheDocument();
    // And the screen itself is not rendered behind the message.
    expect(screen.queryByText(/Audit Log/i)).not.toBeInTheDocument();
  });

  it("renders for a caller who has it", async () => {
    signIn(["ORG_ADMIN"]);

    renderAt("/audit-log");

    expect(await screen.findByText(/Audit Log/i)).toBeInTheDocument();
  });

  it("accepts any one of the roles a route lists", async () => {
    signIn(["INSTANCE_OWNER"]);

    renderAt("/policies");

    expect(await screen.findByText(/Policies/i)).toBeInTheDocument();
  });
});

describe("a route that needs only a session", () => {
  it("offers sign-in to an anonymous visitor rather than a blank screen", async () => {
    renderAt("/settings");

    expect(await screen.findByRole("heading", { name: /sign in/i })).toBeInTheDocument();
  });

  it("shows no password field, because the console has no credential form", async () => {
    renderAt("/settings");

    await screen.findByRole("heading", { name: /sign in/i });
    // docs/PLAN/02 § Constraints: the console logs in through the same hosted
    // page as every other client. A field here would be the backdoor that
    // constraint forbids.
    expect(document.querySelector('input[type="password"]')).toBeNull();
  });
});

describe("the two /auth routes", () => {
  it("are outside the guard, or establishing a session would be a redirect loop", async () => {
    // No token at all. The callback must still render — it is how a token is
    // obtained in the first place.
    renderAt("/auth/callback?error=access_denied&state=x");

    expect(await screen.findByText(/did not complete/i)).toBeInTheDocument();
  });
});

describe("a guard with no roles named", () => {
  it("passes any signed-in caller through", async () => {
    signIn([]);

    render(
      <MemoryRouter initialEntries={["/anything"]}>
        <AuthProvider>
          <Routes>
            <Route
              path="/anything"
              element={
                <RequireAuth>
                  <p>the protected content</p>
                </RequireAuth>
              }
            />
          </Routes>
        </AuthProvider>
      </MemoryRouter>,
    );

    expect(await screen.findByText("the protected content")).toBeInTheDocument();
  });
});

describe("an unconfigured console", () => {
  it("says so rather than looping through a login it cannot start", async () => {
    vi.stubEnv("VITE_AUTH_CLIENT_ID", "");

    renderAt("/settings");

    expect(await screen.findByText(/Sign-in is unavailable/i)).toBeInTheDocument();
    expect(await screen.findByText("not_configured")).toBeInTheDocument();
  });
});

describe("a silent renewal that fails", () => {
  it("degrades to a sign-in prompt rather than a blank screen", async () => {
    renewalOutcome = () => Promise.reject(new AuthError("login_required", null));

    renderAt("/settings");

    expect(await screen.findByRole("heading", { name: /sign in/i })).toBeInTheDocument();
  });

  it("says something is wrong when the failure is not just a missing session", async () => {
    // A network failure, a misconfigured issuer, a broken token endpoint.
    // Telling the user to sign in again would send them round a loop that
    // cannot succeed.
    renewalOutcome = () => Promise.reject(new AuthError("renewal_failed", "the network is down"));

    renderAt("/settings");

    expect(await screen.findByText(/Sign-in is unavailable/i)).toBeInTheDocument();
  });
});
