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

### ~~OQ-04 — Email delivery provider~~ — ANSWERED 2026-09-10

**Was blocking**: `P1-19.1`, `P1-19.4`, `P3-08`.

Invitations, password resets, and anomaly notifications all require outbound email, and none of the plan documents name a provider or approach. This also carries a security dimension: SPF, DKIM, and DMARC configuration matters for an identity provider, since password reset emails are a prime phishing target.

**Answered** by [ADR-018](../MEMORY/DECISIONS.md): plain SMTP configured by URL, no provider SDK, and nothing waits on delivery — an invite still returns `201` with `invite_email_sent: false`, and a reset returns the same `202` whether the address exists or not, which means it must also be the same whether the send succeeded or not.

**Still open**, and deliberately not code: SPF, DKIM and DMARC for the first domain that sends to a real address. Tracked as an operational follow-up against `P0-20`. Staging sends only to Mailpit and `example.test`.

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

### DV-03 — The demo applications have no public hostnames yet — **RESOLVED 2026-09-11**

**Affects**: `P1-26`'s last two DoD items, `P1-27`'s SSO E2E test, and the Phase 1 exit checklist's first two lines.
**Status**: Closed. Zed added the two Cloudflare tunnel records; both hostnames answer.

Verified end to end through the public names — `scratchpad/p126-public.sh`, 15 assertions, 0 failures. Every hop is a real DNS name over real TLS through the tunnel: `demo-a.zedth.my.id` redirects to the issuer, the issuer asks for a password once, the browser comes back to demo A, and `demo-b.zedth.my.id` then obtains a code **with no second login**. Demo B refuses a token minted for demo A, naming the audience.

No redeploy was needed, as predicted: the containers already held those hostnames as their base URLs.

Both demo applications are deployed, healthy and verified on the staging VM at `127.0.0.1:10940` and `127.0.0.1:10941`. Their registered redirect URIs and their own `DEMO_BASE_URL` say `https://demo-a.zedth.my.id` and `https://demo-b.zedth.my.id`.

What is missing is two Cloudflare tunnel records mapping those hostnames to those ports. The tunnel on this host is token-based and remotely managed, so the mapping is made in the Cloudflare dashboard — the same step that put `console.zedth.my.id` and `app-auth.zedth.my.id` in front of their containers.

**What is blocked until they exist**: walking the SSO flow by hand in a browser (the issuer redirects to a name that does not resolve), and the Playwright E2E test in `P1-27` step 3, which drives a real browser through the same redirect.

**What is not blocked**: everything else. The scripted staging run drives the real issuer over TLS and reaches the applications on loopback, which is how all 33 assertions — including "App B gets a code with no second login" and "App B refuses App A's token" — were verified.

**The moment those records exist**, no redeploy is needed: the containers already believe those are their base URLs.

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

### PG-17 — There is no CORS policy anywhere in the plan, and two Phase 1 consumers need one — **RESOLVED 2026-09-11**

**Affects**: `P1-08` (userinfo), `P1-15` onward (the Management API), `P1-21` (console login).

**Resolved by `P1-29`** ([ADR-020](../MEMORY/DECISIONS.md), [record](../MEMORY/records/2026-09-11-P1-29-cors.md)), with the plan amended rather than worked around: `docs/PLAN/05` § Cross-Origin Access, `docs/PLAN/04` § `applications`, `docs/PLAN/09` § Transport & Storage.

The recommendation below was taken almost unchanged — a per-application `allowed_origins text[]`, empty by default, validated like `redirect_uris`. The one thing it did not anticipate is the split: the token endpoint and the discovery documents needed a wildcard, because a public client cannot complete a login without one and there is no application to resolve at the moment the exchange happens. Everything returning personal data is per application, which was this gap's actual objection.

The rest of this entry is kept as written, because the measurement below is what made the decision, and because the "until then" rule was the right rule right up until it was answered.

---

`docs/PLAN/12-PERFORMANCE.md` says `/oauth/userinfo` is "often called on every page load by consumer SPAs". An SPA calling it is a browser making a cross-origin request, and no plan document specifies an origin policy — not `docs/PLAN/05-API-CONTRACT.md`, not `docs/PLAN/09-SECURITY.md`, not `docs/PLAN/06-FRONTEND-ARCHITECTURE.md`. The only mention anywhere is `docs/SECURITY/02` §12, which names "tokens exposed via overly permissive CORS configuration" as a leak path — a warning with no rule behind it to follow.

