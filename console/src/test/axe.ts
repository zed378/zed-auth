import axe, { type ElementContext, type Result } from "axe-core";
import { expect } from "vitest";

/**
 * Asserts that a rendered tree has no axe violations.
 *
 * A hand-written helper rather than a matcher library: `vitest-axe` augments
 * the `Vi` global namespace, which Vitest 5 no longer uses, so its matcher
 * works at runtime and fails to typecheck. Calling axe-core directly removes
 * the layer that was only supplying a matcher.
 *
 * The failure message is the other reason. "expected no violations" tells
 * whoever broke it nothing; this prints the rule, its impact, the offending
 * selector, and the URL of the page explaining how to fix it — which is what
 * someone reading a red CI log actually needs.
 *
 * `UI-UX/13` § Testing & Sign-off asks for automated accessibility linting
 * catching regressions on every change. It also asks for a manual screen
 * reader pass before Phase 5, and this is not that: axe catches the mechanical
 * failures, not the ones that need judgement about whether an announcement
 * makes sense.
 */
export async function expectNoAxeViolations(container: ElementContext): Promise<void> {
  const results = await axe.run(container, {
    // The console targets WCAG 2.1 AA (UI-UX/13). Restricting the rule set to
    // that target keeps the suite honest: a best-practice warning failing the
    // build teaches people to disable the check.
    runOnly: { type: "tag", values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"] },
  });

  if (results.violations.length === 0) return;

  expect.fail(
    `${results.violations.length} accessibility violation(s):\n\n` +
      results.violations.map(describeViolation).join("\n\n"),
  );
}

function describeViolation(violation: Result): string {
  const nodes = violation.nodes
    .map((node) => `      ${node.target.join(" ")}\n        ${node.failureSummary ?? ""}`)
    .join("\n");

  return (
    `  [${violation.impact ?? "unknown"}] ${violation.id}: ${violation.help}\n` +
    `    ${violation.helpUrl}\n${nodes}`
  );
}
