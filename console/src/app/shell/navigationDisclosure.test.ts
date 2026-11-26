import { describe, expect, it } from "vitest";
import {
  ADVANCED_NAV_DISCLOSURE,
  discloseAdvancedItems,
  isDisclosureVisible,
  type DisclosureContext,
} from "./navigationDisclosure";

const grants = ADVANCED_NAV_DISCLOSURE.find((rule) => rule.feature === "project-grants")!;
const abac = ADVANCED_NAV_DISCLOSURE.find((rule) => rule.feature === "abac-policies")!;
const noAccess: DisclosureContext = { existingFeatures: new Set(), managerRoles: new Set() };

describe("progressive navigation disclosure", () => {
  it("reveals advanced concepts once an instance exists, even without a manager role", () => {
    const context = { ...noAccess, existingFeatures: new Set(["project-grants", "abac-policies"]) };
    expect(isDisclosureVisible(grants, context)).toBe(true);
    expect(isDisclosureVisible(abac, context)).toBe(true);
  });

  it("reveals first-time concepts to an applicable manager role", () => {
    expect(isDisclosureVisible(grants, { ...noAccess, managerRoles: new Set(["ORG_ADMIN"]) })).toBe(true);
    expect(isDisclosureVisible(abac, { ...noAccess, managerRoles: new Set(["ORG_OWNER"]) })).toBe(true);
  });

  it("keeps irrelevant concepts out without hiding ordinary navigation", () => {
    const items = [
      { label: "Projects" },
      { label: "Granted Projects", feature: "project-grants" },
      { label: "ABAC Policies", feature: "abac-policies" },
    ];
    expect(discloseAdvancedItems(items, noAccess)).toEqual([items[0]]);
  });

  it("fails closed for an advanced feature with no registered rule", () => {
    expect(discloseAdvancedItems([{ label: "New concept", feature: "unregistered" }], noAccess)).toEqual([]);
  });

  it("does not treat roles outside the feature's authorization scope as applicable", () => {
    expect(isDisclosureVisible(grants, { ...noAccess, managerRoles: new Set(["AUDITOR"]) })).toBe(false);
    expect(isDisclosureVisible(grants, { ...noAccess, managerRoles: new Set(["PROJECT_OWNER"]) })).toBe(false);
  });
});
