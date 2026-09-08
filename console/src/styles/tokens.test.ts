import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

import { contrastRatio } from "../branding/branding";

/**
 * These tests read tokens.css and compute against what is actually in it.
 *
 * The alternative — asserting a table of values written out here — would pass
 * forever while the stylesheet drifted, which is the failure mode the whole
 * token layer exists to prevent. The point is that changing a colour in
 * tokens.css runs this check against the new value.
 */
// Resolved from the project root rather than from `import.meta.url`: under
// jsdom that is an http:// URL, not a file:// one, so fileURLToPath throws.
const CSS = readFileSync(resolve(process.cwd(), "src/styles/tokens.css"), "utf8");

function token(name: string): string {
  // Comments in this file contain hex values as prose, so the match is
  // anchored to a declaration at the start of a line.
  const match = CSS.match(new RegExp(`^\\s*--${name}:\\s*([^;]+);`, "m"));
  if (!match) throw new Error(`token --${name} is not defined in tokens.css`);
  return match[1].trim();
}

function defined(name: string): boolean {
  return new RegExp(`^\\s*--${name}:`, "m").test(CSS);
}

// UI-UX/05-DESIGN-SYSTEM.md § Color, verbatim. Every one of the nine must
// exist by this name — the DoD says "every token in UI-UX/05 exists in code,
// by name", and a token silently renamed is a page spec that no longer maps
// onto anything.
const COLOR_TOKENS = [
  "color-bg-base",
  "color-bg-surface",
  "color-text-primary",
  "color-text-secondary",
  "color-accent",
  "color-danger",
  "color-warning",
  "color-success",
  "color-border",
] as const;

const TYPE_TOKENS = [
  "font-family-base",
  "font-size-small",
  "font-size-body",
  "font-size-heading-1",
  "font-size-heading-2",
  "font-size-heading-3",
  "font-weight-regular",
  "font-weight-medium",
  "font-weight-bold",
] as const;

const SPACING_TOKENS = [
  "spacing-1",
  "spacing-2",
  "spacing-3",
  "spacing-4",
  "spacing-5",
  "spacing-6",
  "spacing-7",
  "spacing-8",
] as const;

// UI-UX/05 § Elevation: flat, raised, overlay. Three, not a scale.
const ELEVATION_TOKENS = ["shadow-flat", "shadow-raised", "shadow-overlay"] as const;

describe("every token UI-UX/05 names exists, by name", () => {
  it.each([...COLOR_TOKENS, ...TYPE_TOKENS, ...SPACING_TOKENS, ...ELEVATION_TOKENS])(
    "--%s",
    (name) => {
      expect(defined(name)).toBe(true);
    },
  );

  it("has exactly the nine colour tokens, no more", () => {
    // A tenth colour token is a design-system change, and UI-UX/05 §
    // Governance requires that to happen in the design system before a screen
    // uses it. This fails on a colour added here first.
    const declared = [...CSS.matchAll(/^\s*--(color-[a-z-]+):/gm)].map((m) => m[1]);

    expect(new Set(declared)).toEqual(new Set(COLOR_TOKENS));
  });

  it("keeps the type scale to five steps", () => {
    // UI-UX/05 asks for 5-6, "not an open-ended set".
    const sizes = [...CSS.matchAll(/^\s*--(font-size-[a-z0-9-]+):/gm)].map((m) => m[1]);

    expect(sizes).toHaveLength(5);
  });
});

describe("text meets WCAG 2.1 AA against both background tokens", () => {
  // UI-UX/05 states this for color-text-*, and UI-UX/13 § Colour & Contrast
  // makes it a requirement of the accessibility target. Checked against BOTH
  // backgrounds because a token that passes on the page but fails inside a
  // card is a failure on every table in the console.
  const backgrounds = [
    ["bg-base", token("color-bg-base")],
    ["bg-surface", token("color-bg-surface")],
  ] as const;

  const textLike = [
    "color-text-primary",
    "color-text-secondary",
    "color-accent",
    "color-danger",
    "color-warning",
    "color-success",
  ] as const;

  for (const [bgName, bg] of backgrounds) {
    it.each(textLike)(`--%s on --color-${bgName}`, (name) => {
      const ratio = contrastRatio(token(name), bg);

      expect(ratio).not.toBeNull();
      expect(ratio!).toBeGreaterThanOrEqual(4.5);
    });
  }
});

describe("borders meet WCAG 2.1 AA non-text contrast", () => {
  // 1.4.11: 3:1 for the visual information that identifies a UI component. An
  // input's border is exactly that, and UI-UX/05 gives one token for both
  // input borders and table dividers — so the single token errs toward the
  // accessible reading. See BACKLOG PG-12 for the proposed split.
  it.each([
    ["bg-base", "color-bg-base"],
    ["bg-surface", "color-bg-surface"],
  ])("--color-border on --color-%s", (_label, bgToken) => {
    const ratio = contrastRatio(token("color-border"), token(bgToken));

    expect(ratio).not.toBeNull();
    expect(ratio!).toBeGreaterThanOrEqual(3);
  });
});

describe("the accessibility rules that are easy to delete by accident", () => {
  it("defines a focus ring using the accent token", () => {
    // UI-UX/13: visible focus indicators on every focusable element, using
    // color-accent, "never suppressed for aesthetic reasons". The most common
    // way this regresses is someone removing the outline to tidy a design.
    expect(CSS).toMatch(/:focus-visible\s*\{/);
    expect(CSS).toMatch(/outline:\s*2px solid var\(--color-accent\)/);
  });

  it("never sets outline: none", () => {
    expect(CSS).not.toMatch(/outline:\s*(none|0)\b/);
  });

  it("honours prefers-reduced-motion", () => {
    // UI-UX/13 § Motion calls this "a hard requirement, not a nice-to-have".
    expect(CSS).toMatch(/@media \(prefers-reduced-motion: reduce\)/);
  });
});
