import { useState } from "react";

import { Button } from "./Button";
import { Modal } from "./Modal";

/**
 * The client secret, shown once (P1-22 step 4, `docs/UI-UX/08` § Applications
 * tab).
 *
 * This is the one screen in the console that shows a credential, and it shows
 * it exactly once because that is what the service does: the plaintext exists
 * in the `201` response and nowhere else, and no endpoint returns it again
 * (`P1-18`). Every choice here follows from that being **true rather than a
 * convention**:
 *
 *   - The warning is unmissable and states the consequence, not the rule.
 *     "This will not be shown again" is a fact about the system; "keep it
 *     safe" is advice.
 *   - The dialog cannot be dismissed by Escape or by clicking away. An
 *     accidental dismissal costs a rotation and a redeploy, and this is one of
 *     the few places where friction is worth more than flow.
 *   - The confirmation is a checkbox the user must tick. A single "Done"
 *     button is one reflexive click away from a lost secret.
 */
export function ClientSecretModal({
  open,
  applicationName,
  secret,
  onClose,
}: {
  open: boolean;
  applicationName: string;
  secret: string;
  onClose: () => void;
}) {
  const [acknowledged, setAcknowledged] = useState(false);
  const [copied, setCopied] = useState(false);

  return (
    <Modal
      open={open}
      // Not dismissible. See above.
      dismissible={false}
      title={`${applicationName} is registered`}
      onClose={onClose}
      footer={
        <Button
          variant="primary"
          disabled={!acknowledged}
          onClick={() => {
            setAcknowledged(false);
            setCopied(false);
            onClose();
          }}
        >
          Done
        </Button>
      }
    >
      <p role="alert" className="rounded border border-warning bg-bg-base p-3 text-warning">
        <strong>This secret will never be shown again.</strong> It is stored as a hash and cannot
        be recovered. If it is lost, the only way forward is to rotate it and update every
        deployment that uses it.
      </p>

      <label className="mt-4 block text-small font-medium text-text-secondary" htmlFor="secret">
        Client secret
      </label>
      <div className="mt-1 flex gap-2">
        <input
          id="secret"
          readOnly
          value={secret}
          // Readable, not masked. A masked value the user cannot check is a
          // value they paste wrong and discover at the next deploy — and it is
          // already on their screen either way.
          className="w-full rounded border border-border bg-bg-base px-3 py-2 font-mono text-small text-text-primary"
          onFocus={(event) => event.currentTarget.select()}
        />
        <Button
          onClick={() => {
            void navigator.clipboard?.writeText(secret).then(() => setCopied(true));
          }}
        >
          Copy
        </Button>
      </div>

      {/*
        aria-live, so a screen-reader user learns the copy worked. A visual
        "Copied" that says nothing to a screen reader is a confirmation only
        some users receive (docs/UI-UX/13).
      */}
      <p aria-live="polite" className="mt-1 h-4 text-small text-text-secondary">
        {copied ? "Copied to the clipboard." : ""}
      </p>

      <label className="mt-4 flex items-start gap-2 text-body text-text-primary">
        <input
          type="checkbox"
          checked={acknowledged}
          onChange={(event) => setAcknowledged(event.currentTarget.checked)}
          className="mt-1"
        />
        <span>I have stored this secret somewhere safe.</span>
      </label>
    </Modal>
  );
}
