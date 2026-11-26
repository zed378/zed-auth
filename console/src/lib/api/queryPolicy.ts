import type { QueryClient, QueryKey } from "@tanstack/react-query";

import { queryKeys } from "./client";

export const queryStaleTime = {
  directory: 30_000,
  configuration: 60_000,
  access: 10_000,
  sessions: 5_000,
  audit: 0,
} as const;

type Invalidation = readonly QueryKey[];

export const mutationInvalidations = {
  applications: (orgId: string | null, projectId: string | null): Invalidation => [
    [...queryKeys.applications, orgId, projectId],
  ],
  grants: (orgId: string | null, userId: string): Invalidation => [
    [...queryKeys.grants, orgId, userId],
  ],
  delegatedGrants: (orgId: string | null, grantId: string | null): Invalidation => [
    [...queryKeys.delegatedGrants, orgId, grantId],
    [...queryKeys.receivedGrants, orgId],
  ],
  mfa: (orgId: string | null): Invalidation => [[...queryKeys.mfa, orgId]],
  organization: (orgId: string | null): Invalidation => [[...queryKeys.organizations, orgId]],
  projects: (orgId: string | null): Invalidation => [[...queryKeys.projects, orgId]],
  projectGrants: (orgId: string | null, projectId: string | null): Invalidation => [
    [...queryKeys.projectGrants, orgId, projectId],
  ],
  roles: (orgId: string | null, projectId: string | null): Invalidation => [
    [...queryKeys.roles, orgId, projectId],
  ],
  sessions: (orgId: string | null, actor: string): Invalidation => [
    [...queryKeys.sessions, orgId, actor],
  ],
  users: (orgId: string | null): Invalidation => [[...queryKeys.users, orgId]],
} as const;

export function invalidateQueries(queryClient: QueryClient, keys: Invalidation): Promise<void[]> {
  return Promise.all(keys.map((queryKey) => queryClient.invalidateQueries({ queryKey })));
}
