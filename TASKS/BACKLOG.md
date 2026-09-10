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

Written in English to match the existing 49 documents across `docs/PLAN/`, `docs/UI-UX/`, and `docs/SECURITY/`, so the whole corpus reads as one body of work and cross-references stay natural. If Indonesian is preferred for these two folders, say so and they can be translated — the structure is unaffected.

### ~~OQ-03 — Deployment target~~ — ANSWERED 2026-09-08

**Answer**: a **self-managed VM** for now, with **Kubernetes support planned later**.

Recorded as [ADR-011](../MEMORY/DECISIONS.md). Consequences worked through in `P0-14` (secrets) and `P0-20` (staging). The architecture stays deployment-agnostic — configuration by environment variable, no local disk state, stateless service — so the move to Kubernetes is a manifest change rather than a rewrite.

**This answer creates one open deviation**, tracked below as `DV-01`, because a single VM cannot satisfy `docs/PLAN/14`'s and `docs/PLAN/15`'s Multi-AZ requirement for production.

### ~~OQ-11 — Does GitHub Actions get a deploy path to the VM?~~ — ANSWERED 2026-09-08

**Answer**: **No.** The VM has no public IP and nothing external can reach it, so GitHub Actions will not be used for deployment.

Deployment is **pull-based**: the VM runs `git pull`, receives or builds artifacts, and restarts its own services. Nothing outside the network holds a credential to the machine — the property that matters more than the convenience of a push-based pipeline, and the direction this question recommended.

**Consequence for `P0-20`**: its Definition of Done says "a merge to `main` deploys to staging automatically and runs a smoke test". That will not be satisfied as written, and the item should be **re-read rather than left failing**. The intent was fast, repeatable deployment; a pull on the VM achieves it without the trust relationship the phrasing assumed. Worth amending the task card when someone next touches it.

Recorded in [the clone-based deploy record](../MEMORY/records/2026-09-08-clone-based-deploy-and-state-separation.md).

### ~~OQ-12 — Where do backups go?~~ — ANSWERED 2026-09-08

**Answer**: local-only for now; **S3 or NFS** in future.

Accepted deliberately rather than overlooked. The cost was demonstrated the same day: re-cloning the repository destroyed every backup, and only the Docker volume saved the data.

`backup.sh` warns on every run that backups are local-only, and **that warning should keep appearing** until `AUTH_BACKUP_REMOTE` is set. It is not noise; it is the accurate description of a known gap.

### OQ-10 — Is there a numeric Lighthouse bar for the public site?

**Blocks**: the last item of `P0-18`'s Definition of Done.

That item reads "Lighthouse performance and SEO scores meet the bar set in `docs/UI-UX/20` § Cross-Page Requirements". That section sets an accessibility bar — the same WCAG 2.1 AA as the console — and no numeric performance or SEO target. There is nothing to measure against.

The site is statically generated, ships a sitemap, canonical URLs and per-page metadata, and has no render-blocking third-party script, so it is likely to score well. "Likely to score well" is not a gate.

**Recommendation**: either set a number in `docs/UI-UX/20` (90+ on performance, accessibility, best practices and SEO for the landing page and one docs page is a conventional bar), or delete the item from the DoD and rely on the accessibility checks that do exist. The second is defensible: `docs/PLAN/20` names SEO as critical without quantifying it, and a score threshold nobody chose is a gate that gets waived the first time it fails.

### OQ-04 — Email delivery provider

**Blocks**: `P1-19.1`, `P1-19.4`, `P3-08`.

Invitations, password resets, and anomaly notifications all require outbound email, and none of the plan documents name a provider or approach. This also carries a security dimension: SPF, DKIM, and DMARC configuration matters for an identity provider, since password reset emails are a prime phishing target.

### OQ-05 — Concrete RPO and RTO values

**Blocks**: `P5-07`'s pass/fail judgment.

`docs/PLAN/15` deliberately leaves both as "to be set based on consumer-app SLAs," and warns against leaving them as placeholders in the production runbook. The DR drill can be executed without them, but it cannot be judged successful or unsuccessful without them.

### OQ-06 — Capacity assumptions

**Affects**: `P0-20` sizing, `P5-04` load test design.

`docs/PLAN/12` asks for capacity planning based on expected concurrent users, token issuance rate, and authz check rate — none of which are stated anywhere. Load testing needs target numbers, or it measures an arbitrary shape of traffic.

### OQ-07 — Number of organizations at launch

**Affects**: `P2-08`, `P2-09`.

`docs/PLAN/02` assumes a single default organization initially. If the actual launch is multi-tenant from day one, `P2-09`'s tenant resolution strategy needs deciding earlier and the Phase 1 assumptions shift.

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

