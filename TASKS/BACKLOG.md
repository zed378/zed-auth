# Backlog — Open Questions, Gaps, and Deferrals

Three kinds of entry live here:

- **Open Questions (OQ)** — decisions the plan does not contain. `CLAUDE.md` is explicit: "If a question isn't answered in these documents, that's a real gap — flag it to the user rather than guessing and silently deciding." These are raised, not resolved unilaterally.
- **Plan Gaps (PG)** — things the plan implies but does not specify, usually a missing table or an unstated dependency. Each needs a plan amendment or a documented decision before the task that depends on it can start. **All eleven original gaps were resolved on 2026-09-08** by amending the plan documents; the section below is kept as the record of what changed and why.
- **Deferred (DF)** — explicitly out of scope now, recorded so the decision isn't quietly reversed later.

Every entry names the task it blocks or affects, so nothing here is a note without a consequence.

---

## Open Questions

### OQ-01 — Should `CLAUDE.md` and `AGENTS.md` reference `TASKS/` and `MEMORY/`?

**Blocks**: `P0-21` final item.

Both files carry documentation maps that route an agent to the right document for any task. Neither mentions `TASKS/` or `MEMORY/`, because neither existed when they were written. Adding them would make the execution plan discoverable the same way the design plan is.

These two files govern agent behavior, so editing them is a deliberate act rather than a side effect. **Recommendation**: add a row to each documentation map, plus a short note in `CLAUDE.md`'s Mandatory Workflow section pointing at `TASKS/00-TASK-CONVENTIONS.md`. Awaiting approval.

### OQ-02 — Language of the `TASKS/` and `MEMORY/` documents

**Affects**: all of `TASKS/` and `MEMORY/`.

Written in English to match the existing 49 documents across `PLAN/`, `UI-UX/`, and `SECURITY/`, so the whole corpus reads as one body of work and cross-references stay natural. If Indonesian is preferred for these two folders, say so and they can be translated — the structure is unaffected.

### ~~OQ-03 — Deployment target~~ — ANSWERED 2026-09-08

**Answer**: a **self-managed VM** for now, with **Kubernetes support planned later**.

Recorded as [ADR-011](../MEMORY/DECISIONS.md). Consequences worked through in `P0-14` (secrets) and `P0-20` (staging). The architecture stays deployment-agnostic — configuration by environment variable, no local disk state, stateless service — so the move to Kubernetes is a manifest change rather than a rewrite.

**This answer creates one open deviation**, tracked below as `DV-01`, because a single VM cannot satisfy `PLAN/14`'s and `PLAN/15`'s Multi-AZ requirement for production.

### OQ-10 — Is there a numeric Lighthouse bar for the public site?

**Blocks**: the last item of `P0-18`'s Definition of Done.

That item reads "Lighthouse performance and SEO scores meet the bar set in `UI-UX/20` § Cross-Page Requirements". That section sets an accessibility bar — the same WCAG 2.1 AA as the console — and no numeric performance or SEO target. There is nothing to measure against.

The site is statically generated, ships a sitemap, canonical URLs and per-page metadata, and has no render-blocking third-party script, so it is likely to score well. "Likely to score well" is not a gate.

**Recommendation**: either set a number in `UI-UX/20` (90+ on performance, accessibility, best practices and SEO for the landing page and one docs page is a conventional bar), or delete the item from the DoD and rely on the accessibility checks that do exist. The second is defensible: `PLAN/20` names SEO as critical without quantifying it, and a score threshold nobody chose is a gate that gets waived the first time it fails.

### OQ-04 — Email delivery provider

**Blocks**: `P1-19.1`, `P1-19.4`, `P3-08`.

Invitations, password resets, and anomaly notifications all require outbound email, and none of the plan documents name a provider or approach. This also carries a security dimension: SPF, DKIM, and DMARC configuration matters for an identity provider, since password reset emails are a prime phishing target.

### OQ-05 — Concrete RPO and RTO values

**Blocks**: `P5-07`'s pass/fail judgment.

`PLAN/15` deliberately leaves both as "to be set based on consumer-app SLAs," and warns against leaving them as placeholders in the production runbook. The DR drill can be executed without them, but it cannot be judged successful or unsuccessful without them.

