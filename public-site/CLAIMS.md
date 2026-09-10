# Capability Audit

Every claim on every published page, and what it maps to.

`P0-19`'s Definition of Done asks that this "confirms every claim on every published page maps to something either shipped or explicitly labelled as planned". `docs/UI-UX/21` § Content Governance and `CLAUDE.md` make it a standing rule rather than a launch task: **copy never describes a capability beyond the shipped phase.**

**Audited**: 2026-09-08, at `P0-18`/`P0-19`, against Phase 0.

Two of the mechanical parts are enforced on every build — `scripts/check-claims.mjs` (every capability carries a phase label, and no label contradicts the roadmap board) and `scripts/check-no-internal-leak.mjs` (nothing verbatim from the documents `docs/PLAN/20` forbids publishing). Neither can read prose. **This document is the part a person has to keep true.**

---

## The rule, and the failure it prevents

The project is in Phase 0. What exists: the service builds, deploys, runs against real PostgreSQL and Redis, enforces tenant isolation in the database, writes an append-only audit log, and serves two operational probes. That is all.

`docs/UI-UX/21`'s own landing-page blueprint writes four capabilities in the present tense — "Log in once, access every registered application", "Everything the console can do, your scripts and CI/CD can do too". Copied as written, the site would claim four things that do not exist.

They are on the page as *design*, each labelled with the roadmap phase that delivers it, under a heading that says none is finished.

---

## Landing (`/`)

| Claim | Status | Basis |
|---|---|---|
| "One login. Every app. Full control over who can do what." | **Positioning** | The hero headline from `docs/UI-UX/21`, verbatim. Describes what the product is *for*, not what a visitor can do today. |
| "A centralized identity and access service with single sign-on, a complete REST API, and role-based access control…" | **Positioning** | Same reading. Immediately followed by the status statement below. |
| "**Status: in development.** The service runs and is deployed; the authentication and authorization endpoints described below are being built." | **Shipped, accurate** | Above the fold, deliberately. The service is deployed and healthy at `auth.zedth.my.id`. |
| "Stop rebuilding login for every service." + problem framing | **Problem statement** | Describes the reader's situation, claims nothing about the product. |
| Single sign-on | **Planned — Phase 1** | Labelled on the card. The protocol is now complete — `P1-06` issues codes, `P1-07` exchanges them, `P1-12` is the page a person types a password into, all three done — and the capability is still not available. A visitor cannot register an application (`P1-18`) or create a user with a password (`P1-19`), so there is nothing to sign in to and nobody to sign in as. The audit fired on the day `P1-12` landed and said the card should stop saying "Phase 1"; the fix was to extend the task list rather than to relabel the card, because the thing a capability card promises is *usable by a visitor*, not *implemented*. Second time the same lesson, found by the check the first one produced. |
| A complete REST API | **Planned — Phase 1** | Labelled. `P1-15`, not started. |
| Roles that scale to delegation | **Planned — Phase 4** | Labelled. `P4-01`, not started. |
| Policies when roles are not enough | **Planned — Phase 4b** | Labelled, and conditional — Phase 4b happens only if `P4B-00`'s justification gate is satisfied. |
| "Each of these is specified and none is finished." | **Shipped, accurate** | The section heading that makes the four cards unambiguous. Still accurate after `P1-12`: the OIDC endpoints behind single sign-on are finished, and the capability a visitor could use is not. If that sentence ever has to be qualified, it is the card that should change, not the sentence. |
| "The engineering plan, the threat model, and every architectural decision are in the repository — including the ones that turned out to be wrong." | **Shipped, accurate** | `docs/PLAN/`, `docs/SECURITY/`, `MEMORY/DECISIONS.md` are all in the public repository. Several ADRs record something built, found wrong, and changed. |

**No social-proof section.** `docs/UI-UX/20`: include it "only once genuinely available", because "an empty or fabricated social-proof section is worse than omitting it entirely". There is nobody to quote.

---

## About (`/about`)

| Claim | Status | Basis |
|---|---|---|
| "built API-first and standards-based, designed to grow… without a rewrite" | **Design intent** | Stated as intent. `docs/PLAN/02` FR-14 and `docs/PLAN/00`. |
| "Standards over invention. OIDC and OAuth 2.1, not a proprietary protocol." | **Design principle** | A principle the implementation is held to, not a shipped feature. The endpoints arrive in Phase 1. |
| "Every capability in the management console is available through the same public REST API." | **Design principle** | `docs/PLAN/02` FR-14, and enforced by `CLAUDE.md`. Neither surface exists yet, so nothing contradicts it. |
| "cross-tenant isolation is a property of the database rather than of the code that queries it" | **Shipped** | `P0-08`: row-level security on every tenant-scoped table; the service refuses to start as a role that can bypass it. |
| "an architecture decision record for every significant choice live in the repository" | **Shipped** | Fourteen ADRs in `MEMORY/DECISIONS.md`. |
| "**Status.** In development. The service runs and is deployed; the authentication and authorization endpoints are being built." | **Shipped, accurate** | |

---

## Contact (`/contact`)

| Claim | Status | Basis |
|---|---|---|
| `security@zedth.my.id` is the responsible-disclosure channel | **Live** | `docs/PLAN/20` requires a documented path to exist — a researcher with nowhere to report goes public instead. |
| "We will confirm receipt, tell you what we found, and let you know when it is fixed." | **Commitment** | A promise about behaviour, not a product claim. It has to be kept. |
| GitHub issues for everything else | **Live** | |

---

## Docs

| Page | Status | Basis |
|---|---|---|
| `/docs` home | **Accurate** | Carries an "In development" admonition naming the current phase. |
| `/docs/quickstart` | **Explicit placeholder** | Opens with a `danger` admonition: "This guide does not exist yet." Lists what it will cover and the four tasks that must ship first. `P1-24` replaces it. |
| `/docs/concepts/*` | **Model, not endpoints** | `P0-19` step 3 permits this: concepts describe the design rather than shipped endpoints. Each page states it. Derived from `docs/PLAN/03`, `docs/PLAN/04` and `docs/PLAN/08` at a product-explainer level. |
| `/docs/guides` | **Explicit placeholder** | "Nothing here yet", with the planned list and the phase each arrives in. |
| `/docs/console` | **Explicit placeholder** | "The console shell exists; its screens do not." |
| `/docs/api-reference` | **Generated** | From `openapi/openapi.yaml`. It cannot describe an endpoint the service does not serve, because `scripts/openapi-shipped-paths.py` fails CI if the spec documents one. |

---

## Changelog (`/changelog`)

One entry, "Phase 0 — foundation". Every item in it maps to a completed task with a MEMORY record. It opens by saying "Nothing user-facing has shipped", which is the accurate framing for a release note about foundations.

---

## Pages that deliberately do not exist

| Page | Why |
|---|---|
| `/security` | `docs/PLAN/20` § Deployment & Roadmap Placement puts the public trust page alongside Phase 5. Publishing one now would mean describing controls at a maturity the project has not reached. `/contact` carries the disclosure path in the meantime, which is the part `docs/PLAN/20` requires immediately. |
| `/pricing` | `docs/PLAN/20`: "Only if a commercial/paid tier exists; omit entirely otherwise." |

---

## What this audit cannot do

It confirms the pages as they stand today. It does not prevent a future edit from introducing a present-tense claim in prose, and neither does either script — `check-claims.mjs` checks structure and labels, not sentences.

**Re-audit when**: a phase completes, a capability card is added or reworded, or `/security` is published. Update this file in the same commit as the copy change. A capability audit that lags the site by one release is a document describing a site that no longer exists.
