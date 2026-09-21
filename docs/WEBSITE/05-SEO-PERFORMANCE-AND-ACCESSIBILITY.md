# 05 - SEO, Performance and Accessibility

> Category: **Public Website** (`docs/WEBSITE/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: P0-18, P0-19 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

What the site does about discovery, speed and access, what is measured, and what is
currently assumed. The status is `Partially implemented` because performance is assumed
rather than measured.

## Scope

`public-site/docusaurus.config.ts`, `public-site/scripts/write-robots.mjs`,
`public-site/scripts/check-contrast.mjs`, `public-site/src/css/custom.css`.

## As Built

### The origin is configured once and reused everywhere

`SITE_URL` feeds Docusaurus's canonical URLs, its sitemap, **and** the generated
`robots.txt`. They cannot disagree, which is the point:

> The `Sitemap:` directive has to be an absolute URL, so a static `robots.txt` has to
> hard-code a host — and the first deploy to a hostname other than the guessed one
> publishes a sitemap pointer to somewhere that does not exist. That happened: the site
> went live at `app-auth.zedth.my.id` advertising a sitemap at `zedth.my.id`.

And the failure mode is the reason it is worth a script:

> It fails quietly. Nothing 404s for a visitor; a crawler follows the pointer, finds
> nothing, and the site is simply not indexed — on the surface whose entire job is
> discovery.

`robots.txt` is written at build time by `public-site/scripts/write-robots.mjs`.

### Contrast is computed from the stylesheet, in both themes

`public-site/scripts/check-contrast.mjs` parses `public-site/src/css/custom.css` and
computes ratios against **WCAG 2.1 AA**: 4.5:1 for normal text, 3:1 for the boundary of a
UI component (1.4.11).

It exists because a dark mode was shipped that looked deliberate and was unreadable —
measured accent 2.4:1, danger 2.7:1, warning 3.3:1, success 2.8:1 against the dark surface.
**Every one of those was found by measuring rather than by looking.**

Ratios are computed from the stylesheet, so changing a value runs the check against the new
value rather than against the script's assumptions.

### Brand tokens are checked against the console's

`public-site/scripts/check-brand-tokens.mjs`. The two surfaces share no code — that is
enforced separately — so a check that compares values is what keeps them from drifting into
two slightly different blues.

### Static output, system fonts, no client-side framework on the marketing pages

Docusaurus produces static HTML. The landing page is React/MDX rendered at build time.
There is no webfont; a flash of unstyled text on the page whose job is a first impression is
a poor trade for personality.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Canonical URL, sitemap and robots | one `SITE_URL` | `public-site/docusaurus.config.ts`, `write-robots.mjs` |
| Contrast | WCAG 2.1 AA, both themes, computed | `public-site/scripts/check-contrast.mjs` |
| Brand tokens | match the console's | `public-site/scripts/check-brand-tokens.mjs` |
| Shared code with the console | none | `public-site/scripts/check-no-shared-code.mjs` |

## Verification

- `npm run check` in `public-site/` — tokens, contrast, boundary, typecheck, build, claims,
  leak.
- `scripts/check.sh` — the same, with the slow parts behind `CHECK_FULL=1`.

## Not Yet Built / Open Questions

- **Performance is not measured.** No Lighthouse run, no budget, no Core Web Vitals
  tracking. Static output and system fonts make it *likely* fine, and "likely fine" is not
  a measurement.
- **Accessibility is contrast-only.** No axe run over the built pages — the console has one
  and this site does not — and no screen-reader pass.
- **No structured data** (`Organization`, `SoftwareApplication`) and no Open Graph audit.
- **No analytics**, so there is no evidence about which pages are read.

## Related Documents

- [`06-BUILD-AND-DEPLOYMENT.md`](./06-BUILD-AND-DEPLOYMENT.md)
- [`07-LAUNCH-CHECKLIST.md`](./07-LAUNCH-CHECKLIST.md)
- [`../FRONTEND/09-ACCESSIBILITY-PRACTICE.md`](../FRONTEND/09-ACCESSIBILITY-PRACTICE.md)
