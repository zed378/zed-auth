/**
 * A role, with where it came from (P2-12).
 *
 * `docs/UI-UX/08` § Cross-Screen Requirements makes this mandatory on **every**
 * screen that shows roles — the Authorizations tab, the User Grants tab, and
 * Granted Projects in Phase 4 — and says so explicitly because it is the kind
 * of requirement each screen would otherwise answer for itself.
 *
 * `docs/UI-UX/04` is where the reason lives: "where did this role come from"
 * is the question an administrator asks when access looks wrong, and the two
 * answers have completely different remedies. A **direct** grant is revoked
 * here. A **delegated** one arrived through a Project Grant from another
 * organization, and revoking it means talking to them — or removing the
 * delegation, which affects everybody it covers.
 *
 * **Every grant in Phase 2 is direct**, because Project Grants arrive in Phase
 * 4 and the database refuses a non-null `project_grant_id` until then. This is
 * built now anyway, on `P2-12` step 4's instruction: adding the distinction
 * later means auditing every screen that shows a role for a treatment it was
 * never given. The delegated branch is unreachable today and tested anyway, so
 * that Phase 4 turns it on rather than writes it.
 *
 * `docs/UI-UX/06` § Role-Source Visual Treatment specifies the shape: a plain
 * badge for direct, and for delegated a badge plus a small link mark with the
 * source organization "on hover/tap, so the origin is always one interaction
 * away". Hover is not available to a screen reader or a keyboard, so the same
 * sentence is also present as text in the accessibility tree — the rule that
 * colour and hover are never the sole carrier (`docs/UI-UX/13`).
 */
export function RoleSourceBadge({
  roleKey,
  /** The organization a Project Grant delegated this from. Absent means direct. */
  delegatedFrom,
}: {
  roleKey: string;
  delegatedFrom?: string;
}) {
  if (delegatedFrom === undefined) {
    return (
      <span className="inline-flex items-center rounded border border-border px-2 py-0.5 font-mono text-small text-text-primary">
        {roleKey}
      </span>
    );
  }

  return (
    <span
      className="inline-flex items-center gap-1 rounded border border-accent px-2 py-0.5 font-mono text-small text-text-primary"
      title={`Delegated from ${delegatedFrom}`}
    >
      {roleKey}
      <LinkMark />
      {/*
        The same fact as the `title`, for everyone who cannot hover. A tooltip
        is an affordance for a mouse and nothing else.
      */}
      <span className="sr-only">— delegated from {delegatedFrom}</span>
    </span>
  );
}

/** The link mark from `docs/UI-UX/06`. Decorative: the text beside it says this. */
function LinkMark() {
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 16 16"
      width="12"
      height="12"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      className="shrink-0 text-accent"
    >
      <path d="M6.5 9.5a2.5 2.5 0 0 0 3.5 0l2-2a2.5 2.5 0 0 0-3.5-3.5l-.7.7" />
      <path d="M9.5 6.5a2.5 2.5 0 0 0-3.5 0l-2 2a2.5 2.5 0 0 0 3.5 3.5l.7-.7" />
    </svg>
  );
}

/**
 * A user's roles in one project, named rather than counted.
 *
 * `docs/UI-UX/18` is explicit for the Granted Projects screen and the reason
 * generalises: "never just a count ('3 roles') without naming them, since
 * knowing exactly which roles is the entire point of this screen."
 *
 * The empty case is a **sentence**, not a blank. `docs/PLAN/08` § Least
 * Privilege means a user with no grant has no access at all — there is no
 * implicit default role — and that is the normal state for a new account. A
 * blank cell reads as missing data; "No access to this project" is an answer.
 */
export function RoleList({
  roleKeys,
  delegatedFrom,
  none = "No access",
}: {
  roleKeys: string[];
  delegatedFrom?: string;
  none?: string;
}) {
  if (roleKeys.length === 0) {
    return <span className="text-small text-text-secondary">{none}</span>;
  }

  return (
    <span className="flex flex-wrap gap-1">
      {roleKeys.map((key) => (
        <RoleSourceBadge key={key} roleKey={key} delegatedFrom={delegatedFrom} />
      ))}
    </span>
  );
}