The console makes it concrete rather than hypothetical: it is served from `console.zedth.my.id` and the service answers on `auth.zedth.my.id`, so every Management API call `P1-21` makes is cross-origin.

**`P1-08` sends no CORS headers**, which is the safe default and also the one that does not work from a browser. That is the honest state of affairs: the alternative is inventing an origin policy for an endpoint that returns email addresses, and `Access-Control-Allow-Origin: *` on that endpoint is precisely the sentence `docs/SECURITY/02` warns about.

**The answer has a data-model half too**, which is why this is a plan gap rather than a configuration decision. Doing it properly means a per-application allowed-origins list — an application registered by an organization declares which origins may read responses obtained with its tokens — and `docs/PLAN/04` § `applications` has `redirect_uris` and `post_logout_redirect_uris` and nowhere to put one. A single instance-wide allowlist in configuration would be simpler and would let any tenant's application read any other tenant's user data from a browser.

**Recommendation**: specify it in `docs/PLAN/05` with a per-application `allowed_origins text[]`, defaulting to empty, validated the way `redirect_uris` already is — exact origin match, https only outside loopback. Raised rather than made: it is a data-model change and an API-contract change, and `AGENTS.md` rule 9 puts both behind the deliberate plan-change process.

**Until then**: no endpoint may add CORS headers to unblock a caller. A one-endpoint exception is how an instance-wide `*` arrives.

---

#### What `P1-27` measured, 2026-09-11 — this is no longer a forecast

The first time a real browser was pointed at the login flow, it stopped here.

A public client cannot complete the Authorization Code flow without CORS. The authorize step is a **navigation** and needs nothing; the code exchange is a `fetch` to `/oauth/token` from the application's own origin, and the browser refuses it with `Failed to fetch` before the request is sent. That affects:

- **the demo SPA** (`P1-26`), which cannot exchange its code at all, and
- **the console** (`P1-21`), which cannot exchange its code AND cannot make a single Management API call.

So the console has never been able to sign in from a browser, in any deployment where it is not served from the service's own origin — which is every deployment the plan describes, including staging today (`console.zedth.my.id` against `auth.zedth.my.id`). Every test that passed until now drove these flows with `curl`, which has no same-origin policy.

That makes this a **Phase 1 exit blocker**, not a Phase 2 nicety: `docs/PLAN/17`'s checklist requires organizations, projects, applications and users to be creatable through the console, and the console cannot reach the API.

**The split that matters**, and the reason a single answer is wrong:

| Endpoint | What it needs | Why |
|---|---|---|
| `/.well-known/*`, `/oauth/token` | `Access-Control-Allow-Origin: *`, **no credentials** | Public by construction. The token endpoint hands nothing to a caller who cannot present a valid code and its PKCE verifier, and it is not cookie-authenticated, so an origin cannot use a victim's ambient session. Every OIDC provider does this, and a public client is unusable without it. |
| `/oauth/userinfo`, `/v1/*` | The per-application `allowed_origins` above | Bearer-authenticated and returns personal data. `*` here is precisely the sentence `docs/SECURITY/02` §12 warns about. |

The recommendation above is unchanged for the second row. The first row is the part that was not separated before, and separating it is what makes "no blanket `*`" and "a browser can log in" both true at once.

---

### PG-18 — `email_verified` is an OIDC claim with no column behind it

**Affects**: `P1-08`, `P1-19.5` (invitation acceptance), and any consumer that gates on a verified address.

OpenID Connect Core defines the `email` scope as covering two claims, `email` and `email_verified`. `docs/PLAN/04-DATA-MODEL.md` § `users` has `email` and no verification flag, and nothing in `docs/PLAN/` mentions verification state at all — the invitation flow in `docs/PLAN/05` establishes that somebody could read a message sent to an address, which is exactly what verification means, and never records it.

**`P1-08` omits the claim** rather than returning `false`. The distinction matters: absent means "not asserted", which is true, while `false` means "we checked and it is not verified", which we did not. A consumer that refuses unverified addresses would then refuse every user this service will ever have, on the strength of a value the service made up.

