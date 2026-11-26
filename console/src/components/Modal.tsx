import { useId, type ReactNode } from "react";
import { useDialog } from "./useDialog";

/** Focused actions use a modal; destructive confirmations opt out of motion. */
export function Modal({ open, title, onClose, children, footer, dismissible = true, motion = "default" }: {
  open: boolean;
  title: string;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
  /** One-time secrets require acknowledgement before dismissal. */
  dismissible?: boolean;
  motion?: "default" | "none";
}) {
  const panel = useDialog(open, onClose, dismissible);
  const titleId = useId();
  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-text-primary/40 p-4">
      <div ref={panel} role="dialog" aria-modal="true" aria-labelledby={titleId} tabIndex={-1}
        data-state="open" data-confirm-dialog={motion === "none" ? true : undefined}
        className={`${motion === "default" ? "motion-modal " : ""}max-h-full w-full max-w-lg overflow-y-auto rounded border border-border bg-bg-surface p-5 shadow-overlay`}>
        {dismissible ? <button type="button" onClick={onClose} aria-label="Close dialog" className="float-right min-h-7 min-w-7 rounded px-2 py-1 text-small text-text-secondary">Close</button> : null}
        <h2 id={titleId} className="text-heading-2 font-medium text-text-primary">{title}</h2>
        <div className="mt-3 text-body text-text-primary">{children}</div>
        {footer !== undefined ? <div className="mt-5 flex justify-end gap-2">{footer}</div> : null}
      </div>
    </div>
  );
}
