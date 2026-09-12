import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { vi } from "vitest";

import { AuthProvider } from "../lib/auth/AuthProvider";
import { storeToken } from "../lib/auth/tokens";

/**
 * The screen-test harness, in one place.
 *
 * Extracted when `P2-11` added a second screen suite. Two copies of
 * `stubApi` is two definitions of what "the API" means in a test, and the one
 * that gets fixed is whichever file the next person opens — the bug in
 * `screens.test.tsx` where `String(request)` silently matched no branch and
 * answered every call with the success body would have had to be found twice.
 *
 * The `vi.mock` of `../lib/auth/oidc` cannot live here: `vi.mock` is hoisted
 * to the top of the file that calls it and does not apply transitively. Each
 * suite declares its own, which is three lines and visible where it matters.
 */

const encode = (payload: Record<string, unknown>) =>
  `header.${btoa(JSON.stringify(payload))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "")}.signature`;

/** Puts a token in storage so `AuthProvider` treats the session as established. */
export function signIn(roles = ["ORG_ADMIN"]): void {
  storeToken(encode({ sub: "admin-1", org_id: "org-1", roles, exp: 2_000_000_000 }), 3600);
}

/**
 * Answers every API call with one canned body, or a failure.
 *
 * Stubbed at `fetch`, not at the query layer. That keeps the generated client,
 * the bearer middleware and the envelope handling in the path — a test that
 * mocks `useRoles` proves the component renders an array, which is not the
 * thing that breaks.
 */
export function stubApi(
  handler: (url: string, init: { method: string }) => { status: number; body: unknown },
): void {
  vi.stubGlobal("fetch", (input: RequestInfo | URL) => {
    // A Request, not a string: the client hands `fetch` a Request object, and
    // `String(request)` is "[object Request]" — which silently matched no
    // branch of any handler and answered every call with the success body.
    const url =
      typeof input === "string" ? input : input instanceof Request ? input.url : input.toString();
    const method = input instanceof Request ? input.method : "GET";

    const { status, body } = handler(url, { method });
    return Promise.resolve(
      new Response(status === 204 ? null : JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      }),
    );
  });
}

/**
 * `routePath` matters for any screen that reads `useParams`.
 *
 * Rendering the component directly under a MemoryRouter leaves params empty,
 * so a detail screen's query stays disabled and the test measures a permanent
 * loading state rather than the screen.
 */
export function renderScreen(ui: React.ReactElement, path = "/", routePath = "*") {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[path]}>
        <AuthProvider>
          <Routes>
            <Route path={routePath} element={ui} />
          </Routes>
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
