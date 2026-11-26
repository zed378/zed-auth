import { describe, expect, it } from "vitest";
import { buildSearchResults, isResourceIdQuery } from "./searchResults";
import type { components } from "../../lib/api/schema.gen";

const org = (id: string, name: string): components["schemas"]["AdministeredOrganization"] => ({ id, name, roles: ["ORG_ADMIN"] });
const project = (id: string, name: string): components["schemas"]["Project"] => ({
  id, name, created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
});
const user = (id: string, email: string, display_name: string): components["schemas"]["User"] => ({
  id, email, display_name, status: "active", mfa_enabled: false, email_verified: true,
  created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
});

describe("global search results", () => {
  it("groups typed matches and uses server-visible organization data only", () => {
    const results = buildSearchResults({
      query: "billing",
      activeOrgId: "org-active",
      organizations: [org("org-1", "Billing North"), org("org-2", "Other")],
      projects: [project("project-1", "Billing API"), project("project-2", "Other")],
      users: [user("user-1", "billing@example.test", "Rina")],
    });
    expect(results.map(({ kind, title }) => [kind, title])).toEqual([
      ["Organization", "Billing North"], ["Project", "Billing API"], ["User", "Rina"],
    ]);
    expect(results[1].to).toContain("org=org-active");
  });

  it("omits organization results when the caller has no multi-org visibility", () => {
    const results = buildSearchResults({
      query: "acme", activeOrgId: "org-1", organizations: [org("org-1", "Acme")],
      projects: [], users: [],
    });
    expect(results).toEqual([]);
  });

  it("recognizes only complete UUID-like IDs for exact user lookups", () => {
    expect(isResourceIdQuery("123e4567-e89b-42d3-a456-426614174000")).toBe(true);
    expect(isResourceIdQuery("alice@example.test")).toBe(false);
  });
});