The cost of the omission is smaller than it looks today, because both claims are optional in OIDC and a consumer must already handle their absence. It becomes real when a consumer wants the guarantee.

**Recommendation**: `users.email_verified_at timestamptz` (nullable), set by `P1-19.5` when an invitation is accepted through a link sent to that address, and cleared when the address changes. A timestamp rather than a boolean, for the reason `password_changed_at` is a timestamp: "when" answers questions "whether" cannot, and an audit needs it.

**`docs/PLAN/04` should be amended** through the deliberate plan-change process.

---

### PG-37 — `P2-13` is a console task whose Definition of Done needed an endpoint that did not exist

**Affects**: `TASKS/PHASE-2-RBAC-MULTITENANCY.md` § P2-13, `docs/PLAN/05-API-CONTRACT.md`.

The card's surface is **console**, it says "Spec required: No", and its first Definition of Done line reads:

> The switcher lists exactly the organizations the caller administers, **per server-side truth**.

Nothing could answer that. `GET /v1/organizations` requires `INSTANCE_OWNER` by nature — a list that spans tenants cannot be scoped to one — and an `ORG_ADMIN` reads only their own organization. The token's `urn:authservice:manager_roles` claim carries **role names without their scopes**, so it cannot name an organization at all, and `internal/management/store.go` is emphatic that a role in a token is a snapshot this API must not trust.

So the only two ways to satisfy the card were to add an endpoint, or to build the switcher off something that is not server-side truth — which would have satisfied the sentence and broken the requirement.

**What was built**: `GET /v1/me/organizations`, plus a `ScopeSelf` authorization scope for it, plus the `organizations_administered_by` SECURITY DEFINER function. Roughly 300 lines in a task the plan sized as a console `M`.

**The gap is in the planning, not the outcome.** A console task that needs a new endpoint is not a console task, and the roadmap's sizing and dependency graph both assumed otherwise (`P2-13` depends only on `P2-08`). Two more Phase 2 console cards — `P2-14`, and `P2-12`'s roster, already recorded as `PG-36` — sit near the same line.

**Recommendation**: when a console card's DoD says "per server-side truth", the card should name the endpoint it reads, and the roadmap should carry a backend dependency. `docs/PLAN/05` should document `/v1/me/organizations` through the deliberate plan-change process.

---

### PG-36 — Nothing can answer "who has access to this project?"

**Affects**: `P2-12`'s Authorizations tab, `docs/PLAN/17`'s access-review expectations, and any future access certification (`DF-01`).

Grants are stored one row per user per project and exposed **only under the user**: `GET /v1/organizations/{org_id}/users/{user_id}/grants`. There is no `GET .../projects/{project_id}/grants`.

So the console can answer "what access does Budi have in Till?" and cannot answer "who can do anything in Till?" — and neither can any API consumer. The second question is the one an access review starts from, the one an incident starts from, and the one somebody asks before deleting a project.

`P2-12` built the screen the API supports — search a user, then act — rather than faking a roster by issuing a grants request per search result. That would be `N+1` requests producing something that still is not the roster: it would cover only the users matching whatever was typed.

**The workaround today** is to list the organization's users and ask per user, which is exactly the fan-out the console declined to hide. It is fine for a small organization and wrong for a large one.

**Recommendation**: `GET /v1/organizations/{org_id}/projects/{project_id}/grants`, cursor-paginated like every other list, returning `user_id` and `role_keys`. The query is a single indexed read — `user_grants` is already keyed by `(org_id, project_id)` — so this is an endpoint and a handler, not a data-model change. `docs/PLAN/05` should name it, through the deliberate plan-change process.

---

### PG-34 — `P2-11` asks for built-in roles to be non-editable; the API it depends on says only their identity is frozen

**Affects**: `TASKS/PHASE-2-RBAC-MULTITENANCY.md` § P2-11 step 5 and its Definition of Done, and the Roles tab that implements them.

The card says:

> Built-in roles render as **non-editable** with a clear explanation.

The API it is built on says the opposite about everything except identity. `openapi/openapi.yaml`, `updateRole`:

