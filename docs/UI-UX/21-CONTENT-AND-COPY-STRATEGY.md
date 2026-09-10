# 21 — Content & Copy Strategy

This is the copywriting plan for the public site (`20-PUBLIC-SITE-SPECIFICATIONS.md`) — it defines voice, messaging framework, and page-by-page copy blueprints with actual example copy, not just "write something professional and clean." A copywriter or the team member drafting real copy should be able to work directly from this document.

## Voice & Tone (Distinct from the Console's Voice)

Per `20-PUBLIC-SITE-SPECIFICATIONS.md`'s design-direction comparison, public-site copy has a different job than console copy (`00-DESIGN-DIRECTION.md`'s "direct, factual, calm"):

| Principle | What it means here |
|---|---|
| **Confident, not hyped** | State what the product does plainly. Avoid superlatives that aren't backed by something concrete ("blazing fast," "revolutionary") — prefer specific, checkable claims ("issues tokens in under 200ms at p95," referencing real numbers from `PLAN/12-PERFORMANCE.md` once measured). |
| **Concrete over abstract** | "Delegate a project to a partner organization with exactly the roles you choose" beats "Powerful, flexible access control." Every abstract benefit claim should be immediately followed by the concrete mechanism that makes it true. |
| **Developer-respecting** | The primary audience (Nadia, `01-USER-PERSONAS.md`) is technical. Don't over-explain basic concepts (OAuth, SSO) in marketing copy — assume baseline familiarity and let `Docs: Concepts` carry the deeper explanation. |
| **Never mislead about scope** | If a feature is planned but not shipped (see `PLAN/16-IMPLEMENTATION-ROADMAP.md`'s phases), it does not appear in present-tense marketing copy. A roadmap/changelog page can mention what's coming; the homepage describes what exists today. |

## Messaging Framework

A single consistent framework every page's copy should trace back to, so the site doesn't read as disconnected pages written independently:

1. **The problem**: teams building multiple services end up with fragmented, duplicated auth logic, no real SSO, and ad hoc access control that's hard to audit.
2. **The product**: a centralized Auth Service providing SSO (OIDC/SAML), a full REST management API, and RBAC that scales from simple role assignment to cross-organization delegation and attribute-based policies when needed.
3. **The proof**: standards-based (not proprietary), API-first (nothing hidden behind the UI), and built with the layered authorization model (`PLAN/08-AUTHORIZATION.md`) that avoids both under-powered RBAC and over-complicated ABAC-everywhere.
4. **The action**: try it via the Quickstart, or read the Concepts docs first if you want to understand the model before writing code.

## Page-by-Page Copy Blueprint

### Landing Page

**Hero headline** (concrete, not generic):
> "One login. Every app. Full control over who can do what."

**Hero subheadline**:
> "A centralized identity and access service with SSO, a complete REST API, and role-based access control that scales from a single team to multi-organization delegation."

**Primary CTA**: "Get Started" → Quickstart
**Secondary CTA**: "Read the Docs" → Docs Home

**Problem/solution section**:
> Headline: "Stop rebuilding login for every service."
> Body: "Every new app means another login form, another user table, another place permissions can drift out of sync. [Product name] centralizes authentication and authorization once, so every app you build or buy plugs into the same identity layer."

**Capabilities section** (one card per capability, each with a specific claim, not an adjective):
- **SSO**: "Log in once, access every registered application — built on standard OIDC and OAuth 2.1, not a proprietary protocol."
- **Full REST API**: "Everything the console can do, your scripts and CI/CD can do too. Provision organizations, projects, and users programmatically from day one."
- **RBAC that scales**: "Start with simple per-project roles. Add cross-organization delegation when you need to let partners self-manage their own team's access — without giving up control over which roles they can grant."
- **Policy-based access when you need it**: "For access rules that depend on context — department, amount limits, time of day — layer in attribute-based policies without replacing your existing roles."

**Final CTA section**:
> Headline: "See it work in five minutes."
> Body: "Follow the quickstart and connect your first application."
> Button: "Get Started"

### About Page

**Opening framing** (avoid a vague "our mission is..." opener; lead with the concrete reason this exists):
> "[Product name] exists because identity infrastructure shouldn't be the hardest part of building a new service. We built it API-first, standards-based, and designed to grow from a single internal tool into a full multi-tenant platform without a rewrite."

Sections: context/origin (why this was built), design principles (can directly reference `PLAN/00-PROJECT-CONTEXT.md`'s design principles, translated to plain language for an external audience), and a link into Docs/Contact rather than a long team bio section (keep this page short — it's not the conversion driver Nadia's journey depends on).

### Docs Home Intro Copy

> "Everything you need to integrate, from your first login flow to delegating access across organizations."

Followed immediately by the Quickstart callout — per `20-PUBLIC-SITE-SPECIFICATIONS.md`, this page's job is routing, not reading, so intro copy stays to one sentence.

### Quickstart Opening

> "This guide gets a working login flow running against [Product name] in about five minutes. You'll register an application, redirect a user through login, and receive a verified identity token."

Sets an explicit expectation (time, outcome) before the steps begin — critical for Nadia's evaluation journey (`02-USER-JOURNEYS.md` Journey 6), since an unbounded "let's get started" invites abandonment if the real time cost turns out longer.

### Security Page

> Headline: "Security is the reason this product exists."
> Body: "[Product name] is built around short-lived tokens, asymmetric signing, and mandatory PKCE for every client. Every administrative action is logged. Found a security issue? We want to hear about it — see our responsible disclosure process below."

This copy stays at the trust/summary level per `PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`'s "What Never Gets Published" — no mechanism-level detail, just credible, checkable-in-spirit statements plus the disclosure contact path.

### Changelog Entry Format (Template for Every Release)

```
## [Version] — [Date]

### Added
- [Feature], with a one-line link to the relevant doc

### Changed
- [Behavior change], noting any migration/action required

### Fixed
- [Bug], described from the user's perspective, not the internal root cause
```

Consistent formatting matters more here than clever copy — developers scan changelogs, they don't read them as prose.

### Contact Page

Keep the form minimal (name, email, message, optional "reason for contact" dropdown routing to sales/support/security) — a long qualification form here is friction against Nadia's evaluation momentum (`02-USER-JOURNEYS.md` Journey 6).

## Content Governance

- Every page's copy is reviewed against **the messaging framework** above before publishing — if a sentence doesn't trace back to problem/product/proof/action, question whether it belongs.
- Copy referencing product capabilities must be checked against `PLAN/16-IMPLEMENTATION-ROADMAP.md`'s actual shipped phase — marketing copy is never allowed to describe a Phase 4/4b feature as available while the project is still in Phase 1.
- Revisit this document's example copy once real product naming, actual measured performance numbers (`PLAN/12-PERFORMANCE.md`), and any real social proof are available — the copy above is a structural template with realistic placeholder content, not final, ready-to-ship text.

---

This concludes the public-site-related additions to `UI-UX/`. For the technical architecture behind these pages, see `PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`.
