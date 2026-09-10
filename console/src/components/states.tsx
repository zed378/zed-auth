import type { ReactNode } from "react";

import { Button } from "./Button";

/**
 * The four states every screen has, per `docs/UI-UX/14-EMPTY-LOADING-ERROR-STATES.md`.
 *
 * They live together because the document's central point is that they are a
 * SET: a screen that has designed its loading state and improvised its empty
 * one is the failure mode it exists to prevent. Importing from one file makes
 * "which of these did I not think about" answerable by looking.
 */

/**
 * A skeleton that matches the geometry of what is coming.
 *
 * Matching matters: a spinner in the middle of an empty page, followed by
 * content, is two layout shifts. A skeleton the same shape as the result is
 * none, and it tells the user what kind of thing is arriving
 * (`docs/UI-UX/09`, `docs/UI-UX/14`).
 */
export function Skeleton({ className = "" }: { className?: string }) {
  return (
    <span aria-hidden="true" className={`block animate-pulse rounded bg-bg-base ${className}`} />
  );
}

/**
 * A failure, with what to do about it.
 *
 * **The three kinds are distinguished**, because `docs/UI-UX/14` is explicit
 * that a validation error, a server error and a network error are different
 * events with different recoveries — and telling a user to "try again" when
 * their input is wrong is advice that cannot work.
 */
export function ErrorState({
  kind,
  detail,
  onRetry,
}: {
  kind: "network" | "server" | "permission" | "validation";
  detail?: string;
  onRetry?: () => void;
}) {
  const copy = {
    network: {
      title: "Could not reach the service",
      body: "Check your connection. Nothing was changed.",
      retryable: true,
    },
    server: {
      title: "Something went wrong",
      body: "The service could not complete that. Trying again is safe.",
      retryable: true,
    },
    permission: {
      title: "You do not have access to this",
      body: "Your account does not carry the role this needs.",
      // No retry. The same request will be refused again, and offering the
      // button suggests otherwise.
      retryable: false,
    },
    validation: {
      title: "That could not be saved",
      body: "Check the values below and try again.",
      retryable: false,
    },
  }[kind];

  return (
    <div role="alert" className="rounded border border-border bg-bg-surface p-5">
      <h2 className="text-heading-3 font-medium text-text-primary">{copy.title}</h2>
      <p className="mt-1 max-w-prose text-body text-text-secondary">{copy.body}</p>
      {detail !== undefined ? (
        <p className="mt-2 text-small text-text-secondary">{detail}</p>
      ) : null}
      {copy.retryable && onRetry !== undefined ? (
        <p className="mt-3">
          <Button onClick={onRetry}>Try again</Button>
        </p>
      ) : null}
    </div>
  );
}

/**
 * Nothing here — and **which kind of nothing**.
 *
 * `docs/UI-UX/14` asks for "genuinely empty" and "filtered to empty" to be
 * different, and the reason is the recovery: one wants a create action, the
 * other wants the filter cleared. Showing "create your first project" to
 * somebody who has twelve and typed a typo is the failure this prevents.
 */
export function EmptyState({
  filtered,
  what,
  onClearFilter,
  action,
}: {
  filtered: boolean;
  /** Plural noun, lowercase: "projects", "applications", "users". */
  what: string;
  onClearFilter?: () => void;
  action?: ReactNode;
}) {
  if (filtered) {
    return (
      <div className="rounded border border-border bg-bg-surface p-5 text-center">
        <h2 className="text-heading-3 font-medium text-text-primary">
          No {what} match that search
        </h2>
        <p className="mt-1 text-body text-text-secondary">
          There are {what} here — none of them match what you typed.
        </p>
        {onClearFilter !== undefined ? (
          <p className="mt-3">
            <Button onClick={onClearFilter}>Clear the search</Button>
          </p>
        ) : null}
      </div>
    );
  }

  return (
    <div className="rounded border border-border bg-bg-surface p-5 text-center">
      <h2 className="text-heading-3 font-medium text-text-primary">No {what} yet</h2>
      <p className="mt-1 text-body text-text-secondary">
        Nothing has been created here.
      </p>
      {action !== undefined ? <p className="mt-3">{action}</p> : null}
    </div>
  );
}

/**
 * The screen is loading, and says so to a screen reader as well as to an eye.
 *
 * `aria-busy` on the region plus a visually hidden status line: a sighted user
 * sees skeletons, and a screen-reader user hears that something is happening
 * rather than silence (`docs/UI-UX/13`).
 */
export function LoadingRegion({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div aria-busy="true" aria-live="polite">
      <span className="sr-only">{label}</span>
      {children}
    </div>
  );
}
