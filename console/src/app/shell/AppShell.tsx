import type { ReactNode } from "react";

import { SideNav } from "./SideNav";
import { SessionBar } from "./SessionBar";
import { SkipLink } from "./SkipLink";
import { UnsupportedWidth } from "./UnsupportedWidth";

/**
 * The application shell: skip link, navigation, main content region.
 *
 * The landmark structure here is the accessibility foundation for every screen
 * that will ever be added, which is why it exists before any of them. docs/UI-UX/13
 * puts it plainly — accessibility is "built in from the first screen rather
 * than retrofitted", and a shell without landmarks means every page inherits
 * a document a screen reader user has to explore linearly.
 *
 * Three landmarks, each appearing exactly once:
 *
 *   <nav>    the console's navigation, labelled so it is distinguishable from
 *            any future secondary nav
 *   <main>   the page content, and the target of the skip link
 *   <footer> environment and version, which belongs in contentinfo rather
 *            than inside main
 *
 * The nav is a fixed-width track (`--container-nav`) and the content takes the
 * rest. `min-w-0` on the content column is load-bearing: a flex item defaults
 * to `min-width: auto`, so a wide table would push the layout instead of
 * scrolling inside it, and the nav would be squeezed until its labels wrapped.
 */
export function AppShell({ children }: { children: ReactNode }) {
  return (
    <>
      <SkipLink />

      {/*
        Below tablet width the layout is REPLACED by an explanation, not
        covered by one. docs/UI-UX/12 § Testing asks for the message; swapping the
        subtrees rather than overlaying keeps exactly one <h1> in the document
        and stops a screen reader user at narrow width from walking past the
        message into the application it says is unusable.
      */}
      <UnsupportedWidth>
        <div className="flex min-h-screen flex-col bg-bg-base tablet:flex-row">
          <SideNav />

          <div className="flex min-w-0 flex-1 flex-col">
            <SessionBar />

            {/*
              tabIndex={-1} makes this a programmatic focus target for the skip
              link without adding it to the tab order. Without it the skip link
              moves the scroll position but not the focus, so the next Tab
              continues from the navigation — the bug that makes skip links look
              implemented while doing nothing for keyboard users.
            */}
            <main id="main-content" tabIndex={-1} className="min-w-0 flex-1 p-5 desktop:p-6">
              {children}
            </main>

            <footer className="border-t border-border px-5 py-3 text-small text-text-secondary desktop:px-6">
              Zed Auth Console
            </footer>
          </div>
        </div>
      </UnsupportedWidth>
    </>
  );
}
