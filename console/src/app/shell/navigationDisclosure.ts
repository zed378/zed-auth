/**
 * Relevance rules for advanced navigation concepts (PF-15).
 *
 * The route's authorization guard remains independent: these rules decide
 * whether a feature is relevant enough to advertise in navigation, never
 * whether a request or route is permitted.
 */
export interface DisclosureRule {
  /** Stable key understood by the data source that checks whether it exists. */
  feature: string;
  /** Manager roles that make the feature relevant before its first instance. */
  managerRoles: readonly string[];
}

export interface DisclosureContext {
  /** Feature keys for which at least one instance exists in the active org. */
  existingFeatures: ReadonlySet<string>;
  /** Roles applicable to the active organization, not every role in a token. */
  managerRoles: ReadonlySet<string>;
}

/**
 * Data-driven defaults. New advanced concepts add a rule here and a feature
 * existence signal; consumers do not grow another conditional navigation tree.
 */
export const ADVANCED_NAV_DISCLOSURE: readonly DisclosureRule[] = [
  {
    feature: "project-grants",
    managerRoles: ["ORG_OWNER", "ORG_ADMIN", "INSTANCE_OWNER"],
  },
  {
    feature: "abac-policies",
    managerRoles: ["ORG_OWNER", "ORG_ADMIN", "INSTANCE_OWNER"],
  },
];

/** Visible after its first instance exists, or when the active-org role applies. */
export function isDisclosureVisible(rule: DisclosureRule, context: DisclosureContext): boolean {
  if (context.existingFeatures.has(rule.feature)) return true;
  return rule.managerRoles.some((role) => context.managerRoles.has(role));
}

/** Filters arbitrary feature descriptors without coupling disclosure to routes. */
export function discloseAdvancedItems<T extends { feature?: string }>(
  items: readonly T[],
  context: DisclosureContext,
  rules: readonly DisclosureRule[] = ADVANCED_NAV_DISCLOSURE,
): T[] {
  const ruleByFeature = new Map(rules.map((rule) => [rule.feature, rule]));
  return items.filter((item) => {
    if (item.feature === undefined) return true;
    const rule = ruleByFeature.get(item.feature);
    // Unknown advanced feature keys fail closed; registering it requires an
    // explicit role/existence rule rather than silently exposing the item.
    return rule !== undefined && isDisclosureVisible(rule, context);
  });
}
