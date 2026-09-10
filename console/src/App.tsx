import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter } from "react-router-dom";

import { ErrorBoundary } from "./app/ErrorBoundary";
import { AuthProvider } from "./lib/auth/AuthProvider";
import { AppRoutes } from "./app/routes";
import { AppShell } from "./app/shell/AppShell";

/**
 * TanStack Query holds all cached server data.
 *
 * docs/PLAN/06 § Tech Stack is explicit that most console data is cached server
 * data rather than client state, so there is no global store for it. That is
 * not a preference: a Redux-style store holding API responses means every
 * screen owns a copy of the truth and someone has to remember to invalidate
 * them all after a role change. The consequence of missing one here is a
 * console showing a permission the user no longer has.
 */
const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // Authorization data goes stale in a way that matters. A role revoked in
      // another tab should not linger behind a long cache window, so the
      // default is short and screens that can afford more say so explicitly.
      staleTime: 30_000,

      // Refetching on every window focus is noisy for an admin who alt-tabs
      // between the console and a ticket. Reconnect is kept: a resumed laptop
      // showing pre-suspend permissions is worse.
      refetchOnWindowFocus: false,
      refetchOnReconnect: true,

      // A 403 does not become a 200 on the third attempt, and retrying it
      // three times is three audit events for one mistake.
      retry: (failureCount, error) => {
        const status = (error as { status?: number } | null)?.status;
        if (status !== undefined && status >= 400 && status < 500) return false;
        return failureCount < 2;
      },
    },
  },
});

export function App() {
  return (
    <ErrorBoundary>
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>
          {/*
            The provider sits inside the router because logging in needs to know
            where the user was going, and outside the shell because the shell
            renders the signed-in user's name.
          */}
          <AuthProvider>
            <AppShell>
              <AppRoutes />
            </AppShell>
          </AuthProvider>
        </BrowserRouter>
      </QueryClientProvider>
    </ErrorBoundary>
  );
}