`deploy.sh` runs correctly on the VM, but nothing triggers it from CI. That needs either an SSH deploy key held as a repository secret, or a self-hosted runner on the VM. Both are credential decisions with real security consequences — a deploy key in GitHub Actions is a key that can reach production, and a self-hosted runner executes untrusted PR code on the deployment host unless carefully restricted (`docs/SECURITY/02` §18).

**Recommendation**: a self-hosted runner restricted to `main` only, never to pull requests. Awaiting the owner's decision.

### OQ-08 — Which two applications are the MVP consumers?

**Affects**: `P1-26`, `P1-28`.

`docs/PLAN/01`'s MVP definition of done requires two internal applications authenticating real users. `P1-26` builds two demo applications, which proves the mechanism — but the acceptance criterion says *internal applications*, implying real ones. Are there specific applications lined up, and are their teams ready to integrate during Phase 1?

---

## Open Deviations

A deviation is a place where the built system knowingly differs from what `docs/PLAN/`, `docs/UI-UX/`, or `docs/SECURITY/` specifies. Per the deviation protocol in `00-TASK-CONVENTIONS.md`, each carries an ADR, is visible here rather than only in a commit message, and either closes or is accepted as a risk in `docs/PLAN/18-RISK-REGISTER.md`.

### DV-01 — Single-VM production cannot meet the Multi-AZ requirement

**Affects**: `P0-20`, `P5-06` (horizontal scale-out verification), `P5-07` (DR drill), and `docs/PLAN/17` Phase 5 sign-off.
**ADR**: [ADR-011](../MEMORY/DECISIONS.md).
**Status**: Open — accepted for the interim, must be closed or formally risk-accepted before production sign-off.

`docs/PLAN/14-DEPLOYMENT.md` § Environment Strategy specifies production as "Multi-AZ minimum". `docs/PLAN/15-DISASTER-RECOVERY.md` § High Availability repeats it. `docs/PLAN/02-REQUIREMENTS.md` sets availability to "match or exceed the strictest consumer app's SLA", and `docs/PLAN/09` opens by noting a compromise here compromises every dependent application — the same logic applies to an outage.

A single VM is a single point of failure for every consumer application's login. That is a real gap, not a technicality:

- **No instance redundancy.** A VM reboot, a kernel panic, or a failed deploy takes authentication down platform-wide. Running several containers on one host survives a process crash, not a host failure.
- **No database failover.** `docs/PLAN/14` § Scalability assumes read replicas and a single primary; one VM has neither.
- **`P5-06` cannot pass as written.** `docs/PLAN/12` requires horizontal scaling to be "verified in practice… by actually running a scale-out test", and a single host cannot demonstrate multi-node scaling.
- **`P5-07`'s DR drill is weaker.** Restoring into "an isolated environment" (`docs/PLAN/15`) means a second machine that does not exist yet.

**Why it is nonetheless reasonable now**: this is explicitly interim, the consumer applications that would define an SLA do not exist yet (`OQ-08` is still open), and `docs/PLAN/00`'s incremental principle argues against building multi-region HA before a single tenant is live. `docs/PLAN/15` says the same about multi-region: "don't build it speculatively."

**What closes it**: the planned Kubernetes migration, with at least two nodes and a Postgres primary/replica pair. Until then, `P0-20` implements the strongest single-host posture available — offsite backups in a separate failure domain, a verified restore, and a documented recovery time — so the gap is bounded and measured rather than unknown.

