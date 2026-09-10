import type { ButtonHTMLAttributes, ReactNode } from "react";

/**
 * Button, per `docs/UI-UX/07-COMPONENT-SPECIFICATION.md` § Button.
 *
 * Three variants and no more. The specification's rule is the interesting
 * part: **a `destructive` button must never be the visually dominant action on
 * a screen where a non-destructive one is the expected path.** So a
 * "Deactivate user" sitting among ordinary actions is `secondary` with danger
 * text, and the full destructive weight is reserved for the confirmation
 * dialog — where it is the only thing on the screen.
 *
 * `color-danger` appears here and in `ConfirmDialog`, and nowhere else. Its
 * meaning has to stay reliable (`docs/UI-UX/06`, `CLAUDE.md`), and one
 * decorative use degrades every earlier one.
 */
export type ButtonVariant = "primary" | "secondary" | "destructive" | "danger-text";

interface Props extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, "className"> {
  variant?: ButtonVariant;
  loading?: boolean;
  children: ReactNode;
}

const base =
  "inline-flex items-center justify-center gap-2 rounded border px-3 py-2 text-body " +
  "font-medium transition-colors focus-visible:outline focus-visible:outline-2 " +
  "focus-visible:outline-offset-2 focus-visible:outline-accent disabled:cursor-not-allowed " +
  "disabled:opacity-60";

const variants: Record<ButtonVariant, string> = {
  primary: "border-accent bg-accent text-bg-surface hover:opacity-90",
  secondary: "border-border bg-bg-surface text-text-primary hover:bg-bg-base",
  destructive: "border-danger bg-danger text-bg-surface hover:opacity-90",
  "danger-text": "border-border bg-bg-surface text-danger hover:bg-bg-base",
};

export function Button({ variant = "secondary", loading = false, children, ...rest }: Props) {
  return (
    <button
      {...rest}
      // A loading button is disabled, or a second click fires a second
      // request — and on a create that is two objects.
      disabled={rest.disabled === true || loading}
      // The label is REPLACED rather than hidden, and the element keeps its
      // width through `min-w`. A button that shrinks while it works moves
      // everything beside it (docs/UI-UX/07 § Button states).
      aria-busy={loading || undefined}
      className={`${base} ${variants[variant]}`}
    >
      {loading ? (
        <>
          <Spinner />
          {/* Kept in the accessibility tree so the button still has a name. */}
          <span className="sr-only">{children}</span>
          <span aria-hidden="true" className="invisible">
            {children}
          </span>
        </>
      ) : (
        children
      )}
    </button>
  );
}

function Spinner() {
  return (
    <span
      aria-hidden="true"
      className="absolute inline-block h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent"
    />
  );
}
