import { Link, useLocation } from "react-router-dom";

export interface BreadcrumbItem {
  label: string;
  to: string;
}

/** Keep the full identity in the link; only its visual presentation truncates. */
export function Breadcrumb({ items }: { items: readonly BreadcrumbItem[] }) {
  if (items.length === 0) return null;
  return (
    <nav aria-label="Breadcrumb">
      <ol className="flex min-w-0 flex-wrap items-center gap-2 text-small text-text-secondary">
        {items.map((item, index) => (
          <li key={`${item.to}-${index}`} className="flex min-w-0 items-center gap-2">
            {index > 0 && <span aria-hidden="true">/</span>}
            <Link
              to={item.to}
              aria-current={index === items.length - 1 ? "page" : undefined}
              title={item.label}
              className="max-w-xs truncate underline underline-offset-2 hover:text-text-primary focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
            >
              {item.label}
            </Link>
          </li>
        ))}
      </ol>
    </nav>
  );
}

export interface ConsoleBreadcrumbProps {
  organizationName: string;
  projectName?: string;
  userName?: string;
}

const sections: Record<string, string> = {
  projects: "Projects",
  "granted-projects": "Granted Projects",
  users: "Users",
  policies: "Policies",
  "audit-log": "Audit Log",
  settings: "Settings",
};

const projectTabs: Record<string, string> = {
  roles: "Roles",
  authorizations: "Authorizations",
  grants: "Project Grants",
};

/** Names come from the active organization's data, destinations from the route. */
export function ConsoleBreadcrumb({ organizationName, projectName, userName }: ConsoleBreadcrumbProps) {
  const { pathname } = useLocation();
  const parts = pathname.split("/").filter(Boolean);
  const section = Object.hasOwn(sections, parts[0]) ? sections[parts[0]] : undefined;
  if (parts.length > 0 && !section) return null;

  const items: BreadcrumbItem[] = [{ label: organizationName, to: "/" }];
  if (section) items.push({ label: section, to: `/${parts[0]}` });
  if (parts[0] === "projects" && parts[1]) {
    const projectPath = `/projects/${parts[1]}`;
    items.push({ label: projectName ?? parts[1], to: projectPath });
    if (parts.length === 2) items.push({ label: "Applications", to: pathname });
    else if (parts.length === 3 && Object.hasOwn(projectTabs, parts[2])) items.push({ label: projectTabs[parts[2]], to: pathname });
    else return null;
  } else if (parts[0] === "users" && parts[1] && parts.length === 2) {
    items.push({ label: userName ?? parts[1], to: pathname });
  } else if (parts.length > 1) return null;

  return <Breadcrumb items={items} />;
}
