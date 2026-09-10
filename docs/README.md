# Product Documentation — Centralized Auth Service

Reference documentation for an identity and access platform: centralized authentication (OIDC / OAuth 2.1), a complete REST Management API, multi-tenant RBAC with cross-organization delegation, a management console, and a public site. 49 documents across three categories.

The **execution plan** built from these documents lives in [`../TASKS/`](../TASKS/), and the record of what was actually built lives in [`../MEMORY/`](../MEMORY/).

`docs/` is reference material. It is amended **deliberately**, through the plan-change process in `AGENTS.md` rule 9 and the deviation protocol in [`../TASKS/00-TASK-CONVENTIONS.md`](../TASKS/00-TASK-CONVENTIONS.md) — never edited as a side effect of implementation. `.github/CODEOWNERS` enforces that: a change under `docs/` requires a review that a change under `backend/` does not.

## Recommended Reading Order

```
1.  PLAN/00-PROJECT-CONTEXT.md        → start here for the big-picture context
2.  PLAN/01-PRODUCT-SCOPE.md          → and, as importantly, what is out of scope
3.  PLAN/03-ARCHITECTURE.md
4.  PLAN/04-DATA-MODEL.md             → the core domain model, referenced by nearly everything else
5.  PLAN/08-AUTHORIZATION.md          → RBAC, Project Grants and ABAC together, because they are layered rather than separate
6.  PLAN/05-API-CONTRACT.md
7.  SECURITY/00-ASSET-AND-TRUST-BOUNDARY-INVENTORY.md → SECURITY/05
8.  UI-UX/00-DESIGN-DIRECTION.md      → before any console work
9.  PLAN/16-IMPLEMENTATION-ROADMAP.md → what is in scope for the current phase
```

`SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` is mandatory reading for anything touching authentication, authorization or sessions. Every task card in `../TASKS/` names the abuse cases it must be tested against, and they come from there.

## Folder Structure

```
PLAN/        21 files — product scope, requirements, architecture, data model,
                        API contract, authorization, security baseline, testing,
                        performance, observability, deployment, roadmap
UI-UX/       22 files — design direction, design system, page and component
                        specifications, accessibility, the public site
SECURITY/     6 files — asset and trust-boundary inventory, threat actors,
                        attack surface and scenarios, detection, incident
                        response, red-team verification
```

Three categories rather than the ten a larger project might use, because this plan keeps one document per topic instead of a folder per topic. `PLAN/07-BACKEND-ARCHITECTURE.md` is what a `BACKEND/` folder would hold; `PLAN/04-DATA-MODEL.md` is what a `DATABASE/` folder would hold. If a topic ever outgrows its file, splitting it into a folder is the natural move — and it is a documentation change, made deliberately, like any other.

## Core Principles Binding All Documents

1. **Every capability in the console is also in the REST API** (`PLAN/02` FR-14). A console-only shortcut that bypasses the documented contract is never acceptable.
2. **Authorization is enforced server-side, on every request.** The console may hide a button for UX; a hidden button is not a security control (`PLAN/08`, `SECURITY/02` §2/§3).
3. **Project Grant role assignment is validated as a subset of `granted_role_keys` on every request**, not only at grant creation (`PLAN/08` Part C).
4. **Tenant isolation is security priority #1.** Row-level security is the mechanism, not a predicate somebody remembers to write (`PLAN/04`, `PLAN/09`).
5. **Nothing published describes a capability that has not shipped** (`UI-UX/21` § Content Governance). The public site's capability audit enforces it in CI.
6. **The documents cross-reference each other explicitly.** Follow the references before implementing — the answer to most design questions is already written down somewhere in here.

## How to Use This with an AI Agent

`../CLAUDE.md` carries the documentation map: which document to open for any given task. Read that first — it is shorter than the plan and it will route you correctly.

Then work through `../TASKS/`: one branch per task card, a feature specification in `../MEMORY/specs/` for anything touching authentication, authorization or the data model, and a record in `../MEMORY/records/` before the card is marked done.

**If a question is not answered in these documents, that is a real gap.** Raise it in `../TASKS/BACKLOG.md` rather than deciding it silently — the plan is this detailed specifically so that implementation does not require guessing.
