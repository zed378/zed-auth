import { NavLink } from "react-router-dom";

/**
 * The navigation tree from docs/PLAN/06-FRONTEND-ARCHITECTURE.md § Information
 * Architecture, which mirrors docs/PLAN/04-DATA-MODEL.md almost 1:1 on purpose:
 * navigation should never require a concept the data model does not have.
 *
 * Every destination here is a Phase 1+ screen and currently renders a
 * placeholder saying so. That is deliberate rather than lazy — the IA is a
 * decision already made, and encoding it now means Phase 1 adds page bodies
 * rather than renegotiating structure. `docs/PLAN/16` forbids building Phase N+1
 * features early; a route with a placeholder is not the feature.
 */

interface NavItem {
  label: string;
  to: string;
  /** Roadmap phase that fills this in. Rendered so the shell is honest. */
  phase?: string;
}

interface NavSection {
  /** Section heading, or undefined for the top-level group. */
  label?: string;
  items: NavItem[];
}

/**
 * The Instance group is visible only to INSTANCE_OWNER (docs/PLAN/06 § IA).
 *
 * It is not rendered at all yet, rather than rendered and disabled: the
 * console has no token to read a role claim from until P1-03 lands, and
 * showing an instance-administration section to everyone in the meantime would
 * teach the wrong thing about what the console is. When it returns, the check
 * is a claim on the access token — and the API enforces it independently,
 * because a hidden nav item is not a security control (docs/PLAN/08, CLAUDE.md).
 */
const SECTIONS: NavSection[] = [
  {
    items: [{ label: "Overview", to: "/" }],
  },
  {
    label: "Organization",
    items: [
      { label: "Projects", to: "/projects", phase: "P1" },
      { label: "Users", to: "/users", phase: "P1" },
      { label: "Granted Projects", to: "/granted-projects", phase: "P4" },
      { label: "Policies", to: "/policies", phase: "P1" },
      { label: "Audit Log", to: "/audit-log", phase: "P1" },
      { label: "Settings", to: "/settings", phase: "P1" },
    ],
  },
];

export function SideNav() {
  return (
    <nav
      // Labelled because a document may hold more than one navigation
      // landmark, and "navigation" twice in a screen reader's landmark list
      // tells the user nothing about which is which (docs/UI-UX/13).
      aria-label="Console"
      className="
        border-b border-border bg-bg-surface
        tablet:w-nav tablet:shrink-0 tablet:border-r tablet:border-b-0
        tablet:min-h-screen
      "
    >
      <div className="p-4 tablet:p-5">
        {/*
          The wordmark. Per-organization branding may replace the logo and the
          accent colour, and nothing else (docs/UI-UX/05, src/branding/).
        */}
        <div className="flex items-center gap-3">
          {/*
            The identity mark. An <img> rather than an inlined <svg> on
            purpose: the mark paints itself from a gradient with an id, and two
            inlined copies on one page would collide on that id — the second
            would silently render with the first's gradient.

            Decorative here, so alt="" and the accessible name comes from the
            wordmark beside it. Announcing "Zed Auth" twice to a screen reader
            is noise (docs/UI-UX/13).
          */}
          <img src="/zed-auth-mark.svg" alt="" width={36} height={36} className="shrink-0" />
          <span>
            <span className="block text-heading-3 font-bold text-text-primary">Zed Auth</span>
            <span className="block text-small text-text-secondary">Console</span>
          </span>
        </div>
      </div>

      <ul className="flex flex-col gap-1 px-2 pb-4 tablet:px-3">
        {SECTIONS.map((section, index) => (
          <li key={section.label ?? `section-${index}`}>
            {section.label ? (
              <h2 className="px-2 pt-4 pb-1 text-small font-medium text-text-secondary uppercase tracking-wide">
                {section.label}
              </h2>
            ) : null}

            <ul className="flex flex-col gap-1">
              {section.items.map((item) => (
                <li key={item.to}>
                  <NavLink
                    to={item.to}
                    end={item.to === "/"}
                    className={({ isActive }) =>
                      [
                        // Minimum target size, kept even at the compact density
                        // docs/UI-UX/06 asks for. docs/UI-UX/13: density and accessibility
                        // are not in conflict if target sizing is planned from
                        // the component level rather than added afterwards.
                        "flex min-h-11 items-center rounded px-3 py-2 text-body",
                        "transition-colors",
                        isActive
                          ? "bg-accent/10 font-medium text-accent"
                          : "text-text-primary hover:bg-bg-base",
                      ].join(" ")
                    }
                  >
                    <span className="flex-1">{item.label}</span>

                    {item.phase ? (
                      // Says what is not built yet rather than presenting a
                      // dead link as a working one. docs/UI-UX/21's governance rule
                      // is about marketing copy; the same honesty applies to a
                      // nav item that goes nowhere.
                      <span
                        className="ml-2 rounded border border-border px-1 text-small text-text-secondary"
                        title={`Arrives in phase ${item.phase}`}
                      >
                        {item.phase}
                      </span>
                    ) : null}
                  </NavLink>
                </li>
              ))}
            </ul>
          </li>
        ))}
      </ul>
    </nav>
  );
}
