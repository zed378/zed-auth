/**
 * Badge, per `docs/UI-UX/07` § Badge and `docs/UI-UX/06`'s status table.
 *
 * **The rule that shapes it: a badge is never the sole carrier of critical
 * information.** Colour and icon are reinforcement; the text is the message.
 * A user who cannot distinguish the colours, or who is reading a screenshot in
 * greyscale, gets the same information.
 *
 * Revoked grants use danger, as specified in docs/UI-UX/06; deactivated
 * users stay neutral.
 */
export type Tone = "neutral" | "positive" | "attention" | "muted" | "revoked";
export type BadgeStatus = "active" | "invited" | "deactivated" | "locked" | "revoked" | "draft";

const tones: Record<Tone, string> = {
  neutral: "border-border text-text-primary",
  positive: "border-success text-success",
  attention: "border-warning text-warning",
  muted: "border-border text-text-secondary",
  revoked: "border-danger text-danger",
};

export function Badge({ tone = "neutral", children }: { tone?: Tone; children: string }) {
  if (!children.trim()) throw new Error("A badge requires visible text.");
  return (
    <span
      className={`inline-flex items-center gap-1 rounded border bg-bg-surface px-2 py-0.5 text-small ${tones[tone]}`}
    >
      <svg aria-hidden="true" viewBox="0 0 12 12" width="12" height="12" className="shrink-0" fill="none" stroke="currentColor">
        {tone === "revoked" ? <path d="M3 9 9 3" /> : <circle cx="6" cy="6" r="3" fill={tone === "positive" ? "currentColor" : "none"} />}
      </svg>
      {children}
    </span>
  );
}

export function StatusBadge({ status }: { status: BadgeStatus }) {
  return <Badge tone={statusTone(status)}>{status[0].toUpperCase() + status.slice(1)}</Badge>;
}

/** The tone for a user's status, per `docs/UI-UX/06`'s status table. */
export function statusTone(status: string): Tone {
  switch (status) {
    case "active":
      return "positive";
    case "invited":
      return "attention";
    case "locked":
      return "attention";
    case "deactivated":
      return "muted";
    case "revoked":
      return "revoked";
    default:
      return "neutral";
  }
}