**Do not let this deviation quietly expire.** Before any consumer application depends on this service in production, either the Kubernetes migration lands or `docs/PLAN/18` carries an explicit, owned, dated accepted-risk entry saying that single-VM availability is acceptable for the named consumers.

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
| PG-01 | `roles` had no permission keys column, though `docs/PLAN/08` Part A requires them | Added `permission_keys text[]` and `is_builtin bool`, with a note explaining the array-over-join-table choice | `docs/PLAN/04` § `roles` |
| PG-02 | No storage for signing keys, though rotation with overlap and rollback safety both require DB-held key state | Added `signing_keys` — `kid`, purpose (`oidc`/`saml`), algorithm, public key, a secret-manager **reference** rather than key material, and a four-state lifecycle (`next`/`current`/`previous`/`retired`) | `docs/PLAN/04` § `signing_keys` |
| PG-03 | No storage for MFA factors; `users.mfa_enabled` is a single bool | Added `user_mfa_factors` (TOTP and WebAuthn in one table, several per user) and `user_recovery_codes`; `mfa_enabled` demoted to a denormalized fast flag | `docs/PLAN/04` |
| PG-04 | No storage for invite and password-reset tokens | Added `user_tokens` with a `purpose` discriminator covering invite, reset, and email verification | `docs/PLAN/04` § `user_tokens` |
| PG-05 | No storage for federated identity links | Added `user_identities`, unique on `(provider, provider_subject)` — matched on the provider's stable subject, never on email | `docs/PLAN/04` § `user_identities` |
| PG-06 | No storage for webhook endpoints or deliveries | Added `webhook_endpoints` and `webhook_deliveries` | `docs/PLAN/04` |
| PG-07 | Authorization code storage unspecified | Documented in a new "What Is Deliberately Not Stored Here" table: Redis, sub-60-second TTL, atomic redemption | `docs/PLAN/04` |
| PG-08 | `docs/PLAN/05` placed SAML in Phase 2 while `docs/PLAN/03`, `docs/PLAN/16`, and `docs/PLAN/17` placed it in Phase 4 | Corrected `docs/PLAN/05`'s standards table to Phase 4 | `docs/PLAN/05` § Standards Used |
| PG-09 | `docs/PLAN/18` R-04 described subset validation as happening at grant creation — weaker than the four documents requiring it on every request | Rewrote R-04's mitigation to state "on every request, not only at grant-creation time," with the reason | `docs/PLAN/18` R-04 |
| PG-10 | No retention policy for the unbounded `events` table | Added a Retention and Growth section: monthly partitioning, 24-month hot retention (a default pending confirmation — see OQ-09), and pseudonymization rather than deletion for erasure requests | `docs/PLAN/04` |
| PG-11 | Session storage split between Redis and PostgreSQL was unreconciled | Documented PostgreSQL as authoritative and Redis as a cache in front of it; added `org_id`, `last_seen_at`, and `revoked_at`; cross-referenced from `docs/PLAN/07` | `docs/PLAN/04`, `docs/PLAN/07` |

**A twelfth gap, found while fixing the other eleven**: `refresh_tokens` had no way to track a rotation family, which Phase 3's reuse detection (`P3-06`) depends on entirely. Added `family_id`, `replaced_by`, `session_id`, and `family_expires_at`, so Phase 3 changes behavior rather than storage shape.

**Downstream task updates**: `P0-07` now lists the expanded schema and states which tables may be deferred to the phase that uses them; `P1-03`, `P1-06`, `P1-19`, `P2-01`, `P3-02`, `P3-06`, `P4-10`, and `P4-12` now name the tables they operate on.

### One new open question this created

#### OQ-09 — Confirm the audit log retention period

**Affects**: `P0-07` (partitioning), `P5-10` (export), and any future compliance audit.

`docs/PLAN/04`'s new Retention and Growth section sets **24 months hot** as a working default, because an unbounded table with no stated policy is worse than one with a reviewable number. But the correct value is a legal and business question, not a technical one: too short fails an applicable retention obligation, too long is unnecessary liability. Confirm before production.

The erasure approach — pseudonymize the actor reference rather than delete the event row — should also be confirmed with whoever owns data protection, since it is the standard reconciliation of an immutable audit log with a right to erasure but is a position, not a certainty.

---

## New Plan Gaps

The original eleven were closed on 2026-09-08 by amending the plan documents. These are gaps found since, during implementation.

### PG-12 — `color-border` has two jobs with different accessibility requirements

**Affects**: `P0-17`, and every form and table screen in Phase 1.

`docs/UI-UX/05-DESIGN-SYSTEM.md` gives one `color-border` token for "dividers, table borders, input borders". Those are not the same requirement:

- An **input's border** is the visual information that identifies a UI component, so WCAG 2.1 AA (1.4.11 Non-text Contrast) requires **3:1** against its background. `docs/UI-UX/13` targets AA.
- A **table divider** is decorative. At 3:1 it reads as a heavy grid, which works against `docs/UI-UX/00`'s density principle — dense tables want a hairline, not a rule.

`P0-17` set the single token to `#828d9c` (3.1:1 on base, 3.4:1 on surface), erring toward the accessible reading, because a pretty divider that makes every text input's boundary fail is the worse trade. Tables will look heavier than they should until this is resolved.

**Recommendation**: split into two tokens in `docs/UI-UX/05` — `color-border` (controls, 3:1, the current value) and `color-border-subtle` (dividers, hairline). That is a design-system change, and `docs/UI-UX/05` § Governance requires it to happen there before a screen uses it, so it is raised rather than made.

**Until then**: no screen may introduce its own lighter divider colour. That would be a raw value, the lint rule rejects it, and the rejection is correct — the fix is the token, not the exception.

### PG-13 — `max_age_days` is specified as policy but nothing records when a password changed

**Affects**: `P1-02`, and `P1-11`/`P1-12` which would enforce expiry at login.

`docs/PLAN/08-AUTHORIZATION.md` Part B § Policies per Organization specifies the password policy shape as `{ "min_length", "require_uppercase", "max_age_days" }`, and `P0-07`'s migration writes exactly that as the default for every organization. `docs/PLAN/04-DATA-MODEL.md` § `users` lists nine columns and none of them is when the password was last set.

