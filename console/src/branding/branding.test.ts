import { describe, expect, it } from "vitest";

import {
  applyBranding,
  brandingFromApiResponse,
  contrastRatio,
  isBrandableToken,
  parseHexColour,
  RESERVED_TOKENS,
  validateBranding,
} from "./branding";

/** A stand-in for an element's style, recording what was set on it. */
function styleSpy() {
  const set = new Map<string, string>();
  return {
    set,
    style: {
      setProperty(name: string, value: string) {
        set.set(name, value);
      },
    },
  };
}

describe("branding is limited to the accent colour and the logo", () => {
  // P0-17's Definition of Done, and one of CLAUDE.md's non-negotiable
  // constraints. `color-danger` means "irreversible"; a tenant able to repaint
  // it to their brand green would make that word mean nothing, in the console
  // where someone is about to delete an organization.
  it.each(RESERVED_TOKENS)("refuses to apply %s", (token) => {
    const target = styleSpy();

    applyBranding(target, {
      // Cast through unknown deliberately: the type already forbids this, and
      // the point of the test is the runtime guard behind the type. Branding
      // arrives as JSON from an API, where the type system has already ended.
      applied: { [token]: "#00ff00" } as unknown as Record<"--color-accent", string>,
      rejected: [],
    });

    expect(target.set.has(token)).toBe(false);
    expect(target.set.size).toBe(0);
  });

  it("applies the accent colour", () => {
    const target = styleSpy();

    applyBranding(target, { applied: { "--color-accent": "#1d4ed8" }, rejected: [] });

    expect(target.set.get("--color-accent")).toBe("#1d4ed8");
  });

  it("reports a reserved token from an API response rather than dropping it", () => {
    // Silence would leave an org admin believing their custom danger colour
    // took effect. The rejection is what the settings screen surfaces.
    const result = brandingFromApiResponse({
      accentColor: "#1d4ed8",
      "--color-danger": "#00ff00",
    });

    expect(result.applied).toEqual({ "--color-accent": "#1d4ed8" });
    expect(result.rejected).toContainEqual({
      reason: "reserved-token",
      token: "--color-danger",
    });
  });

  it("treats only the accent as brandable", () => {
    expect(isBrandableToken("--color-accent")).toBe(true);
    for (const token of RESERVED_TOKENS) {
      expect(isBrandableToken(token)).toBe(false);
    }
    expect(isBrandableToken("--color-text-primary")).toBe(false);
    expect(isBrandableToken("--spacing-4")).toBe(false);
  });
});

describe("a custom accent is contrast-checked before it is applied", () => {
  // UI-UX/13 § Colour & Contrast: validate at the point an org admin sets the
  // colour, "rather than allowing an inaccessible combination to ship
  // silently". The accent is the focus ring, so an unreadable one is an
  // accessibility failure on every screen at once.
  it("accepts an accent that meets AA against the surface", () => {
    const result = validateBranding({ accentColor: "#1d4ed8" }, "#ffffff");

    expect(result.applied["--color-accent"]).toBe("#1d4ed8");
    expect(result.rejected).toEqual([]);
  });

  it("rejects an accent that does not, and says by how much", () => {
    // A mid-yellow: plausible as a brand colour, unreadable as a focus ring.
    const result = validateBranding({ accentColor: "#ffd21e" }, "#ffffff");

    expect(result.applied).toEqual({});
    expect(result.rejected).toHaveLength(1);

    const [rejection] = result.rejected;
    expect(rejection.reason).toBe("insufficient-contrast");
    if (rejection.reason === "insufficient-contrast") {
      expect(rejection.ratio).toBeLessThan(4.5);
      expect(rejection.required).toBe(4.5);
    }
  });

  it("checks against the surface it is given, not an assumed white", () => {
    // The same colour passes on one background and fails on another, which is
    // the whole reason the surface is a parameter.
    const onWhite = validateBranding({ accentColor: "#767676" }, "#ffffff");
    const onDark = validateBranding({ accentColor: "#767676" }, "#15181d");

    expect(onWhite.rejected).toEqual([]);
    expect(onDark.rejected).toHaveLength(1);
  });

  it("rejects a colour it cannot parse rather than guessing", () => {
    for (const value of ["rebeccapurple", "rgb(29 78 216)", "var(--x)", "#12345"]) {
      const result = validateBranding({ accentColor: value }, "#ffffff");
      expect(result.rejected).toEqual([{ reason: "unparseable-colour", value }]);
    }
  });
});

describe("contrast maths", () => {
  // Anchored against the two ratios WCAG itself fixes, so an error in the
  // luminance formula cannot hide behind plausible-looking numbers.
  it("puts black on white at 21:1 and a colour on itself at 1:1", () => {
    expect(contrastRatio("#000000", "#ffffff")).toBeCloseTo(21, 5);
    expect(contrastRatio("#1d4ed8", "#1d4ed8")).toBeCloseTo(1, 5);
  });

  it("is symmetric", () => {
    expect(contrastRatio("#1d4ed8", "#ffffff")).toBeCloseTo(
      contrastRatio("#ffffff", "#1d4ed8")!,
      10,
    );
  });

  it("expands three-digit hex the same way CSS does", () => {
    expect(parseHexColour("#fff")).toEqual([255, 255, 255]);
    expect(parseHexColour("#1a2")).toEqual([0x11, 0xaa, 0x22]);
    expect(parseHexColour("1d4ed8")).toEqual([0x1d, 0x4e, 0xd8]);
    expect(parseHexColour("#xyzxyz")).toBeNull();
  });
});
