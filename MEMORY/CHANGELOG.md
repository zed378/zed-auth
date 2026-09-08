# Changelog

Chronological summary of changes at a coarser grain than the individual records in [`records/`](./records/). If you want to know what happened and roughly when, read this. If you want to know why it was done that way, follow the link to the record.

This is the **internal** changelog. The public-facing `/changelog` on the marketing site (`PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`) is a separate, user-facing artifact that never describes an unshipped capability.

Format follows Keep a Changelog conventions, grouped by release once releases exist. Before the first release, entries are grouped by date.

---

## Unreleased

### 2026-09-08

**Added**
- `TASKS/` — the execution layer: task conventions and global definition of done, seven phase files covering 124 tasks, a progress board, and a backlog. Every task cites the plan documents it implements and names its abuse cases. ([P0-21](./records/2026-09-08-P0-21-tasks-and-memory-scaffolding.md), [ADR-001](./DECISIONS.md#adr-001--establish-tasks-and-memory-as-the-execution-layer))
- `MEMORY/` — the record layer: change records, decision log, this changelog, and templates. A task is not done until its record exists. ([P0-21](./records/2026-09-08-P0-21-tasks-and-memory-scaffolding.md))

**Found**
- Two contradictions between existing plan documents, recorded as `PG-08` and `PG-09` in `TASKS/BACKLOG.md`. Neither plan document has been edited — both are flagged for the deliberate plan-change process (`AGENTS.md` rule 9).
- Eleven plan gaps: capabilities the plan requires functionally but does not model, most of them missing tables in `PLAN/04-DATA-MODEL.md` (signing keys, MFA factors, invite and reset tokens, federated identity links, webhook endpoints, role permission keys). Each is recorded in `TASKS/BACKLOG.md` against the task it blocks.
- Eight open questions requiring a decision from the project owner — deployment target, email provider, RPO/RTO values, capacity assumptions, and others. Recorded in `TASKS/BACKLOG.md`.

**Changed** — plan amendments, made deliberately at the user's instruction under `AGENTS.md` rule 9
- `PLAN/04-DATA-MODEL.md` (158 → 295 lines): added `signing_keys`, `user_mfa_factors`, `user_recovery_codes`, `user_tokens`, `user_identities`, `webhook_endpoints`, `webhook_deliveries`; extended `roles` (permission keys), `sessions` (org scoping, revocation, and the Redis-versus-PostgreSQL authority note), `refresh_tokens` (rotation families); added retention/partitioning policy and a "what is deliberately not stored" table. ([record](./records/2026-09-08-plan-gap-remediation.md), [ADR-002](./DECISIONS.md), [ADR-003](./DECISIONS.md), [ADR-004](./DECISIONS.md))
- `PLAN/05-API-CONTRACT.md`: SAML 2.0 corrected from Phase 2 to Phase 4, matching `PLAN/03`, `PLAN/16`, and `PLAN/17`.
- `PLAN/18-RISK-REGISTER.md`: R-04's mitigation corrected to state that Project Grant subset validation happens **on every request**, not only at grant creation — matching `CLAUDE.md`, `AGENTS.md` rule 3, `PLAN/08` Part C, and `PLAN/19`. The weaker wording described a system where a narrowed or revoked grant would keep working.
- `PLAN/07-BACKEND-ARCHITECTURE.md`: Redis clarified as a cache in front of PostgreSQL for sessions, not a second source of truth.
- `UI-UX/08-PAGE-SPECIFICATIONS.md`: four screens added that the IA included but the "full inventory" omitted — Organization Overview, Organization Settings, Instance-wide policies, Instance audit log.

**Added** — frontend track
- `TASKS/PHASE-F-FRONTEND-IMPLEMENTATION.md`: 53 tasks across seven tracks covering every component in `UI-UX/07`, all 21 console screens, the hosted authentication screens, the public site, and the frontend quality suite. Foundation tasks are phase-independent; every page task carries a binding gate naming the backend task that unblocks it, which enforces `PLAN/16`'s lockstep rule per screen rather than per phase. ([record](./records/2026-09-08-phase-f-frontend-track.md), [ADR-005](./DECISIONS.md))
- Project total: 124 → 177 tasks.

**Added** — implementation begins ([record](./records/2026-09-08-P0-phase-0-foundation-first-eleven.md))
- Go service skeleton with fail-fast configuration, a documented middleware chain, and graceful shutdown that provably completes in-flight requests. Health endpoints separate liveness from readiness and disclose no infrastructure detail. (`P0-04`, `P0-10`)
- Structured JSON logging with redaction enforced by the logger across 23 sensitive key names, plus per-request correlation IDs. (`P0-09`)
- Local Docker stack — PostgreSQL, Redis, Mailpit — with a distroless non-root runtime image whose healthcheck is the binary itself. The Postgres init script creates the application role as `NOSUPERUSER NOBYPASSRLS` and non-owner, which is the precondition for row-level security. (`P0-05`)
- Migration tooling as a separate binary with embedded SQL, and the full 14-table Phase 0/1 schema including month-partitioned, append-only `events`. (`P0-06`, `P0-07`)
- CI pipeline: commit-convention enforcement, build and race-enabled tests, integration tests against real Postgres and Redis, migration round-trip verification, destructive-migration justification, gosec, govulncheck, secret scanning, and a non-root image assertion. Third-party actions pinned by SHA. (`P0-13`)
- ADR-006 through ADR-010 record the backend stack, embedded migrations, the distroless runtime, text-plus-CHECK over native enums, and why `events` has no foreign keys. (`P0-01`)

**Added** — cross-tenant isolation ([record](./records/2026-09-08-P0-08-row-level-security.md))
- Row-level security on 11 tables, keyed to a transaction-scoped `app.current_org_id`. A query with no tenant context returns zero rows rather than every row — fail-closed as a consequence of how SQL evaluates NULL, not as a check someone has to remember. (`P0-08`)
- A storage API with no exported way to query outside a declared scope: every path goes through `WithTenant` or `WithInstanceScope`, both of which set the tenant inside a transaction before returning a queryable handle. `SET LOCAL` rather than `SET`, so a pooled connection cannot carry one request's tenant into the next. (`P0-08`)
- The service refuses to boot if its database role is a superuser, has `BYPASSRLS`, or owns a table — closing the hole flagged in the previous record, where pointing `AUTH_POSTGRES_DSN` at the owner would silently disable every policy with no test failure. (`P0-08`)
- A CI gate failing the build when a table with an `org_id` has no RLS enabled. (`P0-08`)
- `/readyz` now genuinely checks PostgreSQL; it previously reported ready with no dependencies wired.
- Deployed and verified on the VM; `https://auth.zedth.my.id` healthy throughout.

**Added** — the public site ([record](./records/2026-09-08-P0-18-public-site-skeleton.md))
- The full route structure from `PLAN/20`: landing, about, contact, docs (quickstart, concepts, guides, console, API reference) and a changelog. 34 pages. `/pricing` and `/security` are deliberately absent — the first because no commercial tier exists, the second because `PLAN/20` places the trust page alongside Phase 5. (`P0-18`)
- **The API reference is generated** from `openapi/openapi.yaml`, the same file that generates the backend's server interface and the console's client — three consumers, one source, none able to disagree. This closes `P0-16`, whose last open step was exactly this. (`P0-18`)
- Docs versioning live from the first commit, with a `1.0` snapshot labelled *placeholder*: it proves the pipeline before there is a release to version. Retrofitting versioning once v1 docs exist means reorganising every file at the moment there is most content to break. (`P0-18`)
- Local search over 123 documents. `UI-UX/20` wants fuzzy matching because "a developer often doesn't know the exact terminology this project uses yet".
- Three scripts turn `PLAN/20`'s separation rules into checks: the nine shared colour values still match the console's, nothing here imports from `console/`, and every token meets AA in both themes. All three erode by convenience rather than by decision, so none is left to memory.
- `deploy/public-site/` and a CI job with its own install and cache — `PLAN/20`: "a docs typo fix shouldn't require a backend deploy pipeline".

**Decided**
- [ADR-014](./DECISIONS.md): one Docusaurus project rather than a marketing SSG plus a separate docs framework. `PLAN/20` implies two; `UI-UX/20` § Cross-Page Requirements requires a shared header and footer so Landing → Docs "never feels like a different product", and two projects make that a duplicated component. The deciding argument was that `PLAN/20` requires docs versioning and the obvious marketing-side alternative has none.

**Fixed**
- **A dark mode that would have shipped unreadable.** Carrying the console's palette to a dark surface measures accent 2.4:1, danger 2.7:1, warning 3.3:1, success 2.8:1 — all below AA — and the border 1.8:1, below the 3:1 for a component boundary. It would have looked deliberate. Each token was re-tuned: same meaning, different value for a different surface. None of it was visible by looking; it came from computing the ratios. (`P0-18`)
- **A soft 404.** The nginx config served the 404 page with a `200` status, telling every crawler that missing URLs exist — on the one surface whose job is discovery. `=404` with `error_page` returns the real status and still renders the styled page. (`P0-18`)
- **A security check that cried wolf.** `scripts/check.sh` reported "a PEM private key block is committed" for three files inside `public-site/node_modules` — gitignored, committed by no definition. It walked the working tree while its message said "committed"; it now scans `git ls-files` like the credential check beside it always did. A check that fires the first time someone installs dependencies is a check that gets commented out.

**Added** — the console shell ([record](./records/2026-09-08-P0-17-console-skeleton.md))
- React 19 + TypeScript on Vite 8 and Tailwind 4: every design token `UI-UX/05` names, routing, the navigation tree from `PLAN/06`, an error boundary, TanStack Query, and the typed API client generated from `openapi/openapi.yaml` — which closes `P0-16` step 3. (`P0-17`)
- Three local ESLint rules make the token discipline a build failure rather than a convention: a raw hex, a Tailwind arbitrary value, or an inline style all fail lint. The first run caught a real bug — `w-[--spacing-nav]` referenced a token that did not exist, so the navigation would have had no width. (`P0-17`)
- `color-danger` is un-overridable structurally: the brandable set is a union of literal token names, backed by a runtime filter because branding arrives as JSON where the type system has ended. A custom accent is contrast-checked against its surface before it is applied, which `UI-UX/13` requires at the moment an org admin sets it. (`P0-17`)
- Accessibility in the shell rather than on a list: landmarks, a skip link whose target is genuinely focusable, 44px targets at compact density, a focus ring using `color-accent`, and a `prefers-reduced-motion` fallback. Verified with real `Tab` presses in a browser, plus an axe pass on the WCAG 2.1 A/AA rule set. (`P0-17`)
- `deploy/console/` — nginx config and compose file for the staging preview at `console.zedth.my.id`, bound to `127.0.0.1:10920` so only `cloudflared` reaches it.
- Console lint, typecheck, tests and generated-client freshness are gates in `scripts/check.sh` and CI. 25 gates became 29.

**Fixed**
- **Every piece of text rendered at the browser default size, and nothing failed.** Tailwind v4 reads font sizes from `--text-*`; `--font-size-*` is not a namespace it knows, so the five type-scale tokens sat in the stylesheet as inert custom properties and generated no CSS. The same for `--line-height-*`, `--easing-*`, and `--duration-*` (not a namespace at all). Lint, typecheck, 72 tests and the build were all green — **because the tests read the source file**, which contained exactly what it should. A test that reads the input to a compiler cannot tell you what the compiler did. `utilities.test.ts` now compiles Tailwind and asserts the classes the components use produce rules; verified by reintroducing the bug, which the new test catches and the old one passes 48/48 through. (`P0-17`)
- Two `<h1>` elements: the narrow-width message overlaid the layout instead of replacing it, so below tablet width a screen-reader user would have walked past "this screen is too narrow" into the application it says is unusable. A visual overlay hides nothing from assistive technology. (`P0-17`)
- Refreshing on `/projects` returned 404 — the history-mode failure, where a static host looks up the route as a file. `deploy/console/nginx.conf` adds the fallback, never caches `index.html` (it is the file that names the current bundle), and sets the console's own security headers. (`P0-17`)
- Those headers then silently vanished: in nginx `add_header` does not accumulate across contexts, so a `location` block with any header of its own discards every server-level one. Found with `curl -I` against the deployed site; nothing in the config looked wrong. (`P0-17`)

**Added** — the API contract ([record](./records/2026-09-08-P0-16-api-contract.md))
- `openapi/openapi.yaml` is the single contract artifact, and it **generates the code rather than describing it** ([ADR-013](./DECISIONS.md)). Handlers implement a generated interface, so a signature that stops matching the contract fails to compile. `PLAN/05` accepts a spec that CI merely validates; that is now the backstop, not the mechanism. (`P0-16`)
- The shared component schemas — error envelope, pagination token, common parameters — are deliberately ahead of the endpoints. They are contract infrastructure, and an error format that changes after twenty endpoints exist is a breaking change to all twenty. (`P0-16`)
- `scripts/openapi-shipped-paths.py`: the spec documents only endpoints that exist. `/docs/api-reference` renders from it, so a documented endpoint is a public claim it exists (`UI-UX/21` governance, `CLAUDE.md`). Adding one means adding it to `SHIPPED` in the same commit. (`P0-16`)
- Three new gates in CI and `scripts/check.sh` — spec validity, generated-code freshness, and the shipped-endpoint rule. 22 gates became 25. Each was verified by deliberately breaking it.
- **ADR-012**, the audit write-semantics decision, written at last. Four code comments and a change record referenced it and it had never been written into `DECISIONS.md`, which `P0-12`'s Definition of Done required. A reference to a decision that does not exist reads as though the reasoning was recorded somewhere.

**Fixed**
- Writing the spec against the running service found that `/healthz` returns `{"status":"ok"}` while `/readyz` returns `{"status":"ready"}`. Recorded as two schemas rather than tidied into one: a consumer already parsing `ready` would break if the server were changed to match a prettier document. A test now pins both values and says what changing them would cost. (`P0-16`)
- `scripts/check.sh` defined a shell function named `head`, which shadowed the `head` command for the whole script. Every `... | head -20` in a pipeline called the function, which ignores stdin and **discards the piped output**. Two failure paths — unit-test failures and `govulncheck` findings — had been throwing their diagnostic detail away since they were written. Invisible until something failed, which is where a silent bug survives longest. Renamed to `section`.

**Added** — metrics endpoint authentication ([record](./records/2026-09-08-P0-11-metrics-endpoint-authentication.md))
- A bearer token on the metrics endpoint, resolved through the same secret indirection as everything else, compared in constant time, and rejecting with a bare `404` rather than a `401` — a `401` with a challenge header confirms to a prober that the endpoint exists and says what it wants. (`P0-11`)
- The service now refuses to boot when the metrics listener binds beyond loopback with no token configured. It caught two misconfigurations within the hour, both of them ours. (`P0-11`)
- `deploy/vm/secrets.sh` — creates and repairs the secrets directory. It exists because `chmod 400` is necessary and not sufficient, and the gap between those two costs a restart loop to find. (`P0-11`)
- The metrics port is **not published to the host**. `metrics.zedth.my.id` was live and token-gated for about an hour; the hostname and the published port are both gone. The endpoint stays reachable inside the compose network, which is where the only scraper that will ever exist here would run. A port published for a scraper that does not exist is a port open for no one. `docker-compose.metrics-port.yml` is the opt-in override, loopback-only, with a header explaining what publishing it means.

**Fixed**
- Secrets are resolved once, immediately after configuration loads, before any pool is opened or goroutine started. Previously a bad secret reference produced `sql: database is closed` on repeat — the deferred pool close racing the already-running audit goroutine — which is a consequence three steps removed from the cause, pointing at the wrong subsystem. Startup failures should be ordered so the first thing to fail is the thing that is wrong. (`P0-11`)
- The secrets directory and its contents now belong to the service's uid (`65532`), not the operator's. The runtime image is distroless `:nonroot`, so a directory owned by the operator at mode `700` cannot be traversed by the service at all and the mode on the file inside is never reached. The signing key had the same wrong ownership and would have failed identically in `P1`. (`P0-11`)
- `scripts/check.sh` supplies a synthetic `AUTH_ADMIN_TOKEN_REF` when validating the tunnel compose, since `${VAR:?}` fails static validation as well as deployment.

**Added** — audit log and toolchain patches ([record](./records/2026-09-08-P0-12-audit-writer.md))
- The audit writer. Events commit inside the transaction of the action that caused them, so a permission change and its record succeed or fail together. Redaction happens in the writer rather than at call sites, because the table is append-only and a credential written there cannot be deleted by anyone. (`P0-12`)
- Partition maintenance at startup and daily, keeping three months of runway. Without it every `INSERT` into `events` — and therefore every security-sensitive action — would have failed at a month boundary, with no deploy to correlate against. This was flagged as a time bomb two records ago. (`P0-12`)
- Partition creation via `SECURITY DEFINER` with a pinned `search_path`, rather than granting the runtime role `CREATE` on the schema. (`P0-12`)
- Instance-level events: `events.org_id` is now nullable, so the cross-tenant database path can be audited as `PLAN/08` Part B requires. (`P0-12`)
- `scripts/check.sh` — 22 gates, the same ones CI runs, before a push rather than after.

**Fixed**
- Six Go standard-library vulnerabilities at 1.26.5, one of them directly relevant: `ReadHeaderTimeout` was not applied during the unencrypted HTTP/2 check, and that timeout exists to bound slow-header attacks. Toolchain pinned to 1.26.6; `golang.org/x/text` upgraded past an infinite-loop bug. (`P0-12`)

**Added** — observability ([record](./records/2026-09-08-P0-11-metrics-and-tracing.md))
- Sixteen Prometheus instruments on a listener separate from the public one, so "not reachable from the public ingress" is a property of the socket rather than an ingress rule someone has to remember. The endpoint is unauthenticated and discloses request rates, error rates and login outcomes, so an all-interfaces bind is refused in production and compose publishes it to loopback. (`P0-11`)
- Histogram buckets sit exactly on `PLAN/12`'s latency targets, so a quantile query can answer "did we meet it" without interpolating across a wide bucket. Tested. (`P0-11`)
- Fifteen alert rules, promtool-validated, each carrying the reason it exists. Rules that must catch "stopped happening" use `absent()` rather than `== 0`, because a counter with no observations produces no time series and a zero comparison never fires when the thing is down. (`P0-11`)
- OpenTelemetry tracing with W3C propagation, off unless an OTLP endpoint is configured. Handler and query spans arrive with the endpoints in Phase 1. (`P0-11`)
- Both `P0-12` follow-ups closed: the unsupervised partition-maintenance goroutine is now visible as a gauge with two alerts, and cross-tenant database access is counted as `PLAN/08` Part B asks.

**Status**
- Phase 0: 19 of 21 done. `P0-16` is complete — its last open step, the public site's generated API reference, landed with `P0-18` — step 3 (the console's typed client) landed with `P0-17`; step 4, the public site's API reference, waits on `P0-18`.
- Open deviations: DV-01 (single-VM production vs. Multi-AZ), DV-02 (`manager_roles` has no tenant policy until `P2-05` provides a user context).
- Remaining in Phase 0: the test harness (`P0-15`), landing and docs content (`P0-19`), and staging (`P0-20`).
- New plan gap `PG-12`: `color-border` serves both input borders (WCAG 1.4.11 wants 3:1) and table dividers (which want a hairline). One token cannot do both well.
- The OIDC provider library remains undecided by design: confirming JWKS rotation with overlap and refresh-token reuse detection requires building against it, so it moves to `P1-03`.
- Open questions now number nine; `OQ-09` (audit log retention period and the erasure approach) is new and should be confirmed before `P0-07` writes the partitioning migration.
