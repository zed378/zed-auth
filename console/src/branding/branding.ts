/**
 * Per-organization branding.
 *
 * `PLAN/01-PRODUCT-SCOPE.md` and `UI-UX/05-DESIGN-SYSTEM.md` allow an
 * organization to override the accent colour and the logo. Nothing else.
 *
 * The interesting part is what may *not* be overridden, and why the rule is
 * expressed as a type rather than as a comment:
 *
 *   `color-danger` and `color-warning` must stay universally recognizable
 *   regardless of tenant branding (UI-UX/05 § Color). An admin who manages
 *   several organizations should never have to relearn what red means
 *   (UI-UX/06 § What "On-Brand" Means Here). If a tenant could set danger to
 *   their brand green, the colour that means "this is irreversible" would
 *   mean nothing at all — and it would mean nothing precisely in the console
 *   where someone is about to delete an organization.
 *
 * So the overridable set is a closed union of two token names. Adding a third
 * is a type change in this file, which is a code review, rather than a value
 * that happens to arrive from an API response.
 */

/**
 * The only design tokens an organization may override.
 *
 * A union of literals rather than `string`, so `applyBranding` cannot be
 * called with `--color-danger` even by a caller that has one in a variable.
 */
export type BrandableToken = "--color-accent";

export const BRANDABLE_TOKENS: readonly BrandableToken[] = ["--color-accent"] as const;

/**
 * Tokens that are never overridable, listed explicitly.
 *
 * Redundant with the type above — `BrandableToken` already excludes them — and
 * kept anyway for two reasons. It documents the intent where someone reading
 * `applyBranding` will see it, and it gives the runtime guard something to
 * name in its error message. The type protects compiled callers; the guard
 * protects against branding arriving as untyped JSON from an API, which is
 * exactly how it will arrive.
 */
export const RESERVED_TOKENS = [
  "--color-danger",
  "--color-warning",
  "--color-success",
] as const;

export interface OrganizationBranding {
  /** CSS colour for `--color-accent`. Contrast-checked before it is applied. */
  accentColor?: string;
  /** URL of the organization's logo. */
  logoUrl?: string;
}

/** Why a branding value was refused. Surfaced to the org admin who set it. */
export type BrandingRejection =
  | { reason: "reserved-token"; token: string }
  | { reason: "unparseable-colour"; value: string }
  | { reason: "insufficient-contrast"; value: string; ratio: number; required: number };

export interface BrandingResult {
  applied: Partial<Record<BrandableToken, string>>;
  rejected: BrandingRejection[];
}

/**
 * WCAG 2.1 AA for normal text.
 *
 * The accent is used for the focus ring and for primary-action text, so it has
 * to be legible rather than merely on-brand.
 */
const MIN_CONTRAST = 4.5;

/** Relative luminance, per the WCAG 2.1 definition. */
function relativeLuminance(rgb: readonly [number, number, number]): number {
  const [r, g, b] = rgb.map((channel) => {
    const c = channel / 255;
    return c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
  }) as [number, number, number];

  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

/** Contrast ratio between two colours, per WCAG 2.1. */
export function contrastRatio(a: string, b: string): number | null {
  const rgbA = parseHexColour(a);
  const rgbB = parseHexColour(b);
  if (!rgbA || !rgbB) return null;

  const lumA = relativeLuminance(rgbA);
  const lumB = relativeLuminance(rgbB);
  const lighter = Math.max(lumA, lumB);
  const darker = Math.min(lumA, lumB);

  return (lighter + 0.05) / (darker + 0.05);
}

/**
 * Parses `#rgb` and `#rrggbb`.
 *
 * Deliberately narrow. Accepting `rgb()`, `hsl()`, named colours and
 * `color-mix()` would mean either reimplementing CSS colour parsing or handing
 * the value to the browser to resolve — and the browser cannot tell us the
 * contrast ratio of something it has not painted yet. A branding field that
 * accepts only hex is a small restriction on an org admin and the difference
 * between checking contrast and hoping.
 */
export function parseHexColour(value: string): [number, number, number] | null {
  const hex = value.trim().replace(/^#/, "");

  if (/^[0-9a-f]{3}$/i.test(hex)) {
    const [r, g, b] = hex.split("");
    return [
      parseInt(r + r, 16),
      parseInt(g + g, 16),
      parseInt(b + b, 16),
    ];
  }

  if (/^[0-9a-f]{6}$/i.test(hex)) {
    return [
      parseInt(hex.slice(0, 2), 16),
      parseInt(hex.slice(2, 4), 16),
      parseInt(hex.slice(4, 6), 16),
    ];
  }

  return null;
}

/**
 * Validates branding without applying it.
 *
 * Separated from application so the org-admin settings screen can show the
 * rejection at the moment the colour is chosen. `UI-UX/13` § Colour & Contrast
 * requires exactly that: validate at the point an admin sets it, "rather than
 * allowing an inaccessible combination to ship silently".
 *
 * @param surface the background the accent will sit against, so the caller
 *   checks against the real surface rather than an assumed white.
 */
export function validateBranding(
  branding: OrganizationBranding,
  surface: string,
): BrandingResult {
  const applied: Partial<Record<BrandableToken, string>> = {};
  const rejected: BrandingRejection[] = [];

  if (branding.accentColor !== undefined) {
    const ratio = contrastRatio(branding.accentColor, surface);

    if (ratio === null) {
      rejected.push({ reason: "unparseable-colour", value: branding.accentColor });
    } else if (ratio < MIN_CONTRAST) {
      rejected.push({
        reason: "insufficient-contrast",
        value: branding.accentColor,
        ratio,
        required: MIN_CONTRAST,
      });
    } else {
      applied["--color-accent"] = branding.accentColor;
    }
  }

  return { applied, rejected };
}

/**
 * Applies validated branding to a DOM element, usually `document.documentElement`.
 *
 * Sets only tokens in `BRANDABLE_TOKENS`. The filter is not defensive
 * programming for its own sake: branding arrives from an API response, and
 * `validateBranding`'s type safety stops at the boundary where JSON becomes an
 * object. A response carrying `"--color-danger": "#00ff00"` must not be able
 * to repaint the colour that means "irreversible".
 */
export function applyBranding(
  target: { style: { setProperty(name: string, value: string): void } },
  result: BrandingResult,
): void {
  for (const [token, value] of Object.entries(result.applied)) {
    if (!isBrandableToken(token)) continue;
    target.style.setProperty(token, value);
  }
}

export function isBrandableToken(token: string): token is BrandableToken {
  return (BRANDABLE_TOKENS as readonly string[]).includes(token);
}

/**
 * The path branding actually takes from an API response.
 *
 * Untyped input, because that is what a network response is. Anything not
 * brandable is refused and reported rather than dropped silently — an org
 * admin who set a custom danger colour should be told it was refused and why,
 * not left believing it worked.
 */
export function brandingFromApiResponse(payload: unknown): BrandingResult {
  const rejected: BrandingRejection[] = [];

  if (typeof payload !== "object" || payload === null) {
    return { applied: {}, rejected };
  }

  const record = payload as Record<string, unknown>;

  for (const token of RESERVED_TOKENS) {
    if (token in record) {
      rejected.push({ reason: "reserved-token", token });
    }
  }

  const branding: OrganizationBranding = {};
  if (typeof record.accentColor === "string") branding.accentColor = record.accentColor;
  if (typeof record.logoUrl === "string") branding.logoUrl = record.logoUrl;

  const result = validateBranding(branding, "#ffffff");
  return { applied: result.applied, rejected: [...rejected, ...result.rejected] };
}
