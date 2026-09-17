import { useQuery } from "@tanstack/react-query";

import { api, queryKeys } from "./client";
import { useOrgContext } from "../org/OrgProvider";

/**
 * The reads the console does, in one place (P1-22).
 *
 * Every key is **scoped to the organization**. Without that, switching
 * organizations renders the previous one's cached data — a leak in the UI even
 * with a correct API, and the reason `PF-20` is on the watch list.
 */

/**
 * Every page of a collection, up to a bound.
 *
 * Each list hook used to ask for one page of 100 and ignore `page_info`. The
 * API orders most collections oldest first, so past 100 rows the NEWEST were
 * the ones missing — an administrator who had just invited somebody could not
 * find them in the list, and nothing said the list was incomplete. Counts
 * built from those lists (the Overview's active users) were wrong the same way.
 *
 * Following the tokens is the fix; the bound is there so a runaway collection
 * or a server that kept answering a token cannot hang the screen. `complete`
 * says whether the bound was reached, and a screen that shows a list or a
 * count must say so when it was.
 */
export const MAX_PAGES = 20;
export const PAGE_SIZE = 100;

export interface Collected<T> {
  items: T[];
  complete: boolean;
}

export async function collectPages<T>(
  fetchPage: (pageToken: string | undefined) => Promise<{
    items: T[];
    next: string | null | undefined;
  }>,
  maxPages = MAX_PAGES,
): Promise<Collected<T>> {
  const items: T[] = [];
  let token: string | undefined;
  for (let page = 0; page < maxPages; page++) {
    const { items: batch, next } = await fetchPage(token);
    items.push(...batch);
    if (next === undefined || next === null || next === "") return { items, complete: true };
    token = next;
  }
  return { items, complete: false };
}

const tokenParam = (token: string | undefined) => (token === undefined ? {} : { page_token: token });

/**
 * The organization the console is acting in. Null before sign-in.
 *
 * The token's `org_id` until `P2-13`; now the **active** organization, which
 * is the token's unless an administrator has switched (`?org=`). Every query
 * key below is built from it, so switching re-scopes every read without a
 * single call site changing — and the switch clears the cache anyway, so a
 * hook that forgot to include it could not serve the previous tenant's rows.
 */
export function useOrgId(): string | null {
  return useOrgContext().orgId;
}

/**
 * The organizations this caller administers (P2-13).
 *
 * From `GET /v1/me/organizations`, never from the token's
 * `urn:authservice:manager_roles` claim: that claim carries role names without
 * their scopes, so it cannot name an organization at all, and a role in a token
 * is a snapshot up to ten minutes stale.
 *
 * NOT keyed by organization. It is a property of the caller, not of the
 * context they are in, and re-fetching it on every switch would make the
 * switcher empty itself the moment it is used.
 */
export function useAdministeredOrganizations(enabled: boolean) {
  return useQuery({
    queryKey: [...queryKeys.administered],
    enabled,
    queryFn: async () => {
      const all = await collectPages(async (token) => {
        const { data, error } = await api.GET("/v1/me/organizations", {
          params: { query: { page_size: PAGE_SIZE, ...tokenParam(token) } },
        });
        if (error !== undefined) throw asFailure(error);
        return { items: data.organizations, next: data.page_info?.next_page_token };
      });
      return all.items;
    },
  });
}

export function useOrganization(orgId: string | null) {
  return useQuery({
    queryKey: [...queryKeys.organizations, orgId],
    enabled: orgId !== null,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/organizations/{org_id}", {
        params: { path: { org_id: orgId as string } },
      });
      if (error !== undefined) throw asFailure(error);
      return data;
    },
  });
}

export function useProjects(orgId: string | null) {
  return useQuery({
    queryKey: [...queryKeys.projects, orgId],
    enabled: orgId !== null,
    queryFn: async () => {
      const all = await collectPages(async (token) => {
        const { data, error } = await api.GET("/v1/organizations/{org_id}/projects", {
          params: {
            path: { org_id: orgId as string },
            query: { page_size: PAGE_SIZE, ...tokenParam(token) },
          },
        });
        if (error !== undefined) throw asFailure(error);
        return { items: data.projects, next: data.page_info?.next_page_token };
      });
      return all.items;
    },
  });
}

export function useApplications(orgId: string | null, projectId: string | null) {
  return useQuery({
    queryKey: [...queryKeys.applications, orgId, projectId],
    enabled: orgId !== null && projectId !== null,
    queryFn: async () => {
      const all = await collectPages(async (token) => {
        const { data, error } = await api.GET(
          "/v1/organizations/{org_id}/projects/{project_id}/applications",
          {
            params: {
              path: { org_id: orgId as string, project_id: projectId as string },
              query: { page_size: PAGE_SIZE, ...tokenParam(token) },
            },
          },
        );
        if (error !== undefined) throw asFailure(error);
        return { items: data.applications, next: data.page_info?.next_page_token };
      });
      return all.items;
    },
  });
}

/**
 * A project's roles (P2-11).
 *
 * Ordered by `key` by the server rather than by creation time: a role list is
 * read as a reference table, and somebody looking for `billing-admin` should
 * not have to know when it was defined.
 *
 * Each row carries `grant_count`, which is what lets the delete confirmation
 * state the consequence before the request instead of discovering the server's
 * refusal afterwards.
 */
export function useRoles(orgId: string | null, projectId: string | null) {
  return useQuery({
    queryKey: [...queryKeys.roles, orgId, projectId],
    enabled: orgId !== null && projectId !== null,
    queryFn: async () => {
      const all = await collectPages(async (token) => {
        const { data, error } = await api.GET(
          "/v1/organizations/{org_id}/projects/{project_id}/roles",
          {
            params: {
              path: { org_id: orgId as string, project_id: projectId as string },
              query: { page_size: PAGE_SIZE, ...tokenParam(token) },
            },
          },
        );
        if (error !== undefined) throw asFailure(error);
        return { items: data.roles, next: data.page_info?.next_page_token };
      });
      return all.items;
    },
  });
}

