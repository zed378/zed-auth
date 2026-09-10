/**
 * Badge, per `docs/UI-UX/07` § Badge and `docs/UI-UX/06`'s status table.
 *
 * **The rule that shapes it: a badge is never the sole carrier of critical
 * information.** Colour and icon are reinforcement; the text is the message.
 * A user who cannot distinguish the colours, or who is reading a screenshot in
 * greyscale, gets the same information.
 *
 * `color-danger` is not among the tones. A status badge is a statement about
 * what something *is*, and danger is reserved for destructive actions
 * (`docs/UI-UX/06`, `CLAUDE.md`) — a deactivated user is not a destructive
 * action, and colouring them the same as a delete button spends the one signal
 * that has to stay reliable.
 */
export type Tone = "neutral" | "positive" | "attention" | "muted";

const tones: Record<Tone, string> = {
  neutral: "border-border text-text-primary",
  positive: "border-success text-success",
  attention: "border-warning text-warning",
  muted: "border-border text-text-secondary",
};

export function Badge({ tone = "neutral", children }: { tone?: Tone; children: string }) {
  return (
    <span
      className={`inline-flex items-center rounded border px-2 py-0.5 text-small ${tones[tone]}`}
    >
      {children}
    </span>
  );
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
    default:
      return "neutral";
  }
}