> A built-in role's `display_name` and permissions may still be edited; only its identity is frozen.

Both are deliberate. The card was written before `P2-02` worked out what "built-in" costs, and `P2-02` landed on a narrower freeze on purpose: a service-owned role whose **label** cannot be corrected is a typo nobody can fix, while a role whose **key** can change is a rename that silently revokes access from everyone holding it.

**What was built**: the console matches the API. A built-in role's key is read-only and says why, its delete control is replaced by the reason it is absent, and its name and permissions are editable like any other role's. Making the console stricter than the API would have produced a capability reachable only by `curl`, which is the inverse of the API-first rule and just as confusing.

**The card should be amended** to "built-in roles cannot be deleted or re-keyed, and say so" — a wording change, through the deliberate process (`AGENTS.md` rule 9), not a code change. Nothing is broken today because **no built-in roles are seeded** (`PG-30`), so the divergence has no live behaviour behind it yet; it will the moment one is.

---

### PG-35 — The project detail is specified as tabs and no tab component is specified

**Affects**: `docs/UI-UX/07-COMPONENT-SPECIFICATION.md`, `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` § Project detail, `P2-11`, `P2-12`.

`docs/UI-UX/08` names three tabs on the project detail — Applications, Roles, and Authorizations from `P2-12` — and `docs/UI-UX/13` requires every interactive pattern to have a stated keyboard model. `docs/UI-UX/07` specifies Table, Badge, Button, Modal, Side Panel, Breadcrumb, Confirmation Dialog and Form Field. **It does not specify a tab.**

`UserDetailPage` already renders real ARIA tabs, because its tabs genuinely are panels inside one document. The project detail's are not: each is a route with its own data, loading state, error state and back-button behaviour.

`P2-11` implemented the project detail's as what they are — a labelled `nav` of links with `aria-current` — rather than inventing a `Tabs` component that a later specification would have to contradict. The reasoning is in `console/docs/implementation-chain-P2-11.md`.

**`docs/UI-UX/07` should say which of the two a screen gets and when**, because the next person to add a tabbed screen has two working examples in this codebase that disagree, and no document saying they are supposed to.

---

### PG-33 — Part B lists four tenant-resolution options and the service uses a fifth

**Affects**: `docs/PLAN/08-AUTHORIZATION.md` Part B § Tenant Resolution, and anyone reading it to learn how a request finds its organization.

Part B says:

> Options (combinable): by domain (`acme.auth.company.com`), by path, by email domain at login, or a single default organization for purely internal deployments. MVP: single default organization, schema kept multi-tenant-ready.

The service does none of those. **The organization is the one that owns the OIDC client the request is authenticating to** — determined before a password is typed, from a value the caller must already supply for the protocol to work.

That is not a deviation anybody chose: it fell out of `P1-05` giving applications an `org_id` and `P1-06` resolving the client before anything else. It is also better than the four for a specific reason — there is nothing for a caller to supply and therefore nothing to forge, where a subdomain is a `Host` header and a path segment is a path.

It happens to subsume the MVP mode Part B wanted: with one organization owning every client, every request resolves to it with no special case.

Recorded and reasoned in [ADR-023](../MEMORY/DECISIONS.md). **Part B should name it**, as the default with the other four available as additions for a deployment that needs a tenant chosen before a client is named. One paragraph; a plan change, so it goes through the deliberate process (`AGENTS.md` rule 9) rather than through the task that noticed.

---

### PG-31 — Nothing anywhere creates an API for assigning manager roles

**Affects**: `P2-05` steps 6 and 7, `P2-13`'s organization switcher, the console's ability to show who administers anything, `PG-26`'s bootstrap problem, and every deployment after the first.

`manager_roles` is the table that decides who may administer this service — `INSTANCE_OWNER`, `ORG_OWNER`, `ORG_ADMIN`, `PROJECT_OWNER`, `PROJECT_GRANT_OWNER`. It has existed since `P0-07`, it is read on every `/v1` request, and **no endpoint writes it**. `openapi/openapi.yaml` does not contain the word "manager".

Searched across all seven phase files: no task creates one. `P2-05` is the closest, and it is about *enforcing* the hierarchy rather than administering it. So two of its own steps have nothing to attach to:

