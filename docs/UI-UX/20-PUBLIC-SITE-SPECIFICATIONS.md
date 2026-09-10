# 20 — Public Site Specifications (Landing, Docs, About, and More)

Covers the design specification for the **public-facing website** — landing page, documentation, about page, changelog, security/trust page, and contact — as distinct from the authenticated management console specified in `08-PAGE-SPECIFICATIONS.md`/`18-DETAILED-PAGE-SPECIFICATIONS.md`. Technical architecture is in `PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`; actual copy content is planned in `21-CONTENT-AND-COPY-STRATEGY.md`.

## Design Direction Differences from the Console

Per `00-DESIGN-DIRECTION.md`, the console optimizes for "density over whitespace" and "predictable before novel," since it's a repeat-use operational tool. The public site inverts several of these priorities, because its job is entirely different — persuading and informing a first-time, often impatient, visitor (Nadia, `01-USER-PERSONAS.md`):

| Dimension | Console | Public Site |
|---|---|---|
| Density | High (tables, compact rows) | Low-to-medium — generous whitespace, one idea per screen section |
| Novelty | Avoided — conventional patterns | A distinctive hero/visual moment on the homepage is appropriate and expected |
| Tone | Calm, factual, no exclamation points (`00-DESIGN-DIRECTION.md`) | Confident and direct, but still factual — never hype-driven or vague (`21-CONTENT-AND-COPY-STRATEGY.md`'s voice principles) |
| Primary interaction | Repeated task completion | One-time reading + a small number of clear conversion points (quickstart, contact, sign up) |

Shared with the console: the same visual language tokens (`06-VISUAL-LANGUAGE.md`), accessibility standard (`13-ACCESSIBILITY.md`), and the underlying brand identity — a visitor moving from the public site into the console (e.g. after signing up) should feel visual continuity, not a jarring switch.

## Page Inventory

| Page | Primary objective | Key sections | Primary conversion point |
|---|---|---|---|
| Landing (`/`) | Answer "what is this and is it for me" within the first screen | Hero, problem/solution framing, key capabilities (SSO, RBAC, multi-tenant, REST API), social proof (if available), final CTA | "Get Started" / "Read the Docs" |
| About (`/about`) | Establish credibility and context — who built this and why | Mission/context, team or org background (if applicable), link to source/community if open | Link into Docs or Contact |
| Docs Home (`/docs`) | Get a developer to the right doc page fast | Quickstart callout, concept links, guides, API reference link, search | Quickstart CTA |
| Docs: Quickstart | Get a working integration in the shortest realistic path | Step-by-step, copy-pasteable code, a "what you just built" recap at the end | Link to relevant Concepts/Guides for deeper understanding |
| Docs: Concepts | Explain the mental model (Instance/Org/Project, RBAC+ABAC, SSO) without requiring code | Diagrams (reused/adapted from `PLAN/03-ARCHITECTURE.md`, `PLAN/08-AUTHORIZATION.md`), plain-language explanation | Link to relevant Guide |
| Docs: Guides | Task-based how-tos ("Set up SSO for your app," "Create a Project Grant") | Step-by-step, tied to real API calls (`PLAN/05-API-CONTRACT.md`) | Link to API Reference for the exact endpoint |
| Docs: API Reference | Canonical, always-accurate endpoint documentation | Generated from OpenAPI spec (`PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`) — request/response schemas, auth requirements, error codes | "Try it" (if the docs framework supports interactive requests) |
| Changelog (`/changelog`) | Let existing and prospective users see what's new/fixed | Reverse-chronological entries tied to releases | Link back to relevant docs for a changed feature |
| Security (`/security`) | Build trust without exposing internal detail (`PLAN/20-PUBLIC-SITE-ARCHITECTURE.md` "What Never Gets Published") | High-level control summary, compliance status, responsible-disclosure contact | Responsible-disclosure contact form/email |
| Contact (`/contact`) | Give a clear path for sales/support/security inquiries | Simple form or direct contact channels, routed appropriately | Form submission |
| Pricing (`/pricing`, only if applicable) | Answer cost questions without a sales call for straightforward cases | Tier comparison (`comparison_card`-style layout), enterprise/contact-us path for custom needs | "Start Free" / "Contact Sales" |

## Detailed Spec: Landing Page

**Primary objective**: per Journey 6 (`02-USER-JOURNEYS.md`), Nadia must understand what the product is and whether it's relevant within the first screen, without scrolling.

**Visual hierarchy**:
1. Hero: a specific, concrete headline (not generic "Secure your business") + one-line elaboration + primary CTA
2. Problem/solution framing: the specific pain (fragmented auth across services, no SSO, hard to build RBAC well) and how this product addresses it
3. Key capabilities section: SSO, multi-tenant RBAC + delegation, ABAC, REST API — each with a one-sentence explanation, not marketing adjectives alone
4. Social proof (logos, quote, or usage stat) — **only included once genuinely available**; an empty or fabricated social-proof section is worse than omitting it entirely
5. Final CTA section, restating the primary action

**Above-the-fold** (≥1440px, per `12-RESPONSIVE-BEHAVIOR.md`'s grid):
- Navigation (logo, Docs link, About link, primary CTA button)
- Hero headline + subheadline + primary CTA button, occupying roughly the first viewport height

**Interaction**:
- Exactly **one** primary CTA style used consistently across the whole page (e.g. "Get Started") — secondary actions ("Read the Docs") are visually subordinate, avoiding the common failure mode of a homepage with five equally-weighted competing CTAs.
- No autoplay video/audio, no forced modal/newsletter popup on load — respects `00-DESIGN-DIRECTION.md`'s "clarity over decoration" even though this is the more expressive public site.

**Responsive**: single-column stacking below the tablet breakpoint (`12-RESPONSIVE-BEHAVIOR.md`'s grid), full mobile optimization required here (unlike most console screens) since public-site traffic is meaningfully mobile.

**Accessibility**: same WCAG 2.1 AA bar as the console (`13-ACCESSIBILITY.md`) — a public marketing site failing basic accessibility is both an exclusion problem and, in many jurisdictions, a compliance risk.

## Detailed Spec: Docs Home

**Primary objective**: route a developer to the exact doc page they need in as few clicks as possible — this page's success metric is effectively "time to correct page," not time spent on the page itself.

**Visual hierarchy**:
1. Search (prominent, not buried)
2. Quickstart callout (the single most important link for a brand-new evaluator, per Journey 6)
3. Concept/Guide/API Reference sections as parallel entry points, not a forced linear order

**Interaction**: search must support fuzzy matching and surface both guide titles and concept names — a developer often doesn't know the exact terminology this project uses yet (`PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`'s search tooling choice).

## Detailed Spec: API Reference

**Primary objective**: be the single, trustworthy source of truth for every endpoint — this page must never contradict the actual running API.

**Content rule**: every endpoint's documentation is generated from the OpenAPI spec (`PLAN/05-API-CONTRACT.md`), including request/response schemas and error codes — no hand-written duplicate descriptions that can drift out of sync, per `PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`.

**Interaction**: if the docs framework supports it, an interactive "try it" panel (sends a real request against a sandbox/staging environment) significantly shortens Nadia's evaluation loop in Journey 6 — recommended as a Phase 1-adjacent enhancement once the API is stable enough to expose publicly.

## Cross-Page Requirements

- Every page shares the same header/footer navigation and the same visual tokens (`06-VISUAL-LANGUAGE.md`), so moving between Landing → Docs → About never feels like a different product.
- Every page meets the same accessibility bar as the console (`13-ACCESSIBILITY.md`).
- Every page is fully responsive down to mobile width (unlike most console screens) — see `PLAN/20-PUBLIC-SITE-ARCHITECTURE.md` for why the public site's mobile requirements differ from the console's (`16-MOBILE-UX.md`).

Continue to [21 — Content and Copy Strategy](./21-CONTENT-AND-COPY-STRATEGY.md).