### OQ-06 — Capacity assumptions

**Affects**: `P0-20` sizing, `P5-04` load test design.

`PLAN/12` asks for capacity planning based on expected concurrent users, token issuance rate, and authz check rate — none of which are stated anywhere. Load testing needs target numbers, or it measures an arbitrary shape of traffic.

### OQ-07 — Number of organizations at launch

**Affects**: `P2-08`, `P2-09`.

`PLAN/02` assumes a single default organization initially. If the actual launch is multi-tenant from day one, `P2-09`'s tenant resolution strategy needs deciding earlier and the Phase 1 assumptions shift.

### OQ-11 — Cloudflare Access in front of the staging service?

**Affects**: `P0-20`, and the security posture until Phase 1 ships authentication.

`https://auth.zedth.my.id` is now publicly reachable, and the service currently
exposes only `/healthz` and `/readyz` — nothing sensitive. But Phase 1 adds the
hosted login page and the Management API to that same hostname, and until real
authentication exists, anything published there is open to the internet.

Cloudflare Access in front of the tunnel would gate the whole hostname behind
an identity check with no code change, which is worth considering for staging
specifically. It would need removing before the service is meant to serve real
consumer applications, since an OIDC provider behind a second login is not
usable by the applications that depend on it.

**Recommendation**: enable it for staging now, and remove it as part of `P1-28`
when the two demo consumer applications need real access.

### OQ-10 — Deploy credential for automated staging deployment

**Blocks**: the last unmet item of `P0-20` — "a merge to `main` deploys to staging automatically".

`deploy.sh` runs correctly on the VM, but nothing triggers it from CI. That needs either an SSH deploy key held as a repository secret, or a self-hosted runner on the VM. Both are credential decisions with real security consequences — a deploy key in GitHub Actions is a key that can reach production, and a self-hosted runner executes untrusted PR code on the deployment host unless carefully restricted (`SECURITY/02` §18).

**Recommendation**: a self-hosted runner restricted to `main` only, never to pull requests. Awaiting the owner's decision.

### OQ-08 — Which two applications are the MVP consumers?

**Affects**: `P1-26`, `P1-28`.

`PLAN/01`'s MVP definition of done requires two internal applications authenticating real users. `P1-26` builds two demo applications, which proves the mechanism — but the acceptance criterion says *internal applications*, implying real ones. Are there specific applications lined up, and are their teams ready to integrate during Phase 1?

---

## Open Deviations

A deviation is a place where the built system knowingly differs from what `PLAN/`, `UI-UX/`, or `SECURITY/` specifies. Per the deviation protocol in `00-TASK-CONVENTIONS.md`, each carries an ADR, is visible here rather than only in a commit message, and either closes or is accepted as a risk in `PLAN/18-RISK-REGISTER.md`.

### DV-01 — Single-VM production cannot meet the Multi-AZ requirement

**Affects**: `P0-20`, `P5-06` (horizontal scale-out verification), `P5-07` (DR drill), and `PLAN/17` Phase 5 sign-off.
**ADR**: [ADR-011](../MEMORY/DECISIONS.md).
**Status**: Open — accepted for the interim, must be closed or formally risk-accepted before production sign-off.

`PLAN/14-DEPLOYMENT.md` § Environment Strategy specifies production as "Multi-AZ minimum". `PLAN/15-DISASTER-RECOVERY.md` § High Availability repeats it. `PLAN/02-REQUIREMENTS.md` sets availability to "match or exceed the strictest consumer app's SLA", and `PLAN/09` opens by noting a compromise here compromises every dependent application — the same logic applies to an outage.

A single VM is a single point of failure for every consumer application's login. That is a real gap, not a technicality:

- **No instance redundancy.** A VM reboot, a kernel panic, or a failed deploy takes authentication down platform-wide. Running several containers on one host survives a process crash, not a host failure.
- **No database failover.** `PLAN/14` § Scalability assumes read replicas and a single primary; one VM has neither.
- **`P5-06` cannot pass as written.** `PLAN/12` requires horizontal scaling to be "verified in practice… by actually running a scale-out test", and a single host cannot demonstrate multi-node scaling.
- **`P5-07`'s DR drill is weaker.** Restoring into "an isolated environment" (`PLAN/15`) means a second machine that does not exist yet.

