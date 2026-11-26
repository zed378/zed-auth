import { useEffect, useId, useRef, useState, type ReactNode } from "react";

import { Button } from "./Button";
import { Modal } from "./Modal";

/**
 * Confirmation dialog, per `docs/UI-UX/07` § Confirmation Dialog and
 * `docs/UI-UX/04`'s "consequence before confirmation" pattern.
 *
 * Three rules from the specification, all load-bearing:
 *
 *   - The title states the ACTION plainly — "Deactivate this user?" — not
 *     "Are you sure?".
 *   - The body is a **consequence summary in plain language**. A generic
 *     "this cannot be undone" teaches nothing; "every session ends
 *     immediately" tells somebody what will happen to the person they are
 *     about to affect.
 *   - The confirm button carries the **actual verb**. "Deactivate", never
 *     "OK" or "Confirm" — a button labelled with the verb is one that cannot
 *     be clicked without reading it.
 *
 * Typed confirmation is available and reserved: `docs/UI-UX/07` calls it the
 * design system's one deliberate friction point, for cases where an accidental
 * click would be very costly. Using it everywhere would make it furniture.
 */
export function ConfirmDialog({
  open,
  title,
  consequence,
  verb,
  onConfirm,
  onCancel,
  busy = false,
  /** When set, the confirm button stays disabled until this exact text is typed. */
  typeToConfirm,
  consequenceLoading = false,
  consequenceError,
  onRetryConsequence,
  success,
}: {
  open: boolean;
  title: string;
  consequence: Exclude<ReactNode, null | undefined | boolean>;
  verb: string;
  onConfirm: () => void;
  onCancel: () => void;
  busy?: boolean;
  typeToConfirm?: string;
  /** Hide cached consequences and block the action while recomputing. */
  consequenceLoading?: boolean;
  consequenceError?: string;
  onRetryConsequence?: () => void;
  /** Set only after the mutation succeeds; keep open until acknowledgement. */
  success?: string;
}) {
  const [typed, setTyped] = useState("");
  const input = useRef<HTMLInputElement>(null);
  const inputId = useId();
  const reasonId = useId();

  // What was typed belongs to ONE opening. Kept across a cancel, reopening the
  // dialog for another row with the same name would arrive already unlocked —
  // the friction spent before anyone read the new consequence.
  if (!open && typed !== "") setTyped("");

  // The typed input takes focus when the dialog opens (`docs/UI-UX/18` §
  // Project Grants Tab). This effect runs after `Modal`'s own, which focuses
  // the panel, so it wins; the panel's focus trap still holds.
  const typing = open && typeToConfirm !== undefined;
  useEffect(() => {
    if (typing) input.current?.focus();
  }, [typing]);

  // Case-sensitive. "acme corp" and "Acme Corp" being interchangeable defeats
  // the point of typing it (P1-16 made the same call server-side).
  const unlocked = typeToConfirm === undefined || typed === typeToConfirm;
  const blocked = !unlocked || consequenceLoading || Boolean(consequenceError);
  const reason = consequenceLoading ? "Wait for the current consequences to load."
    : consequenceError ? "Load the consequences before continuing."
    : !unlocked ? `Type ${typeToConfirm} exactly to continue.` : undefined;

  if (open && /^(ok|confirm)$/i.test(verb.trim())) {
    throw new Error("ConfirmDialog requires an action verb, not OK or Confirm.");
  }
  if (open && (consequence == null || typeof consequence === "boolean" || consequence === "")) {
    throw new Error("ConfirmDialog requires a plain-language consequence.");
  }

  return (
    <Modal
      motion="none"
      open={open}
      title={title}
      onClose={onCancel}
      dismissible={!busy}
      footer={
        success ? <Button onClick={onCancel}>Done</Button> : <>
          <Button onClick={onCancel} disabled={busy} disabledReason="Wait for the action to finish.">Cancel</Button>
          <Button
            variant="destructive"
            loading={busy}
            disabled={blocked}
            disabledReason={reason}
            aria-describedby={reason ? reasonId : undefined}
            onClick={() => {
              if (busy || blocked) return;
              setTyped("");
              onConfirm();
            }}
          >
            {verb}
          </Button>
        </>
      }
    >
      {success ? <p role="status">{success}</p> : <>
      <div className="text-body text-text-primary" aria-busy={consequenceLoading || undefined}>
        {consequenceLoading ? <div role="status"><span className="sr-only">Loading current consequences.</span><div aria-hidden="true" className="h-12 w-full rounded bg-border" /></div>
          : consequenceError ? <div role="alert"><p>{consequenceError}</p>{onRetryConsequence ? <Button onClick={onRetryConsequence}>Retry consequences</Button> : null}</div>
          : consequence}
      </div>
      {reason ? <p id={reasonId} className="mt-2 text-small text-text-secondary">{reason}</p> : null}

      {typeToConfirm !== undefined ? (
        <>
          <label htmlFor={inputId} className="mt-4 block text-small text-text-secondary">
            Type <span className="font-mono text-text-primary">{typeToConfirm}</span> to confirm
          </label>
          <input
            ref={input}
            id={inputId}
            disabled={busy}
            autoComplete="off"
            spellCheck={false}
            value={typed}
            onChange={(event) => setTyped(event.currentTarget.value)}
            className="mt-1 w-full rounded border border-border bg-bg-base px-3 py-2 font-mono text-body text-text-primary"
          />
        </>
      ) : null}
      </>}
    </Modal>
  );
}
