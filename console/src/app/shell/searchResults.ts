import type { components } from "../../lib/api/schema.gen";

type AdministeredOrganization = components["schemas"]["AdministeredOrganization"];
type Project = components["schemas"]["Project"];
type User = components["schemas"]["User"];

export type GlobalSearchResult = {
  id: string;
  kind: "Organization" | "Project" | "User";
  title: string;
  detail: string;
  to: string;
};

const matches = (query: string, ...values: Array<string | null | undefined>) => {
  const needle = query.trim().toLocaleLowerCase();
  return needle.length > 0 && values.some((value) => value?.toLocaleLowerCase().includes(needle));
};

/** Build result groups from data the server already scoped to the caller. */
export function buildSearchResults({
  query,
  activeOrgId,
  organizations,
  projects,
  users,
}: {
  query: string;
  activeOrgId: string;
  organizations: readonly AdministeredOrganization[];
  projects: readonly Project[];
  users: readonly User[];
}): GlobalSearchResult[] {
  const orgs = organizations.length > 1
    ? organizations.filter((organization) => matches(query, organization.name, organization.id)).map((organization) => ({
      id: organization.id,
      kind: "Organization" as const,
      title: organization.name,
      detail: organization.id,
      to: `/?org=${encodeURIComponent(organization.id)}`,
    }))
    : [];
  const projectResults = projects.filter((project) => matches(query, project.name, project.id)).map((project) => ({
    id: project.id,
    kind: "Project" as const,
    title: project.name,
    detail: project.id,
    to: `/projects/${encodeURIComponent(project.id)}?org=${encodeURIComponent(activeOrgId)}`,
  }));
  const userResults = users.filter((user) => matches(query, user.display_name, user.username, user.email, user.id)).map((user) => ({
    id: user.id,
    kind: "User" as const,
    title: user.display_name || user.username || user.email,
    detail: user.email,
    to: `/users/${encodeURIComponent(user.id)}?org=${encodeURIComponent(activeOrgId)}`,
  }));
  return [...orgs, ...projectResults, ...userResults];
}

export function isResourceIdQuery(query: string): boolean {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(query.trim());
}