- Step 6, "require confirmation and audit for granting `INSTANCE_OWNER` or `ORG_OWNER`" — there is no grant operation to confirm or audit.
- Step 7, "guard against removing the last `ORG_OWNER`" — there is no removal to guard.

Every manager role in every environment so far was created with a direct `INSERT`. `P1-19`'s record says so plainly, the staging bootstrap script does it, and `PG-26` describes the same wall from the other side.

**Why it is more than an inconvenience.** An administrator cannot be given or taken away through any published surface. A departing employee's `ORG_OWNER` is removed by somebody with database access, if they remember — and `docs/SECURITY/02` §3's privilege-escalation scenarios are all about a table nobody can inspect through the API. The console cannot answer "who can administer this organization", which is the first question in an incident.

**Recommendation**: `/v1/organizations/{org_id}/managers` with list, grant and revoke, `ORG_OWNER` to write and `ORG_ADMIN` to read, plus the two guards `P2-05` already specifies — confirmation and audit for the powerful roles, and a refusal to remove the last `ORG_OWNER`. The authorization model to enforce it exists as of `P2-05`; what is missing is only the endpoint.

This is a **roadmap gap rather than a plan contradiction**: `docs/PLAN/08` describes the roles correctly and `docs/PLAN/05` never promised the endpoint. Adding the task goes through the deliberate process.

---

### PG-32 — The hierarchy diagram does not say whether an ORG_ADMIN administers a project

**Affects**: `P2-05`'s inheritance table, `P2-02`'s authorization, and every endpoint under a project.

`docs/PLAN/08` Part C draws the hierarchy as a tree:

```
INSTANCE_OWNER --> ORG_OWNER
ORG_OWNER      --> ORG_ADMIN
ORG_OWNER      --> PROJECT_OWNER
PROJECT_OWNER  --> PROJECT_GRANT_OWNER
```

with the rule "permissions flow downward only". Read strictly, `ORG_ADMIN` and `PROJECT_OWNER` are **siblings**: neither inherits the other, so an endpoint requiring `PROJECT_OWNER` refuses an `ORG_ADMIN`.

That reading contradicts two things. The diagram's own label for `ORG_ADMIN` is "org access, except deleting org/changing owner" — and a project is inside the organization. And Phase 1 already ships `ORG_ADMIN` creating, editing and listing projects and applications (`P1-17`, `P1-18`), which is project administration by any reading; under the strict interpretation those endpoints have been wrong since they were written.

It also produces an incoherent system: an `ORG_ADMIN` could create a project but not manage the roles inside it.

**What `P2-05` implemented**: `ORG_ADMIN` satisfies `PROJECT_OWNER` **within its own organization**, and never outside it. Downward-only still holds in the direction that matters — a `PROJECT_OWNER` never gains `ORG_ADMIN`, and cannot reach anything outside its one project. The reasoning is in `internal/management/roles.go` beside the table, not only here.

**What the plan should say**: that the tree shows *delegation paths* rather than an exclusive partition, or that an organization-scoped role covers every project in its organization. One sentence either way; picking it is a plan change and goes through the deliberate process (`AGENTS.md` rule 9).

---

### PG-30 — The two "built-in roles" the plan names are not roles of that kind

**Affects**: `P2-01` (the `roles` entity and its `is_builtin` column), `P2-02`, `P2-11`, and anyone reading `docs/PLAN/08` Part A to learn the role model.

`docs/PLAN/08-AUTHORIZATION.md` Part A, under Role Structure:

> Roles can be **built-in** (`org_owner`, `org_admin`) or **custom** (created by an organization admin).

Three lines above it, the same section establishes that roles are defined **per Project** — "so 'admin' in Project A doesn't automatically become 'admin' in Project B".

`ORG_OWNER` and `ORG_ADMIN` are not project-scoped and are not that kind of role. Part C of the same document says so directly:

> `manager_roles`: administrative roles (`INSTANCE_OWNER`, `ORG_OWNER`, `ORG_ADMIN`, `PROJECT_OWNER`, `PROJECT_GRANT_OWNER`) — these govern who can **administer the Auth Service itself**, distinct from application-level roles that govern access **within consumer applications**.