/**
 * The Project Grants a project has given, active and revoked (P4-05).
 *
 * Revoked grants are listed on purpose: a delegation that ended last week is
 * the answer to "why can the partner no longer assign that role", and hiding it
 * would make the table read as though it never existed.
 */
export function useProjectGrants(orgId: string | null, projectId: string | null) {
  return useQuery({
    queryKey: [...queryKeys.projectGrants, orgId, projectId],
    enabled: orgId !== null && projectId !== null,
    queryFn: async () => {
      const all = await collectPages(async (token) => {
        const { data, error } = await api.GET(
          "/v1/organizations/{org_id}/projects/{project_id}/grants",
          {
            params: {
              path: { org_id: orgId as string, project_id: projectId as string },
              query: { page_size: PAGE_SIZE, ...tokenParam(token) },
            },
          },
        );
        if (error !== undefined) throw asFailure(error);
        return { items: data.grants, next: data.page_info?.next_page_token };
      });
      return all.items;
    },
  });
}

/**
 * Every grant one user holds, across every project (P2-12).
 *
 * Addressed by user rather than by project because that is the only shape the
 * API offers: `docs/PLAN/04` stores one row per user per project, and the
 * Management API exposes it under the user. A project-wide roster would need
 * an endpoint that does not exist — see `PG-36`.
 *
 * An empty list is the **normal** state for a new account, not an error:
 * `docs/PLAN/08` § Least Privilege means a user with no grant has no access at
 * all, and there is no implicit default role.
 */
export function useUserGrants(orgId: string | null, userId: string | null) {
  return useQuery({
    queryKey: [...queryKeys.grants, orgId, userId],
    enabled: orgId !== null && userId !== null,
    queryFn: async () => {
      const { data, error } = await api.GET(
        "/v1/organizations/{org_id}/users/{user_id}/grants",
        { params: { path: { org_id: orgId as string, user_id: userId as string } } },
      );
      if (error !== undefined) throw asFailure(error);
      return data.grants;
    },
  });
}

/**
 * The caller's own second factors (P3-10).
 *
 * Keyed on the organization like every other key (PF-20), even though the
 * route takes the user from the token: switching organizations switches the
 * token, and with it whose factors these are.
 */
export function useMyMfa(orgId: string | null, enabled: boolean) {
  return useQuery({
    queryKey: [...queryKeys.mfa, orgId, "me"],
    enabled: orgId !== null && enabled,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/me/mfa");
      if (error !== undefined) throw asFailure(error);
      return data;
    },
  });
}

/** A member's factors, read-only, for an administrator (P3-10). */
export function useUserMfa(orgId: string | null, userId: string | null, enabled: boolean) {
  return useQuery({
    queryKey: [...queryKeys.mfa, orgId, userId],
    enabled: orgId !== null && userId !== null && enabled,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/organizations/{org_id}/users/{user_id}/mfa", {
        params: { path: { org_id: orgId as string, user_id: userId as string } },
      });
      if (error !== undefined) throw asFailure(error);
      return data;
    },
  });
}

export function useUsers(orgId: string | null, search: string) {
  return useQuery({
    queryKey: [...queryKeys.users, orgId, search],
    enabled: orgId !== null,
    queryFn: async () => {
      // `complete` travels with the rows: this is the one collection an
      // organization realistically outgrows the bound on, and both the list
      // and the Overview's counts have to say so when it has.
      return collectPages(async (token) => {
        const { data, error } = await api.GET("/v1/organizations/{org_id}/users", {
          params: {
            path: { org_id: orgId as string },
            query: {
              page_size: PAGE_SIZE,
              ...(search === "" ? {} : { search }),
              ...tokenParam(token),
            },
          },
        });
        if (error !== undefined) throw asFailure(error);
        return { items: data.users, next: data.page_info?.next_page_token };
      });
    },
  });
}

export function useEvents(orgId: string | null, eventTypes: string[]) {
  return useQuery({
    queryKey: [...queryKeys.events, orgId, eventTypes.join(",")],
    enabled: orgId !== null,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/organizations/{org_id}/events", {
        params: {
          path: { org_id: orgId as string },
          query: { page_size: 50, ...(eventTypes.length === 0 ? {} : { event_type: eventTypes }) },
        },
      });
      if (error !== undefined) throw asFailure(error);
      return data.events;
    },
  });
}

/**
 * A failure the UI can tell apart.
 *
 * `docs/UI-UX/14` needs a network failure, a server failure and a permission
 * refusal to render differently, and openapi-fetch hands back the envelope
 * rather than an exception. Turning it into a typed error here means every
 * screen gets the distinction without each one parsing the envelope.
 */
export class ApiFailure extends Error {
  readonly code: string;

  constructor(code: string, message: string) {
    super(message);
    this.name = "ApiFailure";
    this.code = code;
  }

  get kind(): "network" | "server" | "permission" | "validation" {
    switch (this.code) {
      case "PERMISSION_DENIED":
      case "NOT_FOUND":
        return "permission";
      case "VALIDATION_ERROR":
      case "CONFLICT":
        return "validation";
      case "NETWORK":
        return "network";
      default:
        return "server";
    }
  }
}

export function asFailure(error: unknown): ApiFailure {
  const envelope = error as { error?: { code?: string; message?: string } } | undefined;
  return new ApiFailure(
    envelope?.error?.code ?? "SERVER_ERROR",
    envelope?.error?.message ?? "The request failed.",
  );
}
