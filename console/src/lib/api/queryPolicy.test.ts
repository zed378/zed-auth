import { describe, expect, it } from "vitest";

import { queryKeys } from "./client";
import { mutationInvalidations, queryStaleTime } from "./queryPolicy";

describe("query policies", () => {
  it("keeps audit and authorization data fresher than directory data", () => {
    expect(queryStaleTime.audit).toBeLessThan(queryStaleTime.directory);
    expect(queryStaleTime.access).toBeLessThan(queryStaleTime.directory);
    expect(queryStaleTime.sessions).toBeLessThanOrEqual(queryStaleTime.access);
  });

  it("scopes mutation invalidations to the active organization", () => {
    expect(mutationInvalidations.projects("org-1")).toEqual([[...queryKeys.projects, "org-1"]]);
    expect(mutationInvalidations.roles("org-1", "project-1")).toEqual([
      [...queryKeys.roles, "org-1", "project-1"],
    ]);
    expect(mutationInvalidations.users("org-2")).toEqual([[...queryKeys.users, "org-2"]]);
  });

  it("refreshes both sides of a delegated-grant change", () => {
    expect(mutationInvalidations.delegatedGrants("org-1", "grant-1")).toEqual([
      [...queryKeys.delegatedGrants, "org-1", "grant-1"],
      [...queryKeys.receivedGrants, "org-1"],
    ]);
  });
});
