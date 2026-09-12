import { NavLink } from "react-router-dom";

/**
 * The project detail's tab strip (P2-11).
 *
 * `docs/UI-UX/08` names three tabs on this screen — Applications, Roles and,
 * from `P2-12`, Authorizations — but `docs/UI-UX/07` specifies no tab
 * component. Rather than invent one that a later specification would have to
 * contradict, this is what these actually are: **links between routes**.
 *
 * That is not a cosmetic distinction. ARIA's tab pattern describes panels
 * swapped inside one document, and it asks for arrow-key navigation, a single
 * tab stop and `aria-controls` pointing at a panel that is present. None of
 * that is true here — each tab is a URL with its own data, its own loading
 * state and its own back-button behaviour. Marking links up as `role="tab"`
 * would promise a screen-reader user an interaction model this screen does not
 * implement, which is worse than plain links (`docs/UI-UX/13`).
 *
 * So: a labelled `nav`, `NavLink` for the active styling, and `aria-current`
 * from React Router — which is the same signal the breadcrumb uses.
 */
export function ProjectNav({ projectId }: { projectId: string }) {
  const tabs = [
    { to: `/projects/${projectId}`, label: "Applications", end: true },
    { to: `/projects/${projectId}/roles`, label: "Roles", end: false },
    { to: `/projects/${projectId}/authorizations`, label: "Authorizations", end: false },
  ];

  return (
    <nav aria-label="Project sections" className="mt-4 border-b border-border">
      <ul className="flex gap-1">
        {tabs.map((tab) => (
          <li key={tab.to}>
            <NavLink
              to={tab.to}
              end={tab.end}
              className={({ isActive }) =>
                // The active tab is marked by a border AND by weight, not by
                // colour alone — `docs/UI-UX/06`'s rule that colour is never
                // the sole carrier applies to navigation as much as to badges.
                `-mb-px inline-block border-b-2 px-4 py-2 text-body focus-visible:outline ` +
                `focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent ` +
                (isActive
                  ? "border-accent font-medium text-text-primary"
                  : "border-transparent text-text-secondary hover:text-text-primary")
              }
            >
              {tab.label}
            </NavLink>
          </li>
        ))}
      </ul>
    </nav>
  );
}
