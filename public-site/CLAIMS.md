# Capability Audit

Every claim on every published page, and what it maps to.

`P0-19`'s Definition of Done asks that this "confirms every claim on every published page maps to something either shipped or explicitly labelled as planned". `docs/UI-UX/21` § Content Governance and `CLAUDE.md` make it a standing rule rather than a launch task: **copy never describes a capability beyond the shipped phase.**

**Audited**: 2026-09-11, at `P1-25`, against Phase 1. Previously 2026-09-08 at `P0-18`/`P0-19`, against Phase 0.

Two of the mechanical parts are enforced on every build — `scripts/check-claims.mjs` (every capability carries a phase label, and no label contradicts the roadmap board) and `scripts/check-no-internal-leak.mjs` (nothing verbatim from the documents `docs/PLAN/20` forbids publishing). Neither can read prose. **This document is the part a person has to keep true.**

---

## The rule, and the failure it prevents

`docs/UI-UX/21`'s landing-page blueprint writes four capabilities in the present tense — "Log in once, access every registered application", "Everything the console can do, your scripts and CI/CD can do too". Through Phase 0, copying it as written would have claimed four things that did not exist, so all four sat on the page as *design*, each labelled with the roadmap phase that delivers it.

**Two of them are now true.** Phase 1 built single sign-on and the management API; `P1-26` demonstrated SSO across two separate applications on staging, and `P1-25` executed the quickstart end to end against the running service. Those two cards now read "Shipped — Phase 1". The other two are unchanged.

The rule has a second direction, and this is the first audit to exercise it: **a card still labelled with a phase after that phase shipped is as inaccurate as one claiming something that does not exist.** It just fails in the direction nobody complains about. `check-claims.mjs` now fails the build for either.

What is *not* claimed, and what the status sentence exists to say: there is no hosted offering. "Shipped" here means built, deployed and demonstrable — not that a visitor can sign up. Standing up a new deployment still needs database access for the first organization and administrator (`PG-26`).

---

## Landing (`/`)

| Claim | Status | Basis |
|---|---|---|
| "One login. Every app. Full control over who can do what." | **Positioning** | The hero headline from `docs/UI-UX/21`, verbatim. Describes what the product is *for*, not what a visitor can do today. |
| "A centralized identity and access service with single sign-on, a complete REST API, and role-based access control…" | **Positioning** | Same reading. Immediately followed by the status statement below. |
| "**Status: Phase 1 is built and running.** Single sign-on and the management API work end to end — the quickstart below is executed against a live deployment rather than written from the specification. Roles, delegation and policies are specified and not started. There is no hosted signup: you run it yourself, from source." | **Accurate** | Above the fold, deliberately. Every clause is checkable: the quickstart was executed on 2026-09-11 (`P1-25`), Phases 2 and 4 are not started on the board, and no signup flow exists. `check-claims.mjs` pins both this sentence and the "no hosted signup" clause, so losing either fails the build. |
| "Stop rebuilding login for every service." + problem framing | **Problem statement** | Describes the reader's situation, claims nothing about the product. |
| Single sign-on | **Shipped** | The card reads "Shipped — Phase 1". `P1-06` issues codes, `P1-07` exchanges them, `P1-12` is the page a person types a password into, `P1-18` registers applications, `P1-19` creates users — and `P1-26` is on the task list because it is the proof rather than another endpoint: two applications, two client IDs, one login, verified against staging. Twice before, this row said the capability was unavailable while its protocol was complete. The distinction the list encodes is *usable*, not *implemented*, and that is what made this the revision that could finally be marked shipped. |
| A complete REST API | **Shipped** | The card reads "Shipped — Phase 1". `P1-15` is the envelope and the bearer middleware; `P1-16` through `P1-20` are organizations, projects, applications, users and the audit log. The generated reference covers every one of them. |
| Roles that scale to delegation | **Planned — Phase 4** | Labelled. `P4-01`, not started. |
| Policies when roles are not enough | **Planned — Phase 4b** | Labelled, and conditional — Phase 4b happens only if `P4B-00`'s justification gate is satisfied. |
| "Two of these are built and running; two are specified and not started. The label on each card says which, and it is checked against the roadmap board on every build rather than kept true by hand." | **Accurate** | Replaces "Each of these is specified and none is finished", which stopped being true the day Phase 1 landed. The second clause is itself a claim about the build, and `check-claims.mjs` is what makes it good. |
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
| `/docs` home | **Accurate** | Carries a "Where the project is" admonition: Phase 1 built, Phases 2 and 4 named as not built. |
| `/docs/quickstart` | **Executed, not written** | Every command was run against staging, in order, on 2026-09-11 (`P1-25`). The run corrected three things the page had got wrong from reading the specification: a `refresh_token` that is only issued when `offline_access` is requested, an invented `error_description`, and a `request_id` in the error envelope that is really the `X-Request-Id` header. The page's opening claim — "if one of them does not work for you, that is a bug in this page" — is only safe to print because of that run. |
| `/docs/concepts/*` | **Model, with per-section phase labels** | Four of the six objects on the model page are now real, and the page says so. The Role and Grant sections gained "*Arrives in Phase 2*" and "*Direct grants arrive in Phase 2, project grants in Phase 4*" — they had no labels at all, which was defensible while nothing on the page was shipped and is not defensible now that most of it is. `authorization.md` already labelled each of its three layers. |
| `/docs/guides` | **One available, the rest labelled** | "Set up SSO for your application" points at the quickstart. Everything else stays on the planned list with the phase that delivers it, and two rows name the specific blocker rather than only a phase: roles do not exist yet, and provisioning from CI needs `PG-26`. |
| `/docs/console` | **Accurate** | Lists the five screens that exist and names what is not there — roles, grants, per-user sessions, instance administration. The old text ("the console shell exists; its screens do not") was true at `P0-19` and became false at `P1-22`; it is the same stale-page failure as the archived quickstart, in a page nobody had reason to reopen. |
| `/docs/api-reference` | **Generated** | From `openapi/openapi.yaml`. It cannot describe an endpoint the service does not serve, because `scripts/openapi-shipped-paths.py` fails CI if the spec documents one. |

---

## Changelog (`/changelog`)

Two entries.

"Phase 0 — foundation" opens by saying "Nothing user-facing has shipped", which was the accurate framing for a release note about foundations.

"Phase 1 — single sign-on and the management API" carries a **Known limits** section, and that section is the part of it this audit cares about: no hosted signup, roles not in tokens, refresh tokens that do not rotate, and one virtual machine against a plan that requires more. A release note listing only additions is a sales page, and `docs/UI-UX/21`'s governance rule is about the impression a page leaves as a whole, not only its individual sentences.

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

**What the Phase 1 audit added to that list**: re-audit when a capability *ships*, not only when copy changes. Nobody edits a page to introduce the "still says planned" failure. It appears on its own, on the day a task is marked done, in a file nobody opened.
