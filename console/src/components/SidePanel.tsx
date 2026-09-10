import { useEffect, useRef } from "react";
import type { ReactNode } from "react";

/**
 * Side panel, per `docs/UI-UX/07` § Modal / Side Panel.
 *
 * The specification's rule is about which to use, not how they look: a modal
 * interrupts, a side panel accompanies. **Destructive confirmations are always
 * modals** so they interrupt rather than blend into an ongoing flow; a
 * multi-step, detail-heavy flow like inviting a user is a panel.
 *
 * Focus behaviour is identical to `Modal` and for the same reason: a panel
 * that does not take focus is one a screen-reader user never finds, and one
 * that does not trap it is one they walk out of into an inert page
 * (`docs/UI-UX/13`).
 */
export function SidePanel({
  open,
  title,
  onClose,
  children,
  footer,
  /** "Step 1 of 2", so a two-step flow does not feel like one overloaded form. */
  progress,
}: {
  open: boolean;
  title: string;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
  progress?: string;
}) {
  const panel = useRef<HTMLDivElement>(null);
  const restoreTo = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (!open) return;

    restoreTo.current = document.activeElement as HTMLElement | null;
    panel.current?.focus();

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
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
  }, [open, onClose]);

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex justify-end bg-text-primary/40">
      <div
        ref={panel}
        role="dialog"
        aria-modal="true"
        aria-labelledby="panel-title"
        tabIndex={-1}
        className="flex h-full w-full max-w-md flex-col border-l border-border bg-bg-surface shadow-overlay"
      >
        <div className="border-b border-border p-5">
          {progress !== undefined ? (
            // Announced, not just shown. A step counter a screen-reader user
            // does not hear is a counter only some people have.
            <p aria-live="polite" className="text-small text-text-secondary">
              {progress}
            </p>
          ) : null}
          <h2 id="panel-title" className="mt-1 text-heading-2 font-medium text-text-primary">
            {title}
          </h2>
        </div>

        <div className="flex-1 overflow-y-auto p-5 text-body text-text-primary">{children}</div>

        {footer !== undefined ? (
          <div className="flex justify-end gap-2 border-t border-border p-5">{footer}</div>
        ) : null}
      </div>
    </div>
  );
}
