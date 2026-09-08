# 20 — Public Site Architecture (Landing, Docs, About)

Covers the technical architecture of the **public-facing website**: landing page, documentation, about/company page, changelog, and related pages — following the pattern of a project like Zitadel's public site (`zitadel.com` + `zitadel.com/docs`), which is intentionally separate infrastructure from its authenticated console.

## Why a Separate Surface from the Console

| Concern | Console (`06-FRONTEND-ARCHITECTURE.md`) | Public Site |
|---|---|---|
| Audience | Authenticated admins/users, repeat daily use | Anonymous visitors, developers evaluating the product, one-off reads |
| Auth | Behind OIDC login | Fully public, no login required (except optional newsletter/contact forms) |
| Content nature | Live application data (users, roles, grants) | Mostly static/versioned content (docs, marketing copy) |
| SEO requirements | None — never indexed, behind auth | Critical — this is how the product gets discovered |
| Update cadence | Tied to feature releases | Docs update with releases; marketing/blog update independently, more frequently |
| Performance profile | Interactive app, acceptable to load a JS bundle | Must be fast on first load (SEO + bounce rate), favors static generation |

Sharing a codebase between the two would force compromises in both directions — this is why they're deliberately built and deployed as separate projects, matching how Zitadel itself separates its marketing/docs site from its console.

## Recommended Stack

| Concern | Choice | Reason |
|---|---|---|
| Landing/marketing pages | **Static site generator** (e.g. Next.js in static export mode, or Astro) | Fast first paint, strong SEO, no need for the client-state complexity the console needs (`06-FRONTEND-ARCHITECTURE.md`) |
| Documentation site | **Docusaurus** (or similar docs-focused SSG, e.g. Nextra) | Purpose-built for versioned technical docs: built-in search, sidebar navigation generated from folder structure, versioning support for docs that track API/product versions (`PLAN/05-API-CONTRACT.md`) |
| Content source | **Markdown/MDX files in a Git repo**, not a database-backed CMS initially | Docs-as-code: content changes go through the same PR review process as code, keeping docs accurate as the API evolves (`PLAN/17-ACCEPTANCE-CRITERIA.md`'s "General Definition of Done" already requires API changes to update the OpenAPI spec — docs should follow the same discipline) |
| Blog/changelog | MDX files within the same docs-as-code repo, or a lightweight headless CMS if a non-technical team member needs to publish without a PR | Start with Git-based (simplest); revisit only if a real workflow friction appears |
| Search | Docs-site search (e.g. Algolia DocSearch, or the docs framework's built-in local search) | Developers evaluating the product expect fast doc search — this is a make-or-break usability factor for adoption |
| Hosting/deploy | Static hosting/CDN, deployed independently from the console and the backend | Decouples release cadence; a docs typo fix shouldn't require a backend deploy pipeline |
| Analytics | Privacy-respecting page analytics (page views, referrers, popular docs pages) | Informs which content needs improvement — ties into `21-CONTENT-AND-COPY-STRATEGY.md`'s iteration plan |

## Site Structure (Top-Level)

```
/                     → Landing page
/about                → About / company page
/docs                 → Documentation home
  /docs/quickstart     → Getting started
  /docs/concepts       → Core concepts (mirrors PLAN/03-ARCHITECTURE.md, PLAN/08-AUTHORIZATION.md at a product-explainer level)
  /docs/guides         → Task-based guides (e.g. "Set up SSO for your app", "Create a Project Grant")
  /docs/api-reference  → Generated from the OpenAPI spec (PLAN/05-API-CONTRACT.md) — never hand-written and hand-maintained separately
  /docs/console        → How to use the management console (references UI-UX/08-PAGE-SPECIFICATIONS.md's screen inventory at an end-user-facing level)
/changelog            → Release notes
/security             → Public security/trust page (high-level summary derived from SECURITY/ and PLAN/09-SECURITY.md — never exposes internal threat-model detail, see "What Never Gets Published" below)
/pricing              → Only if a commercial/paid tier exists; omit entirely otherwise
/contact              → Contact/support entry point
```

Full page-by-page design specification is in `UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md`; content/copy planning is in `UI-UX/21-CONTENT-AND-COPY-STRATEGY.md`.

## API Reference Generation

The `/docs/api-reference` section must be **generated from the OpenAPI spec** (`PLAN/05-API-CONTRACT.md`), not hand-written, using a standard OpenAPI-to-docs renderer. This guarantees the public API reference can never drift out of sync with the actual API — a hand-maintained duplicate is a recurring source of the exact kind of documentation debt this architecture is meant to avoid.

## What Never Gets Published

The public `/security` page communicates security posture at a **marketing/trust level** (e.g. "TLS everywhere," "SOC 2 in progress," "responsible disclosure program") — it must never expose:
- Specific attack scenarios or mitigation mechanics from `SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` (this is defensive documentation, not public marketing content).
- Infrastructure topology detail from `PLAN/14-DEPLOYMENT.md`.
- Anything from `PLAN/18-RISK-REGISTER.md`.

A public responsible-disclosure/bug-bounty contact path (e.g. `security@` email or a dedicated form) should exist and be the documented channel for external researchers, rather than security detail being published outright.

## Versioning Strategy for Docs

Since the API evolves (`PLAN/05-API-CONTRACT.md`'s `/v1`, `/v2` versioning), docs must support versioned content matching each supported API version — a docs framework like Docusaurus supports this natively. Never leave old-version docs unreachable once a new version ships; consumer apps integrated against an older API version still need accurate docs until that version's deprecation window (`PLAN/05-API-CONTRACT.md`) ends.

## Deployment & Roadmap Placement

The public site is not gated behind the backend's phased roadmap (`PLAN/16-IMPLEMENTATION-ROADMAP.md`) the way console features are — a landing page and basic docs should exist **before** Phase 1 ships, since they're how early adopters and internal stakeholders first evaluate the project. Recommended sequencing:

| Milestone | Public site scope |
|---|---|
| Before Phase 1 | Landing page, About page, basic docs (concepts + quickstart placeholder) |
| Alongside Phase 1 (MVP) | Quickstart guide reflecting the real MVP flow, generated API reference for the endpoints that exist |
| Alongside Phase 2+ | Guides for RBAC/multi-tenancy, Project Grants, ABAC as each ships; changelog entries per release |
| Alongside Phase 5 | Public security/trust page, finalized as the product approaches production hardening |

See `UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md` for the design specification of these pages, and `UI-UX/21-CONTENT-AND-COPY-STRATEGY.md` for the actual copy plan.
