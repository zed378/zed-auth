import { useQuery } from "@tanstack/react-query";

import { api, queryKeys } from "./client";
import { useAuth } from "../auth/AuthProvider";

/**
 * The reads the console does, in one place (P1-22).
 *
 * Every key is **scoped to the organization**. Without that, switching
 * organizations renders the previous one's cached data — a leak in the UI even
 * with a correct API, and the reason `PF-20` is on the watch list.
 */

/** The organization in the caller's token. Null before there is one. */
export function useOrgId(): string | null {
  const { claims } = useAuth();
  return claims?.orgId ?? null;
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
      const { data, error } = await api.GET("/v1/organizations/{org_id}/projects", {
        params: { path: { org_id: orgId as string }, query: { page_size: 100 } },
      });
      if (error !== undefined) throw asFailure(error);
      return data.projects;
    },
  });
}

export function useApplications(orgId: string | null, projectId: string | null) {
  return useQuery({
    queryKey: [...queryKeys.applications, orgId, projectId],
    enabled: orgId !== null && projectId !== null,
    queryFn: async () => {
      const { data, error } = await api.GET(
        "/v1/organizations/{org_id}/projects/{project_id}/applications",
        {
          params: {
            path: { org_id: orgId as string, project_id: projectId as string },
            query: { page_size: 100 },
          },
        },
      );
      if (error !== undefined) throw asFailure(error);
      return data.applications;
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
      const { data, error } = await api.GET(
        "/v1/organizations/{org_id}/projects/{project_id}/roles",
        {
          params: {
            path: { org_id: orgId as string, project_id: projectId as string },
            query: { page_size: 100 },
          },
        },
      );
      if (error !== undefined) throw asFailure(error);
      return data.roles;
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

export function useUsers(orgId: string | null, search: string) {
  return useQuery({
    queryKey: [...queryKeys.users, orgId, search],
    enabled: orgId !== null,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/organizations/{org_id}/users", {
        params: {
          path: { org_id: orgId as string },
          query: { page_size: 100, ...(search === "" ? {} : { search }) },
        },
      });
      if (error !== undefined) throw asFailure(error);
      return data.users;
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

function asFailure(error: unknown): ApiFailure {
  const envelope = error as { error?: { code?: string; message?: string } } | undefined;
  return new ApiFailure(
    envelope?.error?.code ?? "SERVER_ERROR",
    envelope?.error?.message ?? "The request failed.",
  );
}