**Why it is nonetheless reasonable now**: this is explicitly interim, the consumer applications that would define an SLA do not exist yet (`OQ-08` is still open), and `PLAN/00`'s incremental principle argues against building multi-region HA before a single tenant is live. `PLAN/15` says the same about multi-region: "don't build it speculatively."

**What closes it**: the planned Kubernetes migration, with at least two nodes and a Postgres primary/replica pair. Until then, `P0-20` implements the strongest single-host posture available — offsite backups in a separate failure domain, a verified restore, and a documented recovery time — so the gap is bounded and measured rather than unknown.

**Do not let this deviation quietly expire.** Before any consumer application depends on this service in production, either the Kubernetes migration lands or `PLAN/18` carries an explicit, owned, dated accepted-risk entry saying that single-VM availability is acceptable for the named consumers.

### DV-02 — `manager_roles` has no tenant row-level security

**Affects**: `P0-08`, closes in `P2-05`.
**ADR**: recorded inline in `backend/migrations/20260908000007_row_level_security.up.sql`.
**Status**: Open — bounded and deliberate, must close when `P2-05` lands.

Every other tenant-scoped table got an org-scoped RLS policy in `P0-08`. `manager_roles` did not, and the reason is structural rather than an oversight.

The table holds `(user_id, role, scope_id)` triples and is read **during permission resolution** — that is, before a tenant context exists, because what the caller may access is precisely what is being determined. An org-scoped policy would make the table unreadable at the only moment it is needed.

The correct policy keys on the current **user**, not the current organization, which requires an `app.current_user_id` session setting that does not exist until bearer authentication lands in `P1-15`/`P2-05`. Adding a half-working policy now would give the appearance of isolation without the substance, which is worse than a documented gap.

**What limits the exposure meanwhile**: the table holds no personal data — only identifiers and role names — and the application scopes its own queries. `manager_roles` is also not reachable through any API surface yet, since none exists.

**What closes it**: `P2-05` introduces the permission-resolution module and with it a user context. The policy to add then is `user_id = current_user_id()`, plus the instance-scoped path for administration.

**Do not let this expire quietly.** `P2-05` cannot be marked done while this is open.

---

## Plan Gaps — ALL RESOLVED 2026-09-08

All eleven gaps below were closed by amending the plan documents directly, at the user's explicit instruction. This was a deliberate plan change under `AGENTS.md` rule 9, not a side effect of feature work. Full reasoning is in [`MEMORY/records/2026-09-08-plan-gap-remediation.md`](../MEMORY/records/2026-09-08-plan-gap-remediation.md), with ADR-002 through ADR-005 in [`MEMORY/DECISIONS.md`](../MEMORY/DECISIONS.md).

| ID | Gap | Resolution | Amended |
|---|---|---|---|
| PG-01 | `roles` had no permission keys column, though `PLAN/08` Part A requires them | Added `permission_keys text[]` and `is_builtin bool`, with a note explaining the array-over-join-table choice | `PLAN/04` § `roles` |
| PG-02 | No storage for signing keys, though rotation with overlap and rollback safety both require DB-held key state | Added `signing_keys` — `kid`, purpose (`oidc`/`saml`), algorithm, public key, a secret-manager **reference** rather than key material, and a four-state lifecycle (`next`/`current`/`previous`/`retired`) | `PLAN/04` § `signing_keys` |
| PG-03 | No storage for MFA factors; `users.mfa_enabled` is a single bool | Added `user_mfa_factors` (TOTP and WebAuthn in one table, several per user) and `user_recovery_codes`; `mfa_enabled` demoted to a denormalized fast flag | `PLAN/04` |
| PG-04 | No storage for invite and password-reset tokens | Added `user_tokens` with a `purpose` discriminator covering invite, reset, and email verification | `PLAN/04` § `user_tokens` |
| PG-05 | No storage for federated identity links | Added `user_identities`, unique on `(provider, provider_subject)` — matched on the provider's stable subject, never on email | `PLAN/04` § `user_identities` |
| PG-06 | No storage for webhook endpoints or deliveries | Added `webhook_endpoints` and `webhook_deliveries` | `PLAN/04` |
| PG-07 | Authorization code storage unspecified | Documented in a new "What Is Deliberately Not Stored Here" table: Redis, sub-60-second TTL, atomic redemption | `PLAN/04` |
| PG-08 | `PLAN/05` placed SAML in Phase 2 while `PLAN/03`, `PLAN/16`, and `PLAN/17` placed it in Phase 4 | Corrected `PLAN/05`'s standards table to Phase 4 | `PLAN/05` § Standards Used |
| PG-09 | `PLAN/18` R-04 described subset validation as happening at grant creation — weaker than the four documents requiring it on every request | Rewrote R-04's mitigation to state "on every request, not only at grant-creation time," with the reason | `PLAN/18` R-04 |
| PG-10 | No retention policy for the unbounded `events` table | Added a Retention and Growth section: monthly partitioning, 24-month hot retention (a default pending confirmation — see OQ-09), and pseudonymization rather than deletion for erasure requests | `PLAN/04` |
| PG-11 | Session storage split between Redis and PostgreSQL was unreconciled | Documented PostgreSQL as authoritative and Redis as a cache in front of it; added `org_id`, `last_seen_at`, and `revoked_at`; cross-referenced from `PLAN/07` | `PLAN/04`, `PLAN/07` |