So `max_age_days` is, as specified, unenforceable. Two of the three policy fields can be evaluated against a candidate password; the third can only be evaluated against a fact the schema does not hold. A policy value an administrator can set and the system can never act on is worse than an absent one — it reads as a control and is not.

**Resolved in `P1-02`** by an additive migration adding `users.password_changed_at timestamptz` (nullable, no backfill). Added now rather than with the login work that enforces it, because the column accumulates data: every password set before it exists is a row where "never recorded" and "set long ago" are the same NULL.

NULL is treated as **not expired**, deliberately. The alternative — unknown means infinitely old — makes deploying the migration a mass lockout, which is an outage wearing a security control's clothes.

**`docs/PLAN/04` should be amended** to list the column, through the deliberate plan-change process (`AGENTS.md` rule 9). Raised rather than made, since `docs/PLAN/` is reference material during feature work (`CLAUDE.md`).

---

### PG-15 — `refresh_tokens` has no scope column, so a refresh cannot know what was granted

**Affects**: `P1-07`, and `P3-06` when it adds rotation.

`docs/PLAN/04` § `refresh_tokens` lists eleven columns and none of them records the scope the token was issued for. A refresh therefore has nothing to reproduce: it can carry no scope at all, or re-derive one from the client's registration — and those are different things. Scope is what the **user** consented to at the authorization endpoint; the client's `grant_types` is only what the client is permitted to ask for.

The rule that needs it is that a refresh may narrow scope and never widen it. Widening would make the refresh token more powerful than the consent that created it, which is exactly what a refresh token must not be — and narrowing needs an original to narrow from.

**Resolved in `P1-07`** by an additive `refresh_tokens.scope text[] NOT NULL DEFAULT '{}'`.

**`docs/PLAN/04` should be amended** to list the column, through the deliberate plan-change process (`AGENTS.md` rule 9).

---

### PG-14 — The session cookie must not carry the row's primary key

**Affects**: `P1-11`, and every surface that displays or records a session id — `P1-19`'s sessions endpoint, Phase 3's self-service screen, `refresh_tokens.session_id`, and any audit payload.

`docs/PLAN/04` § `sessions` and `P0-07`'s migration comment both say the cookie carries the row's `id`: *"Stored as an opaque cookie ID in the browser."* Opaque it is. Safe to expose it is not, and the data model already contains the places it gets exposed.

`docs/PLAN/05` Part B routes `/v1/organizations/{org_id}/users/{user_id}/sessions`. An organization administrator listing another user's sessions would receive, for each row, the exact string that authenticates as that user. A screen whose entire purpose is to be looked at would be a credential-disclosure endpoint.

It does not stop there. `refresh_tokens.session_id` is a foreign key, so a token row carries a live session credential. A revocation audit event naming its `session_id` writes one into an append-only table with 24-month retention. A support ticket quoting a session id from a screen hands over the account.

**Resolved in `P1-11`** by an additive `sessions.token_hash text` with a unique index. The cookie carries a fresh 256-bit token, the database stores only `sha256(token)`, and `id` stays an internal identifier that is safe to display, join on and audit — the same separation `P1-05` applies to client secrets.

Reusing `id` and simply never displaying it was considered and rejected: that is a rule every future endpoint, screen and log line has to remember, and one of them will not. Separating the credential from the identifier makes the rule unnecessary.

**`docs/PLAN/04` should be amended** to describe the column and to stop describing the cookie as carrying the id, through the deliberate plan-change process (`AGENTS.md` rule 9). Raised rather than made, since `docs/PLAN/` is reference material during feature work (`CLAUDE.md`).

---

### PG-16 — Organization branding is specified, designed and half-implemented, and has nowhere to live

**Affects**: `P1-12` step 7, `P2-14` (organization settings), and `console/src/branding/branding.ts`, which already applies branding that nothing can supply.

`docs/PLAN/01-PRODUCT-SCOPE.md` lists per-organization branding as in scope. `docs/UI-UX/05-DESIGN-SYSTEM.md` bounds it precisely — an organization may override the accent and supply a logo, and nothing else — and `docs/UI-UX/08` gives it a settings screen. `P0-17` implemented the applying half: a closed union of overridable tokens, written that way so an organization can never re-point `color-danger`.

`docs/PLAN/04` § `organizations` has `settings jsonb`, and `docs/PLAN/08` Part B enumerates its four keys: `password_policy`, `mfa_required`, `session_lifetime_hours`, `allowed_login_methods`. Branding is not among them, and no other column or table holds it. So the console can apply branding, the design system says what branding may be, and there is no place any of it is read from — which is how a specified capability becomes a thing each reader invents a shape for.

**Resolved in `P1-12`** with no schema change, by documenting `settings.branding` as a fifth key of the existing `jsonb`:

```json
"branding": { "logo_url": "https://…", "accent_color": "#1d4ed8" }
```