`docs/PLAN/04-DATA-MODEL.md` agrees: they are an enum on `manager_roles`, a different table with a different shape (`user_id` + `role` + `scope_id`, no `project_id`, no `permission_keys`). They have been implemented that way since `P1-15`, and the `ORG_ADMIN` the console holds today is a `manager_roles` row.

So Part A's parenthetical takes two manager roles and offers them as examples of built-in project roles. Read literally it asks `P2-01` to seed every project with an `org_owner` role carrying permission keys — which would be a second, parallel definition of an identity the service already has somewhere else, and the first place a permission check would consult the wrong one.

**What `P2-01` does about it.** `is_builtin` is implemented as the mechanism the data model specifies — a role so marked cannot be renamed or deleted — and **no built-in roles are seeded**, because the only two the plan names belong to another table. The column is not speculative: `P2-05` (manager role enforcement) and `P4-01` (Project Grants) both have reasons to want a role the organization cannot remove, and the mechanism should exist before something needs it rather than be retrofitted around live data.

**What the plan should say.** Either name project-scoped built-in roles that actually make sense for a consumer application to start with, or say plainly that `is_builtin` exists for roles the *service* creates and that none are seeded in Phase 2. Both are small edits; picking one is a plan change and goes through the deliberate process (`AGENTS.md` rule 9), not through this task.

**Why it matters beyond wording.** `docs/PLAN/08` is named in `CLAUDE.md` as "the single source of truth for all authorization logic". A contradiction inside the source of truth is not a typo — the next person to implement a permission check has two documented answers to "what is a role" and no way to tell which one the author meant.

---

### PG-29 — The backup cannot restore the one secret the recovery procedure depends on

**Affects**: `docs/PLAN/15-DISASTER-RECOVERY.md`, `deploy/vm/backup.sh`, the nightly `zed-auth-backup.timer`, and any recovery of this service anywhere.

`docs/PLAN/15` line 9 states it as a design decision:

> Signing keys backed up separately from the database, with restricted access.

**Nothing does this.** `backup.sh` runs `pg_dump`, restores it into a throwaway database and compares row counts — a genuinely good backup of the *database*. The signing key's private half is not in the database. `signing_keys.private_key_ref` is a **reference**; the key itself is a `0400` file in the secrets directory, which is not backed up by anything.

So the plan's own recovery procedure cannot be followed. Step 3 of `docs/PLAN/15` § Recovery says "verify signing key consistency before resuming traffic", and § Restore Testing asks drills to confirm "that signing keys restored alongside the DB are the matching set" — a check that cannot pass, or fail, or be run, because one side of the pair is never captured.

**Demonstrated, not theorised.** On 2026-09-11 the staging VM's `~/auth-state` was deleted. The database survived untouched and the nightly dump was hours old and perfectly good. The signing key was gone permanently: the only remaining copy was in the running process's memory, which is why the service went on answering `/healthz` and issuing tokens as though nothing had happened. Recovery required `keyctl generate` + `rotate` and retiring two orphaned key rows, and every token and session minted before that became unverifiable. On staging that cost nothing. In production it is every consumer application signed out at once, and an audit trail whose `events` rows reference key ids that no longer exist.

The failure mode is also **quiet in the worst way**: a running instance keeps working, so the loss is invisible until a restart — which may be days later, during an unrelated deploy, with no obvious connection to the deletion.

**Recommendation**, in order of value:

1. **Back up the secrets directory** alongside the database, encrypted, with its own retention — the plan already says "separately, with restricted access", so this is implementation, not a new decision.
2. **Make the drill real.** A restore drill that checks the restored `signing_keys` rows resolve to key files that exist and match — the check `docs/PLAN/15` already describes. It would have failed every night since the service was deployed.
3. **Alert on a key that cannot be resolved**, rather than discovering it at the next restart. The service resolves keys at startup; nothing checks between startups.

Until then, `deploy/vm/RUNBOOK-key-rotation.md` says plainly that a missing key file is unrecoverable and points at generate-rotate-retire.

---

### PG-27 — No SBOM is produced, so a dependency can appear without anyone seeing it

**Affects**: `docs/SECURITY/05`'s supply-chain row, which asks for "automated dependency scanning on every build, **SBOM diff review on every release**". The first half is done; the second does not exist.