**A twelfth gap, found while fixing the other eleven**: `refresh_tokens` had no way to track a rotation family, which Phase 3's reuse detection (`P3-06`) depends on entirely. Added `family_id`, `replaced_by`, `session_id`, and `family_expires_at`, so Phase 3 changes behavior rather than storage shape.

**Downstream task updates**: `P0-07` now lists the expanded schema and states which tables may be deferred to the phase that uses them; `P1-03`, `P1-06`, `P1-19`, `P2-01`, `P3-02`, `P3-06`, `P4-10`, and `P4-12` now name the tables they operate on.

### One new open question this created

#### OQ-09 — Confirm the audit log retention period

**Affects**: `P0-07` (partitioning), `P5-10` (export), and any future compliance audit.

`PLAN/04`'s new Retention and Growth section sets **24 months hot** as a working default, because an unbounded table with no stated policy is worse than one with a reviewable number. But the correct value is a legal and business question, not a technical one: too short fails an applicable retention obligation, too long is unnecessary liability. Confirm before production.

The erasure approach — pseudonymize the actor reference rather than delete the event row — should also be confirmed with whoever owns data protection, since it is the standard reconciliation of an immutable audit log with a right to erasure but is a position, not a certainty.

---

## New Plan Gaps

The original eleven were closed on 2026-09-08 by amending the plan documents. These are gaps found since, during implementation.

### PG-12 — `color-border` has two jobs with different accessibility requirements

**Affects**: `P0-17`, and every form and table screen in Phase 1.

`UI-UX/05-DESIGN-SYSTEM.md` gives one `color-border` token for "dividers, table borders, input borders". Those are not the same requirement:

- An **input's border** is the visual information that identifies a UI component, so WCAG 2.1 AA (1.4.11 Non-text Contrast) requires **3:1** against its background. `UI-UX/13` targets AA.
- A **table divider** is decorative. At 3:1 it reads as a heavy grid, which works against `UI-UX/00`'s density principle — dense tables want a hairline, not a rule.

`P0-17` set the single token to `#828d9c` (3.1:1 on base, 3.4:1 on surface), erring toward the accessible reading, because a pretty divider that makes every text input's boundary fail is the worse trade. Tables will look heavier than they should until this is resolved.

**Recommendation**: split into two tokens in `UI-UX/05` — `color-border` (controls, 3:1, the current value) and `color-border-subtle` (dividers, hairline). That is a design-system change, and `UI-UX/05` § Governance requires it to happen there before a screen uses it, so it is raised rather than made.

**Until then**: no screen may introduce its own lighter divider colour. That would be a raw value, the lint rule rejects it, and the rejection is correct — the fix is the token, not the exception.

---

## Deferred

Carried over from `PLAN/01-PRODUCT-SCOPE.md` § Out of Scope, recorded here so the deferral is visible during execution rather than only in the scope document.

