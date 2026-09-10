import { useAuth } from "../../lib/auth/AuthProvider";

/**
 * Who is signed in, and the way out (P1-21).
 *
 * Rendered only when there is a session. An anonymous visitor sees the
 * sign-in screen in the main region, and a "sign out" control beside it would
 * be an affordance for something that has not happened.
 */
export function SessionBar() {
  const { status, claims, logout } = useAuth();

  if (status !== "authenticated" || claims === null) return null;

  return (
    <div className="flex items-center justify-end gap-3 border-b border-border px-5 py-2 text-small desktop:px-6">
      {/*
        The subject, not a display name. The console has one at this point in
        Phase 1 only if it asks the API for it, and an access token carrying a
        name would be a token carrying more than it needs — a decision about
        what a stolen token discloses, made for the sake of a nicer header.
        P1-23 renders the real profile from the users API.
      */}
      <span className="text-text-secondary">
        Signed in as <span className="text-text-primary">{claims.subject}</span>
      </span>

      <button
        type="button"
        onClick={logout}
        className="rounded border border-border px-2 py-1 text-text-primary"
      >
        Sign out
      </button>
    </div>
  );
}
