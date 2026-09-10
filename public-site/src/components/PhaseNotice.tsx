import type { ReactNode } from "react";

/**
 * Marks content describing something that has not shipped.
 *
 * `docs/UI-UX/21` § Content Governance and `CLAUDE.md` both forbid present-tense
 * marketing copy for an unshipped capability. The obvious way to honour that
 * is careful phrasing, and careful phrasing is the first thing to erode — a
 * sentence gets tightened, a hedge disappears, and a roadmap item is now a
 * claim. A component makes the distinction structural instead: it is visible
 * on the page, greppable in the source, and awkward to delete by accident.
 *
 * `color-warning`, never `color-danger`. Nothing here is destructive or
 * alarming, and `docs/UI-UX/05` reserves danger so its appearance stays a reliable
 * signal — spending it on "this is coming later" is exactly the erosion that
 * makes it mean nothing when something really is irreversible.
 */
export function PhaseNotice({
  phase,
  children,
}: {
  /** The roadmap phase from `docs/PLAN/16` that delivers this. */
  phase: string;
  children: ReactNode;
}) {
  return (
    <aside className="site-notice" role="note">
      <p className="site-notice__title">Not built yet — arrives in {phase}</p>
      {children}
    </aside>
  );
}
