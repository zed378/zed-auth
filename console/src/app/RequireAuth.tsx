import type { ReactNode } from "react";

import { useAuth } from "../lib/auth/AuthProvider";
import { SignInPage } from "../pages/SignInPage";

/**
 * A route that needs a session, and optionally a role (P1-21).
 *
 * **This is a user-experience feature, not a control.** `docs/UI-UX/08`
 * § Cross-Screen Requirements asks that a route the user's claims do not
 * permit be genuinely unreachable rather than merely hidden — which this
 * does — and `docs/PLAN/08` is equally clear that the API enforces every
 * permission independently. Somebody who edits their token, or the guard, or
 * simply calls the API directly gets refused by the server. What they do not
 * get is a console screen that renders half a page and then fails.
 */
export function RequireAuth({
  children,
  roles,
}: {
  children: ReactNode;
  /** Any one of these roles is enough. Omitted means a session is enough. */
  roles?: string[];
}) {
  const { status, error, hasRole } = useAuth();

  if (status === "restoring") {
    // The token lives only in memory (ADR-019), so a reload always passes
    // through here. Saying so beats a blank screen (docs/UI-UX/14).
    return (
      <main className="card" aria-busy="true">
        <h1>Loading</h1>
        <p>Checking your session.</p>
      </main>
    );
  }

  if (status === "failed") {
    return (
      <main className="card">
        <h1>Sign-in is unavailable</h1>
        <p>
          The console could not establish a session. This is a configuration or service problem
          rather than something you did.
        </p>
        {error !== null ? <p className="err">{error.code}</p> : null}
      </main>
    );
  }

  if (status === "anonymous") {
    return <SignInPage />;
  }

  if (roles !== undefined && !hasRole(...roles)) {
    // Not a redirect and not a 404. The user is signed in and this page exists;
    // saying so plainly is more useful than pretending it does not, and the
    // API would refuse the data anyway.
    return (
      <main className="card">
        <h1>You do not have access to this</h1>
        <p>Your account does not carry the role this screen needs.</p>
      </main>
    );
  }

  return <>{children}</>;
}