Found by `P1-28`'s threat-model review, walking the nineteen categories rather than the seven in `docs/PLAN/11`.

`govulncheck` runs on every build and answers *"does anything we import have a known CVE"*. That is the more useful question most days, and it is not this one. An SBOM diff answers *"what changed"* — which catches a dependency **appearing**, including one pulled in transitively by a patch bump that no CVE has been filed against yet, and including one that a compromised maintainer published an hour ago. A vulnerability scanner cannot see a package nobody has reported yet; a diff sees every package.

The cost is low and the moment to pay it is a release, not a commit: `go version -m` on the built binary, or `syft` against the image, written to a file and compared with the previous tag's.

**Not a Phase 1 acceptance criterion** — `docs/PLAN/17` does not mention it, and `docs/SECURITY/05` puts SBOM review at "every release", which Phase 1 is not. It becomes due at the first tagged release that anybody other than this project consumes.

**Recommendation**: generate the SBOM in the `docker` CI job, attach it to the release, and fail a release build whose SBOM adds a module that the diff review has not seen. Phase 5 (`docs/PLAN/16` § Hardening) is the natural home; the work is small enough to land earlier.

---

### PG-28 — The container image is never scanned

**Affects**: `docs/SECURITY/05`'s container/runtime row, which asks for "automated container image scanning".

Also found by `P1-28`'s review. What CI does prove about the image is real and worth keeping: it is built from `gcr.io/distroless/static-debian12:nonroot`, it runs as a non-root user and a test asserts that, and on staging it runs `read_only` with `no-new-privileges` and `cap_drop: ALL`.

A `distroless/static` image has no shell, no package manager and no libc, so there is very little in it *to* find — which is a genuine mitigation and the reason this has not bitten. It is still not the check. The image contains our own binaries and whatever the base image ships, and "there is probably nothing there" is an argument, not a scan.

**Recommendation**: `trivy image` (or `grype`) in the `docker` job, failing on HIGH and CRITICAL with an explicit allowlist file for anything triaged and accepted — the same shape `gosec -severity medium` already uses, where the gate is stricter than the rule so findings are seen rather than accumulated. Pairs naturally with `PG-27`, since both want to run where the image is built.

---

### PG-26 — A new deployment cannot be bootstrapped without database access

**Affects**: `P1-25`'s quickstart, anyone standing up a fresh instance, and `docs/PLAN/17`'s claim that organizations and users are creatable through the REST API.

Found while executing the quickstart end to end. Every step works — once you already have an organization, a project, an administrator and an application. Getting the **first** of each does not work through any published surface:

- `POST /v1/organizations` requires `INSTANCE_OWNER`, and nothing creates the first instance owner.
- Registering the first application requires a management access token, which requires completing the Authorization Code flow, which requires an already-registered application.
- The console is itself an application that has to be registered before anyone can log into it.

Every environment so far — local and staging — was seeded with direct `INSERT` statements, which is why this has not blocked anything. It is not a gap in the API contract; the endpoints are correct. It is a missing **operator** path, and it is the first thing a stranger following the quickstart hits.

**Recommendation**: a `bootstrap` subcommand alongside `migrate` in the service image, run once against a new database, creating an organization, an instance owner with a password-set link, and the console's own application registration — printing what it made and nothing it should not. The pieces all exist; nothing here needs new authorization logic.

Until it exists, the quickstart says plainly that this step is an operator task and points here, rather than pretending a reader can start from nothing.

---

### PG-24 — A corrected migration does not reach databases that already ran it

**Affects**: every environment migrated before a fix, and every future correction to an applied migration.

`P1-20` found `events` and its four live partitions writable by `auth_app` on staging, while both migrations that revoke those privileges read correctly and a partition created today gets exactly `SELECT` and `INSERT`.

golang-migrate records a version as applied and never runs it again. So a migration edited after it has run — which is ordinary during a phase, and happened here between `P0-07` and `P0-15` — leaves already-migrated databases in the state the **old** text produced. The repository and the deployment then disagree, silently and indefinitely.

`20260910000019` repairs this instance and `check.sh` now gates it, but that is one property. The general problem is unaddressed:

