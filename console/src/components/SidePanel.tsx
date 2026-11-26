import { useId, type ReactNode } from "react";
import { useDialog } from "./useDialog";

/** Detail-heavy flows use a panel; destructive confirmations must interrupt in a modal. */
export function SidePanel({ open, title, onClose, children, footer, progress, purpose = "flow" }: {
  open: boolean;
  title: string;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
  progress?: string;
  purpose?: "flow" | "detail" | "destructive-confirmation";
}) {
  const panel = useDialog(open, onClose);
  const titleId = useId();
  if (purpose === "destructive-confirmation") throw new Error("Destructive confirmations must use Modal or ConfirmDialog, never SidePanel.");
  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50 flex justify-end bg-text-primary/40">
      <div ref={panel} role="dialog" aria-modal="true" aria-labelledby={titleId} tabIndex={-1}
        data-state="open" className="motion-panel flex h-full w-full flex-col border-l border-border bg-bg-surface shadow-overlay lg:max-w-md">
        <div className="border-b border-border p-5">
          <button type="button" onClick={onClose} aria-label="Close panel" className="float-right min-h-7 min-w-7 rounded px-2 py-1 text-small text-text-secondary">Close</button>
          {progress !== undefined ? <p aria-live="polite" className="text-small text-text-secondary">{progress}</p> : null}
          <h2 id={titleId} className="mt-1 text-heading-2 font-medium text-text-primary">{title}</h2>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto p-5 text-body text-text-primary">{children}</div>
        {footer !== undefined ? <div className="flex justify-end gap-2 border-t border-border p-5">{footer}</div> : null}
      </div>
    </div>
  );
}
