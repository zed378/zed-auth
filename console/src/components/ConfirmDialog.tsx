import { useState } from "react";

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
}: {
  open: boolean;
  title: string;
  consequence: React.ReactNode;
  verb: string;
  onConfirm: () => void;
  onCancel: () => void;
  busy?: boolean;
  typeToConfirm?: string;
}) {
  const [typed, setTyped] = useState("");

  // Case-sensitive. "acme corp" and "Acme Corp" being interchangeable defeats
  // the point of typing it (P1-16 made the same call server-side).
  const unlocked = typeToConfirm === undefined || typed === typeToConfirm;

  return (
    <Modal
      open={open}
      title={title}
      onClose={onCancel}
      footer={
        <>
          <Button onClick={onCancel}>Cancel</Button>
          <Button
            variant="destructive"
            loading={busy}
            disabled={!unlocked}
            onClick={() => {
              setTyped("");
              onConfirm();
            }}
          >
            {verb}
          </Button>
        </>
      }
    >
      <div className="text-body text-text-primary">{consequence}</div>

      {typeToConfirm !== undefined ? (
        <>
          <label htmlFor="confirm-text" className="mt-4 block text-small text-text-secondary">
            Type <span className="font-mono text-text-primary">{typeToConfirm}</span> to confirm
          </label>
          <input
            id="confirm-text"
            value={typed}
            onChange={(event) => setTyped(event.currentTarget.value)}
            className="mt-1 w-full rounded border border-border bg-bg-base px-3 py-2 font-mono text-body text-text-primary"
          />
        </>
      ) : null}
    </Modal>
  );
}