`settings` is already where per-organization policy lives and is already `jsonb`, so the gap is not a missing column — it is a missing specification. A new `organization_branding` table was considered and rejected: it would be a one-row-per-org table holding two nullable strings, joined on every login page render, to hold data that is by definition per-organization configuration.

**Nothing writes it yet.** `P2-14` makes it editable; until then every organization renders unbranded, which `P1-12`'s record states rather than implies.

**`docs/PLAN/08` Part B should be amended** to list the key and its shape, through the deliberate plan-change process (`AGENTS.md` rule 9).

---

### PG-17 — There is no CORS policy anywhere in the plan, and two Phase 1 consumers need one

**Affects**: `P1-08` (userinfo), `P1-15` onward (the Management API), `P1-21` (console login).

`docs/PLAN/12-PERFORMANCE.md` says `/oauth/userinfo` is "often called on every page load by consumer SPAs". An SPA calling it is a browser making a cross-origin request, and no plan document specifies an origin policy — not `docs/PLAN/05-API-CONTRACT.md`, not `docs/PLAN/09-SECURITY.md`, not `docs/PLAN/06-FRONTEND-ARCHITECTURE.md`. The only mention anywhere is `docs/SECURITY/02` §12, which names "tokens exposed via overly permissive CORS configuration" as a leak path — a warning with no rule behind it to follow.

The console makes it concrete rather than hypothetical: it is served from `console.zedth.my.id` and the service answers on `auth.zedth.my.id`, so every Management API call `P1-21` makes is cross-origin.

**`P1-08` sends no CORS headers**, which is the safe default and also the one that does not work from a browser. That is the honest state of affairs: the alternative is inventing an origin policy for an endpoint that returns email addresses, and `Access-Control-Allow-Origin: *` on that endpoint is precisely the sentence `docs/SECURITY/02` warns about.

**The answer has a data-model half too**, which is why this is a plan gap rather than a configuration decision. Doing it properly means a per-application allowed-origins list — an application registered by an organization declares which origins may read responses obtained with its tokens — and `docs/PLAN/04` § `applications` has `redirect_uris` and `post_logout_redirect_uris` and nowhere to put one. A single instance-wide allowlist in configuration would be simpler and would let any tenant's application read any other tenant's user data from a browser.

**Recommendation**: specify it in `docs/PLAN/05` with a per-application `allowed_origins text[]`, defaulting to empty, validated the way `redirect_uris` already is — exact origin match, https only outside loopback. Raised rather than made: it is a data-model change and an API-contract change, and `AGENTS.md` rule 9 puts both behind the deliberate plan-change process.

**Until then**: no endpoint may add CORS headers to unblock a caller. A one-endpoint exception is how an instance-wide `*` arrives.

---

### PG-18 — `email_verified` is an OIDC claim with no column behind it

**Affects**: `P1-08`, `P1-19.5` (invitation acceptance), and any consumer that gates on a verified address.

OpenID Connect Core defines the `email` scope as covering two claims, `email` and `email_verified`. `docs/PLAN/04-DATA-MODEL.md` § `users` has `email` and no verification flag, and nothing in `docs/PLAN/` mentions verification state at all — the invitation flow in `docs/PLAN/05` establishes that somebody could read a message sent to an address, which is exactly what verification means, and never records it.

**`P1-08` omits the claim** rather than returning `false`. The distinction matters: absent means "not asserted", which is true, while `false` means "we checked and it is not verified", which we did not. A consumer that refuses unverified addresses would then refuse every user this service will ever have, on the strength of a value the service made up.

The cost of the omission is smaller than it looks today, because both claims are optional in OIDC and a consumer must already handle their absence. It becomes real when a consumer wants the guarantee.

**Recommendation**: `users.email_verified_at timestamptz` (nullable), set by `P1-19.5` when an invitation is accepted through a link sent to that address, and cleared when the address changes. A timestamp rather than a boolean, for the reason `password_changed_at` is a timestamp: "when" answers questions "whether" cannot, and an audit needs it.

**`docs/PLAN/04` should be amended** through the deliberate plan-change process.

---

### PG-19 — Per-client rate limiting has a requirement and no owner

**Affects**: `P1-09` step 5, `P1-15` onward, and every OAuth endpoint.

`P1-09`'s card says "rate-limit both endpoints per client (`docs/PLAN/05` § Rate Limiting)". That section of **Part A** says only "limit login attempts per account (cooldown, not permanent lockout) and per IP". The per-`client_id` sentence lives in **Part B**, about the Management API: "Rate limits applied per `client_id`/API key (not just IP), with standard `X-RateLimit-*` headers."

So the card cites a policy that does not cover the endpoints it is about, and no Phase 1 task builds the mechanism. `P1-13` is login limiting — per account and per IP, against credential stuffing — and stops there.

