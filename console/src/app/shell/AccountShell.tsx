import type { ReactNode } from "react";
import { Link } from "react-router-dom";

import { SkipLink } from "./SkipLink";
import { useAuth } from "../../lib/auth/AuthProvider";

/**
 * The shell for personal account settings (P3-12).
 *
 * **Outside the narrow-screen guard.** `UnsupportedWidth` says as much: the plan's
 * one mobile screen renders outside that wrapper rather than adding a condition
 * inside it. No side navigation — on a phone it would push the content off the
 * screen, and the only thing a person here needs besides their account is a way
 * back and a way out.
 *
 * The same three landmarks as the console shell, each exactly once.
 */
export function AccountShell({ children }: { children: ReactNode }) {
  const { status, logout } = useAuth();

  return (
    <>
      <SkipLink />
      <div className="flex min-h-screen flex-col bg-bg-base">
        <nav
          aria-label="Account"
          className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-4 py-3 tablet:px-6"
        >
          <Link to="/" className="px-2 py-2 text-body text-text-primary underline">
            Back to the console
          </Link>
          {status === "authenticated" ? (
            <button
              type="button"
              onClick={logout}
              className="rounded border border-border px-3 py-2 text-body text-text-primary"
            >
              Sign out
            </button>
          ) : null}
        </nav>

        <main id="main-content" tabIndex={-1} className="min-w-0 flex-1 px-4 py-6 tablet:px-6">
          {children}
        </main>

        <footer className="border-t border-border px-4 py-3 text-small text-text-secondary tablet:px-6">
          Zed Auth Console
        </footer>
      </div>
    </>
  );
}
