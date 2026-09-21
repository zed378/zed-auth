# 01 - Information Architecture

> Category: **Public Website** (`docs/WEBSITE/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-18, P0-19, P1-25, ADR-014 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

Every page the site has, how they are navigated, and why the marketing pages and the docs
are one Docusaurus project rather than two applications.

## Scope

`public-site/`.

## As Built

### One project, not two — ADR-014

`docs/PLAN/20` suggests a marketing static-site generator alongside a separate docs
framework. `docs/UI-UX/20` § Cross-Page Requirements requires every page to share the same
header and footer so that moving Landing → Docs "never feels like a different product".

Two projects make that shared navigation a duplicated component, which is precisely how it
drifts. So: one Docusaurus site, with the marketing pages as ordinary React/MDX. They could
move to a separate build later without touching the docs.

### The pages

```
/                     landing               public-site/src/pages/index.tsx
/about                                      public-site/src/pages/about.md
/contact                                    public-site/src/pages/contact.md
/changelog            one entry per phase   public-site/changelog/
/docs                 the docs home         public-site/docs/index.md
/docs/quickstart                            public-site/docs/quickstart.md
/docs/concepts/…      model, authorization, sessions
/docs/guides/…        eight task-shaped guides
/docs/console/                              public-site/docs/console/index.md
/docs/api-reference/  generated from openapi/openapi.yaml
```

### Navigation

Navbar: **Docs**, **Changelog**, **About**, plus a primary call to action. Footer in three
columns — documentation (Quickstart, Concepts, API reference), project (About, Changelog,
GitHub), and contact, which includes the **responsible-disclosure path** that
`docs/PLAN/20` § What Never Gets Published requires to exist and be documented.

### Concepts before guides, guides before reference

- **Concepts** (`model`, `authorization`, `sessions`) explain the mental model: what an
  organization, project, application, role and grant are, and how a session relates to a
  token. An integrator who skips these writes code that works and models the domain wrongly.
- **Guides** are task-shaped: *define roles*, *require MFA*, *validate role claims*,
  *authorization checks*, *refresh token rotation*, *step up with AMR*, *two-step
  verification*.
- **The API reference** is generated and answers "what exactly do I send".

### The changelog is per phase, not per commit

`public-site/changelog/` has one entry per completed roadmap phase, with an author and
tags. A per-commit feed would be noise; a per-phase entry is the unit a reader can act on,
and it matches how the roadmap actually ships.

### Versioned docs exist and are used sparingly

`public-site/versioned_docs/version-1.0` and `public-site/versioned_sidebars`. Versioning
the integrator documentation is cheap now and impossible to retrofit once an integrator
has bookmarked a page that changed meaning.

### `PhaseNotice` is the one custom component

`public-site/src/components/PhaseNotice.tsx`. It renders the phase label a capability
carries. Having exactly one component for it is what makes the label checkable by a script
— a hand-written sentence in each card would not be.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| One project for marketing and docs | ADR-014 | `public-site/docusaurus.config.ts` |
| Shared header and footer on every page | required | Docusaurus theme |
| Responsible-disclosure path in the footer | required | `docs/PLAN/20` |
| Capability labels | one component | `public-site/src/components/PhaseNotice.tsx` |
| API reference | generated | `public-site/docs/api-reference/` |

## Verification

- `npm run build` in `public-site/` — every link resolves; Docusaurus fails on a broken
  internal link.
- `public-site/scripts/check-claims.mjs` — runs against the **built** site, because that is
  what a visitor sees and a check over the source would miss anything a component
  interpolates.

## Not Yet Built / Open Questions

- **No search beyond Docusaurus's local plugin.**
- **`/docs/console/` is a single page.** The console has twenty-one screens and one page of
  end-user documentation.
- **No pricing, no sign-up, no hosted offering** — deliberately; see
  [`00-SITE-PURPOSE-AND-AUDIENCE.md`](./00-SITE-PURPOSE-AND-AUDIENCE.md).

## Related Documents

- [`04-DOCS-CONTENT-PLAN.md`](./04-DOCS-CONTENT-PLAN.md)
- [`../UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md`](../UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md)