**Not implemented in `P1-09`.** The deferral is smaller than it sounds: `/oauth/introspect` and `/oauth/revoke` both require client authentication, so abuse costs an attacker a valid client secret rather than merely a network connection. What it does not bound is a **compromised** client, which is the case a limiter exists for — and `/oauth/token` has the same gap today.

**Recommendation**: `P1-15` owns it. That is where `/v1` gets its middleware, and a per-client limiter built there can serve the OAuth endpoints too rather than being invented twice. It also needs `docs/PLAN/05` Part A amended to say what the policy actually is for protocol endpoints, since today it says nothing.

**Resolved in `P1-15`** by `internal/ratelimit`'s `Quota`/`Quotas`: 600 requests a minute per `client_id`, a fixed window counted by an atomic Lua increment, and `X-RateLimit-Limit`/`-Remaining`/`-Reset` on every `/v1` response. Deliberately a different mechanism from `P1-13`'s cooldown — that one counts failures to make guessing expensive, this one counts requests to bound a client whose credentials are entirely valid and have been stolen. Keyed on the client id, so rotating a secret does not reset the bound.

**Still open, and narrower than the original entry**: the OAuth endpoints (`/oauth/token`, `/oauth/introspect`, `/oauth/revoke`) are not yet behind it. The mechanism now exists and is reusable, so this is a wiring task rather than a design one — tracked as **`BL-06`** below. `docs/PLAN/05` Part A still says nothing about a policy for protocol endpoints and should be amended when that lands.

---

### PG-21 — Idempotency records have nowhere to live

**Affects**: `P1-15`, and every `POST` endpoint from `P1-16` onward.

`docs/PLAN/05` Part B requires `Idempotency-Key` support on `POST` "to make automated provisioning retries safe". `docs/PLAN/04` models no table for it, and its "What Is Deliberately Not Stored Here" section does not mention it either way — so this is a gap rather than a decision.

It is not obvious that the answer is PostgreSQL. Redis holds the short-lived and reconstructible — authorization codes, the session cache, rate-limit counters — and an idempotency record superficially looks like one of those. It is not. The guarantee it makes is to a caller retrying **after a failure**, and the failure that prompts a retry is exactly the kind of event that also restarts things. A record that vanishes turns a safe retry into a duplicate provisioning call, which is the thing the header exists to prevent.

**Resolved in `P1-15`** by an additive `idempotency_records` table: primary key `(org_id, client_id, key)`, the request stored as a **hash** rather than as itself (a user-creation body carries a password), the response as `text` so a replay returns the original bytes, RLS like every other tenant-scoped table, and a `SECURITY DEFINER` sweep because instance-scoped maintenance cannot delete under RLS.

**`docs/PLAN/04` should be amended** to list the table, through the deliberate plan-change process (`AGENTS.md` rule 9).

---

### PG-20 — `docs/PLAN/05` treats RP-Initiated Logout and Back-Channel Logout as one specification

**Affects**: `P1-10` (built anyway), `DF-09`, and any later task that reads the plan for what logout the MVP has.

`docs/PLAN/05` § MFA, Passwordless, Social Login, Logout says:

> "Back-channel logout (RP-Initiated Logout) is a later phase; MVP needs per-app logout + a 'log out of all sessions' button."

The parenthesis makes them synonyms. They are two different OpenID Connect specifications:

- **RP-Initiated Logout 1.0** — the front channel. The browser is redirected to the provider's `end_session_endpoint`, the session ends, and the user is sent back to a registered address. This is what `P1-10` implements.
- **Back-Channel Logout 1.0** — server to server. The provider POSTs a logout token to each relying party's registered `backchannel_logout_uri`. No browser is involved, and it needs a new column, a delivery mechanism and a retry policy.

Neither implies the other, and a provider can implement either alone.

Read literally the sentence defers the endpoint **the same document lists in its own core-endpoint table** (`GET /oidc/logout → end-session (single logout)`), and then asks for "per-app logout" in the MVP — so the document contradicts itself unless the two are separated.

`P1-10`'s card already has it right: step 1 implements RP-initiated logout, step 7 defers back-channel. **Built per the card.** `DF-09` has been narrowed to back-channel alone.

**`docs/PLAN/05` should be amended** to name the two specifications separately, through the deliberate plan-change process (`AGENTS.md` rule 9).

---

---

## Operational Gaps

Found by running the system rather than by reading the plan.

### BL-06 — The OAuth endpoints are not behind the per-client rate limiter

**Affects**: `/oauth/token`, `/oauth/introspect`, `/oauth/revoke`.

`P1-15` built the mechanism `PG-19` asked for and wired it to `/v1` only. The three protocol endpoints that take client credentials are still unbounded per client.

The exposure is narrower than it sounds — all three require client authentication, so abuse costs an attacker a valid client secret rather than a network connection. What is unbounded is a **compromised** client, which is the case the limiter exists for.