- Nothing detects the divergence for any other privilege, constraint or default.
- Nothing prevents the next corrected migration from having the same effect.

**Recommendation**: a schema-assertion step that runs on every deploy and checks the properties that matter — the privileges on `events`, the RLS policies, the `CHECK` constraints named in `docs/SECURITY/02` — against what the repository says they should be, rather than against what the migration history claims. It is the difference between "the migrations ran" and "the schema is what we think it is", and only the second is a fact about the database. Related to `P5-*`'s hardening work; worth raising before then, because the failure mode is silent.

**Also worth deciding**: whether editing an applied migration should be refused outright by a CI check on the migration files' hashes, with corrections required to be new migrations. That is stricter and less pleasant during a phase, and it makes this class of divergence impossible rather than detectable.

---

### PG-25 — The owner role can rewrite the audit log

**Affects**: `docs/SECURITY/02` §19's append-only claim, and the deploy path.

The append-only guarantee is `REVOKE UPDATE, DELETE ON events FROM auth_app`. It is real and it is precisely scoped: the **owner** retains everything, necessarily, because it runs the migrations and partition maintenance.

So "the audit log is append-only" is true of the service and not of the database. Anybody holding `AUTH_MIGRATE_DSN` can rewrite history and leave no trace of having done so — and the deploy path uses that credential on every release.

This was demonstrated accidentally: a smoke test ran `UPDATE events SET event_type = 'rewritten'` as the owner "just to see", and 34 rows changed.

**Recommendation**: decide whether that is accepted. The options are all real:

- **Accept it**, and say so wherever the append-only property is claimed, so nobody reads a stronger guarantee than exists.
- **Ship the log off-box** — to a WORM store or an external SIEM — so the durable copy is outside the reach of any database credential. That is what `docs/PLAN/13`'s observability work would want anyway.
- **Separate the roles**: a migration role that owns the schema and a distinct owner for `events` that no deploy credential can act as. Possible, and it complicates partition maintenance.

Not urgent while the operator and the deployer are the same person, and it stops being true the moment they are not.

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

### PG-23 — The contract specifies prefixed identifiers and everything else uses UUIDs

**Affects**: `P1-16` onward, and every endpoint that returns a resource id.

`openapi/openapi.yaml`'s `ResourceId` specified a prefixed, sortable identifier — `usr_`, `org_`, `prj_` — with a good argument: an id pasted into a support ticket is self-describing, and passing a project id where a user id belongs is visible on sight rather than at the database.

Nothing else in the system agrees. `docs/PLAN/04` makes every primary key a UUID, the access token's `org_id` claim is a UUID, and OpenID Connect's `sub` — already shipped by `P1-08` — is a UUID that the contract itself tells integrators to store as a user's permanent key.

The two consistent positions are both expensive. Prefixing only the Management API gives one user two identifiers and makes every consumer convert between them. Prefixing everything means changing `sub`, which is a protocol field with its own conventions and a value integrators have already stored.

**Resolved in `P1-16`** toward UUIDs, and recorded in `ResourceId`'s own description rather than only here, because the next person to read that schema is the one who needs the reasoning. If prefixed identifiers are wanted later they arrive everywhere at once or not at all — a half-applied convention is worse than neither.

---

### PG-22 — Organizations have no soft-delete, and the card requires one

**Affects**: `P1-16`.

`docs/PLAN/04` models `organizations.status` as `active | suspended` and no deletion state at all. `P1-16` step 4 says "prefer soft-delete or suspension over hard delete", and its Definition of Done requires deletion to preserve audit history. There is no column for it.

A hard delete is not available either, and that is a good thing rather than the gap: every table referencing `organizations` is `ON DELETE RESTRICT`, so deleting a populated tenant fails at the database.

**Resolved in `P1-16`** by an additive `organizations.deleted_at timestamptz`. A separate column rather than a `status` value, because `status` is a reversible lifecycle and folding deletion into it raises "may a deleted organization be suspended?" — a question with no useful answer. A timestamp rather than a boolean for the reason `password_changed_at` is one: "when" answers questions "whether" cannot.

**`docs/PLAN/04` should be amended** to list the column, through the deliberate plan-change process (`AGENTS.md` rule 9).

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
