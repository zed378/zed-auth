import { useState } from "react";
import { NavLink } from "react-router-dom";
import { discloseAdvancedItems } from "./navigationDisclosure";
import { useNavigationDisclosure } from "./useNavigationDisclosure";

/** The organization-level destinations from the console information architecture. */
interface NavItem {
  label: string;
  to: string;
  icon: string;
  /** An advanced concept shown only when relevant to the active org. */
  feature?: string;
  /** Only destinations that do not have a screen yet carry a phase badge. */
  phase?: string;
}

interface NavSection {
  label?: string;
  items: NavItem[];
}

const SECTIONS: NavSection[] = [
  {
    items: [{ label: "Overview", to: "/", icon: "M3 3h7v7H3z M14 3h7v7h-7z M3 14h7v7H3z M14 14h7v7h-7z" }],
  },
  {
    label: "Organization",
    items: [
      { label: "Projects", to: "/projects", icon: "M3 7h7l2 2h9v10H3z M3 7V5h7l2 2" },
      { label: "Users", to: "/users", icon: "M16 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2 M10 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8 M20 21v-2a4 4 0 0 0-3-3.87 M16 3.13a4 4 0 0 1 0 7.75" },
      { label: "Granted Projects", to: "/granted-projects", feature: "project-grants", icon: "M14 3h7v7 M10 14 21 3 M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" },
      { label: "Policies", to: "/policies", icon: "M12 22s8-4 8-11V5l-8-3-8 3v6c0 7 8 11 8 11z M9 12l2 2 4-4" },
      { label: "Audit Log", to: "/audit-log", icon: "M3 12a9 9 0 1 0 2.64-6.36L3 8 M3 3v5h5 M12 7v5l3 2" },
      // PG-16 settled that branding lives in organization settings; the screen
      // remains unscheduled, so the link is labelled instead of implied live.
      { label: "Settings", to: "/settings", phase: "Later", icon: "M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8 M19.4 15a1.7 1.7 0 0 0 .34 1.88l.06.06-1.7 2.94-.08-.03a1.7 1.7 0 0 0-1.82.4l-.05.06h-3.4l-.02-.08a1.7 1.7 0 0 0-1.38-1.25l-.08-.01-2.4-1.7.03-.08a1.7 1.7 0 0 0-.4-1.82l-.06-.05v-3.4l.08-.02a1.7 1.7 0 0 0 1.25-1.38l.01-.08 1.7-2.4.08.03a1.7 1.7 0 0 0 1.82-.4l.05-.06h3.4l.02.08a1.7 1.7 0 0 0 1.38 1.25l.08.01 2.4 1.7-.03.08a1.7 1.7 0 0 0 .4 1.82l.06.05v3.4z" },
    ],
  },
];

/**
 * Persistent navigation. At tablet widths it becomes an icon rail, with an
 * explicit toggle for the full labels. NavLink owns active state so it always
 * follows the router, including direct links and browser history.
 */
export function SideNav() {
  const [expandedAtTablet, setExpandedAtTablet] = useState(false);
  const disclosure = useNavigationDisclosure();
  const sections = SECTIONS.map((section) => ({
    ...section,
    items: discloseAdvancedItems(section.items, disclosure),
  }));

  return (
    <nav
      aria-label="Console"
      className="border-b border-border bg-bg-surface tablet:min-h-screen tablet:shrink-0 tablet:border-r tablet:border-b-0"
      data-tablet-expanded={expandedAtTablet}
    >
      <div className={`h-full ${expandedAtTablet ? "tablet:w-nav" : "tablet:w-16"} desktop:w-nav`}>
        <div className={`flex items-center justify-between gap-2 p-3 tablet:flex-col tablet:justify-center tablet:gap-1 tablet:px-2 desktop:flex-row desktop:justify-start desktop:gap-3 desktop:p-5 ${expandedAtTablet ? "tablet:flex-row tablet:justify-between tablet:gap-2" : ""}`}>
          <div className="flex min-w-0 items-center gap-3">
            <img src="/zed-auth-mark.svg" alt="" width={36} height={36} className="shrink-0" />
            <span className={expandedAtTablet ? "hidden min-w-0 tablet:block" : "hidden desktop:block"}>
              <span className="block text-heading-3 font-bold text-text-primary">Zed Auth</span>
              <span className="block text-small text-text-secondary">Console</span>
            </span>
          </div>
          <button
            type="button"
            className="hidden min-h-11 min-w-11 items-center justify-center rounded text-text-primary hover:bg-bg-base focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent tablet:flex desktop:hidden"
            aria-label={expandedAtTablet ? "Collapse navigation" : "Expand navigation"}
            aria-expanded={expandedAtTablet}
            aria-controls="console-navigation-links"
            onClick={() => setExpandedAtTablet((expanded) => !expanded)}
          >
            <svg aria-hidden="true" viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round">
              <path d={expandedAtTablet ? "M15 18l-6-6 6-6 M20 12H9" : "M9 18l6-6-6-6 M4 12h11"} />
            </svg>
          </button>
        </div>

        <ul id="console-navigation-links" className="flex flex-col gap-1 px-2 pb-4 tablet:px-1 desktop:px-3">
          {sections.map((section, index) => (
            <li key={section.label ?? `section-${index}`}>
              {section.label ? (
                <h2 className={`${expandedAtTablet ? "hidden tablet:block" : "hidden desktop:block"} px-2 pt-4 pb-1 text-small font-medium text-text-secondary uppercase tracking-wide`}>
                  {section.label}
                </h2>
              ) : null}
              <ul className="flex flex-col gap-1">
                {section.items.map((item) => (
                  <li key={item.to}>
                    <NavLink
                      to={item.to}
                      end={item.to === "/"}
                      aria-label={item.label}
                      title={expandedAtTablet ? undefined : item.label}
                      className={({ isActive }) => [
                        `flex min-h-11 items-center gap-3 rounded px-3 py-2 text-body transition-colors tablet:justify-center ${expandedAtTablet ? "tablet:px-3" : "tablet:px-0"} desktop:justify-start desktop:px-3`,
                        isActive ? "bg-accent/10 font-medium text-accent" : "text-text-primary hover:bg-bg-base",
                      ].join(" ")}
                    >
                      <svg aria-hidden="true" viewBox="0 0 24 24" className="h-5 w-5 shrink-0" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round">
                        <path d={item.icon} />
                      </svg>
                      <span className={expandedAtTablet ? "hidden tablet:inline" : "hidden tablet:hidden desktop:inline"}>{item.label}</span>
                      {item.phase ? (
                        <span
                          className={`ml-auto rounded border border-border px-1 text-small text-text-secondary ${expandedAtTablet ? "hidden tablet:inline" : "hidden desktop:inline"}`}
                          title={item.phase === "Later" ? "Not built, and not scheduled on the roadmap yet" : `Arrives in phase ${item.phase.replace(/^P/, "")}`}
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
      </div>
    </nav>
  );
}