This is now a wiring task rather than a design one: `ratelimit.NewQuotas` is reusable as it stands, and the endpoints already know their `client_id` by the time they would call it. Two things need deciding rather than assuming:

- **The bound.** `/v1`'s 600 a minute is sized for an administrator's provisioning   script. A resource server calling `/oauth/introspect` once per incoming request is a   different shape entirely, and giving it the same number would either throttle normal   traffic or bound nothing.
- **`docs/PLAN/05` Part A**, which today says nothing about a policy for protocol   endpoints. It should say what the policy is before one is implemented against it.

---

### BL-05 — `AUTH_CLIENT_IP_HEADER` is not set on staging, so per-IP rate limiting sees one client

**Found**: 2026-09-10, building `P1-13`. **Affects**: staging only.

`cloudflared` runs as a host service and reaches the published port, so the service sees the Docker gateway as `RemoteAddr` for **every request in the world**. `P1-13`'s per-IP bound computed from that is not a per-IP bound; it is a global one, and a single attacker could reach it and lock every user out of the service.

The service warns loudly at startup when no client-IP source is configured, and does **not** disable the bound — a limiter that quietly turns itself off is worse than one that is loudly misconfigured.

**The fix is configuration, not code.** On the VM, in `/home/infra/auth-state/.env`:

```
AUTH_CLIENT_IP_HEADER=CF-Connecting-IP
AUTH_TRUSTED_PROXY_CIDRS=172.16.0.0/12
```

The CIDR must be the range `cloudflared` actually connects from — check `docker inspect` for the bridge subnet before setting it, because a wrong range means the header is ignored and nothing changes, silently.

**The per-address bound is unaffected** and is the one that stops a targeted attack. What is currently missing is the credential-stuffing bound, and the exposure is that reaching it locks out everyone rather than one attacker.

### BL-04 — The public API reference cites internal plan documents by bare path

**Found**: 2026-09-10, moving the reference documentation under `docs/`. **Affects**: `/docs/api-reference` on the public site, and `P1-25`.

`openapi/openapi.yaml` carries 18 citations of the form ``(`docs/PLAN/02` FR-14)``, ``(`docs/PLAN/05` Part B)``, ``(`docs/PLAN/08`)`` inside `description` fields. Those descriptions are rendered verbatim into `/docs/api-reference`, so they are public copy. `public-site/docs/concepts/authorization.md` does the same by hand.

This is **not** a leak — the repository is public, so the cited documents are readable, and `check-no-internal-leak.mjs` is about three specific documents' content rather than about citations. It is a documentation-quality problem: a bare path with no link tells an integrator reading the API reference nothing, and it presumes they know the repository is where the answer lives.

The move made it visible rather than creating it: rewriting the paths produced a diff in generated public content, which is where it was noticed.

**Recommendation**: in `description` fields, make the statement stand on its own and drop the citation, or turn it into a real link to the file on GitHub. The claim "the console is a client of this API and has no privileged path around it" is worth reading; "(`docs/PLAN/02` FR-14)" after it is not. Belongs with `P1-25`, which owns the public documentation, rather than as a drive-by edit to the API contract.

### BL-03 — Cloudflare injects a third-party script into the login page

**Found**: 2026-09-09, deploying `P1-12`. **Affects**: `/login`, and every HTML page this service serves through the tunnel.

Served from the container, the login page has zero script elements. Served through `auth.zedth.my.id` it has one — Cloudflare's Web Analytics beacon, `static.cloudflareinsights.com/beacon.min.js`, injected into the HTML at the edge.

The page's `Content-Security-Policy: default-src 'none'` refuses it, and the browser says so in the console. That is the control working on a real injection, which is worth more than the many tests that prove it works on a synthetic one.

It should still be turned off, for the hostname or the zone (Cloudflare dashboard → Speed → Optimization → Web Analytics / Browser Insights). Two reasons:

- An intermediary is adding a third-party script to the one page in the estate where a password is typed. It is stopped by a header this service sends; if a future edit loosens that header the script starts running, and nothing fails, so nobody finds out. A control that is the only thing between a password form and a third-party script should not also be the only thing.
- It writes a console error on every page load, and a page that always logs an error is a page where the next, real error is not noticed.

**Needs the Cloudflare account**, so it is raised rather than done.

### BL-02 — The identity mark's indigo and the product's accent are different blues

**Affects**: `P0-17`, `P0-18`, and every surface that shows both at once.

The brand concept (`brand/reference/zed-auth-brand-v3.html`) specifies the hub
ring as `#4655F5`. The console and the public site run on
`--color-accent: #1d4ed8`, kept identical by
`public-site/scripts/check-brand-tokens.mjs`.

