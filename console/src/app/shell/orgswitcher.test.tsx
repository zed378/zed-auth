import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { OrgSwitcher } from "./OrgSwitcher";
import { AuthError } from "../../lib/auth/oidc";
import { AuthProvider } from "../../lib/auth/AuthProvider";
import { clearToken } from "../../lib/auth/tokens";
import { OrgProvider } from "../../lib/org/OrgProvider";
import { useOrgId } from "../../lib/api/queries";
import { expectNoAxeViolations } from "../../test/axe";
import { signIn, stubApi } from "../../test/harness";

/**
 * The organization switcher (P2-13).
 *
 * Two of these tests are the reason the task exists rather than being a
 * dropdown: **switching must re-scope every subsequent request**, and
 * **switching must clear what was cached for the previous organization**.
 * Stale cross-organization data in the UI is a leak even when the API was
 * correct, and it is invisible until it is in a screenshot.
 */

vi.mock("../../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../../lib/auth/oidc")>("../../lib/auth/oidc");
  return {
    ...actual,
    renewSilently: () => Promise.reject(new AuthError("login_required", null)),
  };
});

const HOME = "org-1";
const OTHER = "org-2";

const administered = [
  { id: HOME, name: "Acme Corporation", roles: ["ORG_ADMIN"] },
  { id: OTHER, name: "Beta Industries", roles: ["ORG_OWNER"] },
];

/** Renders the switcher with the org context, and exposes the active org id. */
function renderSwitcher(entries = ["/users"]) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });

  const result = render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={entries}>
        <AuthProvider>
          <OrgProvider>
            <OrgSwitcher />
            <Routes>
              <Route path="*" element={<Probe />} />
            </Routes>
          </OrgProvider>
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );

  return { ...result, queryClient };
}

/** Reports what the rest of the console would scope its requests to. */
function Probe() {
  const orgId = useOrgId();
  const location = useLocation();
  return (
    <>
      <p data-testid="active-org">{orgId ?? "none"}</p>
      <p data-testid="url">{location.pathname + location.search}</p>
    </>
  );
}

beforeEach(() => {
  clearToken();
  sessionStorage.clear();
  vi.stubEnv("VITE_AUTH_CLIENT_ID", "console-test-client");
  vi.stubEnv("VITE_AUTH_ISSUER", "https://auth.example.test");
  signIn();
});

describe("when the caller administers one organization", () => {
  it("shows its name without offering a switch", async () => {
    stubApi((url) =>
      url.includes("/me/organizations")
        ? { status: 200, body: { organizations: [administered[0]] } }
        : { status: 200, body: { id: HOME, name: "Acme Corporation" } },
    );

    renderSwitcher();

    // Step 4: the active organization is unmistakable at all times, whether or
    // not there is anywhere to switch to.
    expect(await screen.findByText("Acme Corporation")).toBeInTheDocument();
    // Step 1: no switcher until there is more than one. A dropdown with a
    // single entry is a control that teaches people to ignore it.
    expect(screen.queryByRole("button", { name: /acme/i })).not.toBeInTheDocument();
  });
});

