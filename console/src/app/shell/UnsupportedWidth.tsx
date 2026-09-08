import type { ReactNode } from "react";

/**
 * Below the supported width, the console is replaced by an explanation.
 *
 * `UI-UX/12` § Testing asks for this in as many words: below roughly 600px,
 * admin-facing screens should show "not supported, shows a message suggesting
 * a larger screen", because an explicit message beats a silently broken
 * layout. The console is a desktop tool by design (`UI-UX/00`) — admin work is
 * structured data entry and table scanning.
 *
 * The one exception in the plan is personal account settings (`UI-UX/16`),
 * which is genuinely mobile-optimised. It is not built yet; when it is, it
 * renders outside this wrapper rather than adding a condition here.
 *
 * **Replaced, not covered.** The first version painted a full-screen overlay
 * on top of the layout and left the layout in the DOM. That looked right and
 * was wrong twice over: the document then had two `<h1>` elements, and at
 * narrow widths a screen reader user would walk straight past the message into
 * an application the message says is unusable. Swapping the two subtrees means
 * exactly one of them exists for assistive technology at any width.
 *
 * Done with CSS rather than a width listener in JavaScript: a resize listener
 * re-renders on every frame of a drag and reports the wrong answer during the
 * first paint.
 */
export function UnsupportedWidth({ children }: { children: ReactNode }) {
  return (
    <>
      <div className="max-tablet:hidden">{children}</div>

      <div
        className="
          hidden
          max-tablet:flex max-tablet:min-h-screen max-tablet:flex-col
          max-tablet:items-center max-tablet:justify-center max-tablet:gap-3
          max-tablet:bg-bg-base max-tablet:p-6 max-tablet:text-center
        "
      >
        <h1 className="text-heading-2 font-bold text-text-primary">
          This screen is too narrow
        </h1>
        <p className="max-w-prose text-body text-text-secondary">
          The console is built for managing tables of users, roles and grants, which needs
          a tablet-sized screen or larger. Please open it on a device at least
          768&nbsp;pixels wide.
        </p>
      </div>
    </>
  );
}