Both are accessible — `#4655F5` measures 5.41 on white and 5.05 on the console's
`--color-bg-base`, against `#1d4ed8`'s 6.70 and 6.25 — so this is not a
contrast question. It is that the logo's blue and the interface's blue are
visibly different, side by side, in a navbar.

**Not resolved, deliberately.** The brand assets ship with the concept's
palette and the product tokens are untouched, because adopting `#4655F5` as
`--color-accent` is a design-system change governed by `docs/UI-UX/05` and would
touch both surfaces, the token gate and the contrast checks. That is a decision
for the project owner, not a side effect of adding a logo.

Three options when it is taken up:

1. Adopt `#4655F5` as `--color-accent` everywhere. One accent, and the
   contrast numbers above say it is safe. Requires amending `docs/UI-UX/05`.
2. Keep `#1d4ed8` and restate the mark's hub ring in it. Changes the artwork.
3. Accept both, treating the mark's indigo as a brand colour distinct from the
   interface accent. Common, and the least work, but it needs saying out loud
   in `docs/UI-UX/05` or it reads as an oversight.

---

### BL-01 — Nothing watches for a backup that stops happening

**Affects**: `P0-20`, and `docs/PLAN/15-DISASTER-RECOVERY.md` § Restore Testing.

Between 2026-09-08 and 2026-09-09 the nightly backup did not run once, and nothing said so. `P0-14` moved runtime state out of the code checkout; the systemd unit's `ReadWritePaths` still named `/home/infra/auth/backups`, and systemd refuses to start a unit whose `ReadWritePaths` does not exist — exit `226/NAMESPACE`, before `backup.sh` executed a single line.

The failure was maximally quiet. `systemctl list-timers` reported the timer healthy the entire time, because the **timer** was healthy; it fired every night exactly as configured, and the service it triggered died instantly. The only trace was a `systemctl status` nobody ran.

This is `P0-11`'s lesson in a new place: *rules that must catch "stopped happening" cannot be written as a comparison against a value that is never emitted.* A failed backup emits nothing. An alert on `backup_failures > 0` would never have fired.

**What is needed**: a freshness signal, not a failure signal. Something that answers "when did a verified backup last complete" and alerts on the **absence** of a recent one — a `node_exporter` textfile metric written by `backup.sh` on success and alerted on with `time() - auth_backup_last_success_timestamp > 36h`, or a systemd `OnFailure=` unit, or both. The textfile approach is preferred because it also catches the case where the timer itself is disabled, which `OnFailure=` cannot see.

**Interim**: the unit paths are fixed and a verified backup ran on 2026-09-09 (90KB, 19 tables, row counts matching). Until this is closed, the backup's health is only as good as somebody remembering to look.

---

## Deferred

Carried over from `docs/PLAN/01-PRODUCT-SCOPE.md` § Out of Scope, recorded here so the deferral is visible during execution rather than only in the scope document.

| ID | Item | Status per `docs/PLAN/01` | Revisit when |
|---|---|---|---|
| DF-01 | Full identity governance (access certification, automated review) | Deferred indefinitely | A compliance requirement appears |
| DF-02 | Fully white-labeled per-tenant UI | Deferred | Basic org branding proves insufficient |
| DF-03 | No-code visual policy builder | Deferred | Only if the Rego editor proves unusable for real authors |
| DF-04 | Big-bang migration | Explicitly rejected | Never — migration is gradual, app by app |
| DF-05 | Full SCIM provisioning | Deferred to Phase 4, optional | A named external IdP integration needs it (`P4-13`'s gate) |
| DF-06 | Database-per-tenant isolation | Deferred indefinitely | A contractual isolation requirement appears |
| DF-07 | One user in multiple organizations (workspace switching) | Deferred | A real need appears; `docs/PLAN/18` R-09 warns specifically against building it prematurely |
| DF-08 | ABAC (Phase 4b) | Conditional | `P4B-00`'s gate is satisfied |
| DF-09 | **Back-channel** logout (OIDC Back-Channel Logout 1.0) | Deferred per `docs/PLAN/05` | A consumer application needs it. **Narrowed by `P1-10`**: this row used to read "Back-channel / RP-initiated", copying a conflation in the plan. RP-Initiated Logout 1.0 is a different specification and **shipped** in `P1-10`. See `PG-20` |
| DF-10 | CAPTCHA after repeated login failures | Optional / later per `docs/PLAN/05` | Rate limiting alone proves insufficient |
| DF-11 | Multi-region deployment | Per `docs/PLAN/15`, only on confirmed need | A cross-region HA requirement is confirmed |
| DF-12 | Separate event store instead of the `events` table | Per `docs/PLAN/07`, evaluate if volume grows | `events` volume becomes a performance problem — see PG-10 |

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
- **DF** — only reopens with a documented, concrete need. "It would be useful" is not a concrete need; `docs/PLAN/00`'s design principles say so directly.