describe("when the caller administers more than one", () => {
  beforeEach(() => {
    stubApi((url) =>
      url.includes("/me/organizations")
        ? { status: 200, body: { organizations: administered } }
        : { status: 200, body: { id: HOME, name: "Acme Corporation" } },
    );
  });

  it("offers exactly the organizations the server named", async () => {
    renderSwitcher();

    await userEvent.click(await screen.findByRole("button", { name: /acme/i }));

    const list = within(screen.getByRole("list"));
    expect(list.getByRole("button", { name: /acme/i })).toBeInTheDocument();
    expect(list.getByRole("button", { name: /beta/i })).toBeInTheDocument();
    expect(list.getAllByRole("button")).toHaveLength(2);
  });

  it("says what the caller is in each, not merely that they may switch", async () => {
    renderSwitcher();
    await userEvent.click(await screen.findByRole("button", { name: /acme/i }));

    const beta = within(screen.getByRole("list")).getByRole("button", { name: /beta/i });
    // An administrator moving between an organization they own and one they
    // merely administer should see which is which before they arrive.
    expect(beta).toHaveTextContent(/org owner/i);
  });

  it("re-scopes every subsequent request", async () => {
    renderSwitcher();

    expect(screen.getByTestId("active-org")).toHaveTextContent(HOME);

    await userEvent.click(await screen.findByRole("button", { name: /acme/i }));
    await userEvent.click(within(screen.getByRole("list")).getByRole("button", { name: /beta/i }));

    // Step 3, the half that every screen depends on: `useOrgId` is what every
    // query key is built from, so this is every read at once.
    expect(screen.getByTestId("active-org")).toHaveTextContent(OTHER);
  });

  it("clears what was cached for the previous organization", async () => {
    const { queryClient } = renderSwitcher();

    queryClient.setQueryData(["projects", HOME], [{ id: "p1", name: "Secret Project" }]);
    expect(queryClient.getQueryData(["projects", HOME])).toBeDefined();

    await userEvent.click(await screen.findByRole("button", { name: /acme/i }));
    await userEvent.click(within(screen.getByRole("list")).getByRole("button", { name: /beta/i }));

    // Query keys are scoped per organization, so the previous tenant's rows
    // could not be SERVED under the new one's key. Clearing anyway is what
    // makes that a property of the cache rather than of every hook author
    // remembering (PF-20).
    expect(queryClient.getQueryData(["projects", HOME])).toBeUndefined();
  });

  it("puts the context in the URL so a link and a refresh land in the same place", async () => {
    renderSwitcher();

    await userEvent.click(await screen.findByRole("button", { name: /acme/i }));
    await userEvent.click(within(screen.getByRole("list")).getByRole("button", { name: /beta/i }));

    // Step 5. Without this, "send me that page" becomes "send me that page and
    // also click the switcher first".
    expect(screen.getByTestId("url")).toHaveTextContent(`org=${OTHER}`);
  });

  it("starts in the organization a pasted URL names", async () => {
    renderSwitcher([`/users?org=${OTHER}`]);

    expect(screen.getByTestId("active-org")).toHaveTextContent(OTHER);
    expect(await screen.findByText("Beta Industries")).toBeInTheDocument();
  });

  it("says plainly when the caller is not in their own organization", async () => {
    renderSwitcher([`/users?org=${OTHER}`]);

    // Step 4's severe case: a destructive action performed in the wrong
    // organization. A sentence, not a colour — and not `color-danger`, which
    // is reserved for destructive actions themselves.
    expect(await screen.findByRole("status")).toHaveTextContent(
      /acting in another organization, not your own/i,
    );
  });

  it("leaves the URL clean when acting in the caller's own organization", async () => {
    renderSwitcher([`/users?org=${OTHER}`]);

    await userEvent.click(await screen.findByRole("button", { name: /beta/i }));
    await userEvent.click(within(screen.getByRole("list")).getByRole("button", { name: /acme/i }));

    // A parameter that is always present is one nobody reads.
    expect(screen.getByTestId("url")).not.toHaveTextContent("org=");
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("filters the list, and says so when nothing matches", async () => {
    renderSwitcher();

    await userEvent.click(await screen.findByRole("button", { name: /acme/i }));
    await userEvent.type(screen.getByLabelText(/filter organizations/i), "beta");
    expect(within(screen.getByRole("list")).getAllByRole("button")).toHaveLength(1);

    await userEvent.clear(screen.getByLabelText(/filter organizations/i));
    await userEvent.type(screen.getByLabelText(/filter organizations/i), "zzz");
    expect(within(screen.getByRole("list")).queryAllByRole("button")).toHaveLength(0);
    expect(screen.getByText(/None of your organizations match/i)).toBeInTheDocument();
  });

  it("closes on Escape without switching", async () => {
    renderSwitcher();

    await userEvent.click(await screen.findByRole("button", { name: /acme/i }));
    expect(screen.getByRole("list")).toBeInTheDocument();

    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("list")).not.toBeInTheDocument();
    expect(screen.getByTestId("active-org")).toHaveTextContent(HOME);
  });

  it("has no axe violations, closed and open", async () => {
    const { container } = renderSwitcher();
    await screen.findByRole("button", { name: /acme/i });
    await expectNoAxeViolations(container);

    await userEvent.click(screen.getByRole("button", { name: /acme/i }));
    await expectNoAxeViolations(container);
  });
});

describe("when the list cannot be read", () => {
  it("still names the active organization rather than going blank", async () => {
    stubApi((url) =>
      url.includes("/me/organizations")
        ? { status: 500, body: { error: { code: "SERVER_ERROR", message: "no" } } }
        : { status: 200, body: { id: HOME, name: "Acme Corporation" } },
    );

    renderSwitcher();

    // The switcher failing is not a reason to stop saying where the
    // administrator is — which is the more important of the two jobs.
    expect(await screen.findByText("Acme Corporation")).toBeInTheDocument();
  });
});
