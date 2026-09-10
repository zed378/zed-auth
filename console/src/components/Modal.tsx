import { useEffect, useRef } from "react";
import type { ReactNode } from "react";

/**
 * Modal, per `docs/UI-UX/07` § Modal / Side Panel.
 *
 * The centre-screen variant only. The side panel is for multi-step,
 * detail-heavy flows and arrives with `P1-23`'s invite flow; building an
 * unused one now would be a component nobody has run through
 * `docs/UI-UX/19`'s chain.
 *
 * Everything about focus here is `docs/UI-UX/13`'s requirement rather than a
 * nicety: a dialog that does not take focus is one a screen-reader user never
 * finds, and a dialog that does not trap it is one they walk out of into a
 * page that is inert.
 */
export function Modal({
  open,
  title,
  onClose,
  children,
  footer,
  /** Set when the dialog cannot be dismissed casually — see ClientSecretModal. */
  dismissible = true,
}: {
  open: boolean;
  title: string;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
  dismissible?: boolean;
}) {
  const panel = useRef<HTMLDivElement>(null);
  const restoreTo = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (!open) return;

    // Where focus goes back to when this closes. Without it, focus lands on
    // <body> and the next Tab starts from the top of the page — which is the
    // most common way a modal breaks keyboard navigation.
    restoreTo.current = document.activeElement as HTMLElement | null;
    panel.current?.focus();

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape" && dismissible) {
        onClose();
        return;
      }
      if (event.key !== "Tab") return;

      const focusable = panel.current?.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), input:not([disabled]), textarea, select, [tabindex]:not([tabindex="-1"])',
      );
      if (focusable === undefined || focusable.length === 0) return;

      const first = focusable[0];
      const last = focusable[focusable.length - 1];

      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };

    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      restoreTo.current?.focus();
    };
  }, [open, onClose, dismissible]);

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-text-primary/40 p-4">
      <div
        ref={panel}
        role="dialog"
        aria-modal="true"
        aria-labelledby="modal-title"
        tabIndex={-1}
        className="w-full max-w-lg rounded border border-border bg-bg-surface p-5 shadow-overlay"
      >
        <h2 id="modal-title" className="text-heading-2 font-medium text-text-primary">
          {title}
        </h2>
        <div className="mt-3 text-body text-text-primary">{children}</div>
        {footer !== undefined ? <div className="mt-5 flex justify-end gap-2">{footer}</div> : null}
      </div>
    </div>
  );
}