| ID | Item | Status per `PLAN/01` | Revisit when |
|---|---|---|---|
| DF-01 | Full identity governance (access certification, automated review) | Deferred indefinitely | A compliance requirement appears |
| DF-02 | Fully white-labeled per-tenant UI | Deferred | Basic org branding proves insufficient |
| DF-03 | No-code visual policy builder | Deferred | Only if the Rego editor proves unusable for real authors |
| DF-04 | Big-bang migration | Explicitly rejected | Never — migration is gradual, app by app |
| DF-05 | Full SCIM provisioning | Deferred to Phase 4, optional | A named external IdP integration needs it (`P4-13`'s gate) |
| DF-06 | Database-per-tenant isolation | Deferred indefinitely | A contractual isolation requirement appears |
| DF-07 | One user in multiple organizations (workspace switching) | Deferred | A real need appears; `PLAN/18` R-09 warns specifically against building it prematurely |
| DF-08 | ABAC (Phase 4b) | Conditional | `P4B-00`'s gate is satisfied |
| DF-09 | Back-channel / RP-initiated single logout | Deferred per `PLAN/05` | A consumer application needs it |
| DF-10 | CAPTCHA after repeated login failures | Optional / later per `PLAN/05` | Rate limiting alone proves insufficient |
| DF-11 | Multi-region deployment | Per `PLAN/15`, only on confirmed need | A cross-region HA requirement is confirmed |
| DF-12 | Separate event store instead of the `events` table | Per `PLAN/07`, evaluate if volume grows | `events` volume becomes a performance problem — see PG-10 |

---

## Suppressed Lint Rules Awaiting Re-enablement

Rules turned off with a stated shelf life. Each is off because it currently fires on the parts of the codebase that are most correct, and each becomes meaningful again at a named moment. They are tracked here rather than only in a code comment, because a comment explaining why a check is disabled is exactly the kind of thing that outlives its own reasoning.

| ID | Rule | File | Off because | Turn back on when |
|---|---|---|---|---|
| SL-01 | `no-unused-components` | `openapi/redocly.yaml` | The shared error, pagination and parameter components exist so Phase 1 endpoints reuse them rather than each inventing its own error shape. In Phase 0 almost none has a caller, so the rule fires fifteen times on the file's most deliberate content — and fifteen warnings people learn to scroll past is how a real one goes unnoticed | The Phase 1 management endpoints land (`P1-05` onward) and an unused component starts meaning something again |
| SL-03 | OpenAPI 3.1 | `openapi/openapi.yaml` | `oapi-codegen` prints "3.1.x is not yet supported ... some functionality may not be available" and generates anyway. The value of spec-first rests on the generated interface being trustworthy enough to be the enforcement mechanism, and a generator that says it may be silently incomplete cannot be that. Nothing in the contract needs 3.1 | `oapi-codegen` supports 3.1 ([issue #373](https://github.com/oapi-codegen/oapi-codegen/issues/373)). The spec then regains `info.summary`, `license.identifier`, and schema-level `examples` arrays |
| SL-02 | `operation-4xx-response` | `openapi/redocly.yaml` | The only operations in the spec are `/healthz` and `/readyz`: unauthenticated, parameterless, and genuinely incapable of a 4xx. Satisfying the rule would mean inventing responses the endpoints do not return, which is worse than the warning | The first authenticated endpoint is added. From that point a missing 4xx is a real omission, and this rule should be `error`, not merely on |

---

## How to Close an Entry

- **OQ** — the user decides; record it in `MEMORY/DECISIONS.md`, update the affected tasks, and strike the entry here with a link to the decision.
- **PG** — either amend the plan document (a deliberate, code-owner-reviewed edit per `AGENTS.md` rule 9) or record an implementation decision in `MEMORY/DECISIONS.md` explaining what was built and why the plan was not changed.
- **SL** — re-enable the rule, fix whatever it then reports, and delete the row. If the rule turns out to be wrong for this codebase rather than merely early, replace the row with a decision in `MEMORY/DECISIONS.md` saying so — a permanent suppression is a decision, not a deferral.
- **DF** — only reopens with a documented, concrete need. "It would be useful" is not a concrete need; `PLAN/00`'s design principles say so directly.
