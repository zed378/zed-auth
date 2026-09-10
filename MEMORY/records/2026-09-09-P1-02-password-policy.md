# P1-02 — Password Policy and Breached-Password Rejection

| | |
|---|---|
| **Date** | 2026-09-09 |
| **Task** | `TASKS/PHASE-1-MVP-CORE-AUTH-SSO.md` § P1-02 |
| **Phase** | Phase 1 |
| **Surface** | backend |
| **Author** | Zed |
| **Commits / PR** | `feat/P1-02-password-policy` |
| **Status** | Completed |

---

## What Changed

Passwords are now checked two ways. `authn.Evaluate` is a pure function over (password, policy) applying `min_length` and `require_uppercase` read from `organizations.settings.password_policy`; `authn.CheckBreach` consults a breached-password corpus over a k-anonymity API that never receives more than five hex characters. Violations render into `docs/PLAN/05`'s error envelope through two functions — a detailed one for authenticated forms and an opaque one for everything else — so the disclosure rule is chosen by picking a function name rather than by passing a boolean.

`users` gained `password_changed_at`, which closes `PG-13`: `max_age_days` has been part of the specified policy since `docs/PLAN/08` and there was nothing in the schema it could be evaluated against.

The behaviour when the corpus is unreachable is `ADR-015`: **fail open, never silently**, backed by an audit event, a metric label and an alert.

## Why

`docs/PLAN/09` § Passwords & Credentials requires both halves, and they catch different failures. Composition rules bound the search space an attacker has to cover. The corpus catches the password that satisfies every rule and is still the first thing tried — `P4$$w0rd2026!` passes a twelve-character mixed-case digit-and-symbol policy and appears in the corpus hundreds of thousands of times. Neither substitutes for the other.

The part that had to be decided now rather than later is where the values live. A Go constant and a row in `organizations.settings` are both "the minimum length", and only one of them can be edited by an administrator. `P2-14` makes these editable per organization; if enforcement read a constant, that task would have to find and re-route every call site first. So the values come from the database from the first day enforcement exists, even though the only writer today is `P0-07`'s column default.

## How

`Evaluate` takes no clock, no I/O and no database, which is what makes exhaustive table-driven testing worth anything — and it is why the breach check, which is all three, composes on top rather than being passed in.

Length is counted in **runes over the NFC-normalized password**. Bytes would make "at least twelve characters" mean twelve in English and four in Japanese: a different policy per language, enforced by an implementation detail nobody chose. NFC rather than NFKC — compatibility folding is built for search and identifier matching and rewrites characters in ways that are surprising in a length rule. Where NFC changes the count it lowers it, so normalization can only make the rule stricter. Nothing on the hashing path is touched: `P1-01` stores what the user typed, and normalizing on the way in while verifying the raw input would reject correct passwords.

`MinLengthFloor` is 8 and cannot be configured below. That is a policy on policies, and it is the important one: without it `"min_length": 1` is a valid configuration, and the mechanism built to give administrators control becomes the mechanism by which one of them switches the control off — most likely by accident, and invisibly, since a weak policy produces no error anywhere. Every clamp returns an `Adjustment` and logs at WARN, because a silent correction leaves the difference between the configured and the enforced policy discoverable only by experiment.

`ParsePolicy` never fails. A malformed, partial or absent document yields the secure default per field. The settings column is administrator-editable, and the outcome of a typo there cannot be that passwords stop being checked. The JSON fields are pointers so that "absent" and "explicitly false" stay distinguishable — with a plain `bool` they are the same value and the default silently wins, which is a failure in the insecure direction.

**On SHA-1.** `internal/authn` now computes a SHA-1 digest, which is correct here and would be alarming anywhere else in the package. It is the corpus service's index, not a password hash: nothing it touches is stored, `users.password_hash` remains Argon2id and only Argon2id, and only the first five characters of the digest leave the process. The comment on `Client` says this, because a reviewer *should* stop on it.

The k-anonymity argument is what makes sending anything to a third party acceptable: five hex characters is one of 2^20 prefixes over a corpus of hundreds of millions, so the service cannot tell which hash was asked about. Compromising it, or observing the traffic to it, reveals nothing.

## Files and Components Touched

| Path | Change |
|---|---|
| `MEMORY/specs/P1-02-password-policy.md` | New — the specification `CLAUDE.md` requires for authentication work |
| `backend/internal/authn/policy.go` | New — `Policy`, `Evaluate`, `Expired`, `ParsePolicy`, clamping |
| `backend/internal/authn/breach.go` | New — k-anonymity client, `BreachChecker`, `CheckBreach`, `Outcome` |
| `backend/internal/authn/store.go` | New — reads policy from an organization row inside the caller's transaction |
| `backend/internal/authn/apierror.go` | New — the detailed and opaque renderings of `docs/PLAN/05`'s envelope |
| `backend/internal/authn/*_test.go` | New — 26 tests including the integration and non-vacuity ones |
| `backend/migrations/20260909000010_password_changed_at.*.sql` | New — `PG-13` |
| `backend/internal/config/config.go` | `PasswordConfig`: the breach-check switch, endpoint override and timeout |
| `backend/internal/observability/metrics.go` | `auth_password_policy_rejections_total`, `auth_password_breach_checks_total` |
| `backend/internal/audit/audit.go` | `user.password.rejected`, `user.password.breach_check_skipped` |
| `backend/cmd/authservice/main.go` | `passwordChecks`, built at startup and reported in the startup log |
| `deploy/observability/alerts.yml` | `PasswordBreachCheckFailingOpen`, `PasswordBreachCheckDisabled` (17 rules, promtool-validated) |
| `deploy/docker-compose.yml`, `deploy/vm/docker-compose.tunnel.yml`, `deploy/vm/env.example` | The new variables, with the reasoning |
| `MEMORY/DECISIONS.md` | ADR-015 |
| `TASKS/BACKLOG.md` | `PG-13` |

## Decisions Made

| Decision | Rationale | ADR |
|---|---|---|
| The breach check fails open, loudly | A third party must not be a hard dependency of password changes; the moment it matters most is an incident, which is also when it is most likely to rate-limit us | **ADR-015** |
| Policy values are read from the database from day one | `P2-14` makes them editable; enforcement reading a constant would have to be rewritten first | — |
| An 8-character floor that configuration cannot go below | Otherwise the control an administrator was given is the control they can silently remove | — |
| Length is runes over NFC, not bytes | A byte count is a different policy per language | — |
| Two rendering functions rather than one with a flag | The disclosure choice becomes visible in review instead of hidden in an argument | — |
| `password_changed_at` NULL means *not* expired | Unknown-as-infinitely-old makes deploying the migration a mass lockout | — |
| SHA-1 is used, and only as the corpus index | The alternative is not using the corpus at all; the containment is that nothing it touches is stored | — |

## Deviations from the Plan

None. One plan **gap** was found and registered rather than absorbed: `PG-13`, below.

## Tests Added

| Layer | What it covers |
|---|---|
| Unit | `Evaluate` across every rule and both sides of every boundary; runes vs bytes with a Japanese passphrase; NFC normalization; digits and symbols not satisfying the uppercase rule; Greek and Cyrillic capitals satisfying it; all violations at once; the byte bound; the floor applying to a hand-built `Policy` |
| Unit | `Expired` across NULL, zero, negative, one day either side of the limit, exactly at it, and future-dated |
| Unit | `ParsePolicy` across 13 documents: the migration default, stricter, partial, absent, empty, malformed, explicit `false`, below floor, above ceiling, negative age |
| Unit | The outbound request carries exactly five hex characters, and contains neither the password, its suffix, nor the full hash — in URL, headers or body |
| Unit | Five ways the corpus can fail, each producing `ErrBreachServiceUnavailable` rather than a clean answer; case-insensitive suffix matching; context cancellation; errors carrying no password material |
| Unit | The fail-open composition: a service error admits the password **and** reports `OutcomeSkipped`; a definitive match rejects; the four outcomes are distinct values |
| Unit | The error envelope against serialized JSON rather than the Go struct; `details` omitted when empty; the opaque rendering naming no rule, no value and no corpus |
| Integration | Policy read from a real `organizations` row as `auth_app`; an `UPDATE` changes enforcement with no restart, for two different fields |
| Integration | Four broken settings documents falling back to the default, each still refusing `"abc"` |
| Integration | Another tenant's policy is not readable, with a control proving the failure is isolation rather than a missing row |
| Integration | `password_changed_at` round-trips through the runtime role, and NULL is not expired |
| Manual | `-tags=manual` verification against the live corpus: `"password"` is breached, a random 35-character string is not |

**Three control tests** guard against vacuity: the outbound-leak assertion is proven able to detect a leak *and* not to fire on a correct request; the two error renderings are asserted to differ; the accepting outcomes are asserted distinct. Each exists because the corresponding check would otherwise pass while covering nothing.

## Abuse Cases Covered

| Abuse case | Source | Test |
|---|---|---|
| A breached password that satisfies every composition rule | `docs/PLAN/09` § Passwords | `TestBreachedRecognisesAMatch`, `TestCheckBreachRejectsAKnownBreachedPassword` |
| Policy detail learned from an unauthenticated endpoint | `docs/SECURITY/02` §12 | `TestOpaqueValidationErrorDisclosesNothing` |
| A network observer inferring a password from our traffic | `docs/SECURITY/02` § Information Disclosure | `TestBreachRequestCarriesOnlyAPrefix` |
| A compromised corpus service reading our users' passwords | `docs/SECURITY/00` trust boundary | Same — it never receives enough to identify one |
| Forcing the corpus to fail to deny password changes | — | `TestCheckBreachFailsOpenAndSaysSo` |
| Forcing the corpus to fail to admit a breached password | — | Accepted and bounded by ADR-015; `TestBreachServiceFailuresAreDistinguishable` keeps the skip visible |
| An administrator configuring the control away | `docs/PLAN/08` Part B | `TestParsePolicy` (below-floor), `TestABrokenPolicyDocumentFallsBackToTheDefault` |
| An oversized password exhausting memory through Argon2 | `P1-01` | `TestEvaluateBoundsTheInput` |
| The password reaching a log or an error message | `CLAUDE.md`, `docs/PLAN/13` | `TestBreachErrorsCarryNoPasswordMaterial`, plus the existing log-redaction gate |
| Reading another tenant's policy | `docs/PLAN/08` Part B, `docs/SECURITY/02` §2 | `TestPolicyIsNotReadableAcrossTenants` |

## Definition of Done Verification

- [x] Tests at the appropriate pyramid layer
- [x] Every abuse case has an automated test
- [x] No API surface changed; the error envelope was already in the spec
- [x] Sensitive actions write audit events — two new event types, neither carrying the password
- [x] Nothing sensitive is logged
- [x] `scripts/check.sh` green (35/35, plus the two `CHECK_FULL` gates)
- [x] `TASKS/PROGRESS.md` and the phase file updated

Task-specific DoD:

- [x] **Policy evaluation is a pure function with table-driven unit tests, including boundary lengths.** Both sides of every boundary, not only the failing side — a test that checks only that 11 characters is rejected passes against an implementation that rejects everything.
- [x] **The breached-password check transmits only a hash prefix, verified by an outbound-request test.** And the test is itself verified against a synthetic leaky request, so its containment check is known to be able to fail.
- [x] **The fail-open/fail-closed decision is recorded in `MEMORY/DECISIONS.md`.** ADR-015.
- [x] **Validation errors match `docs/PLAN/05`'s error schema exactly.** Asserted on the serialized JSON, not the Go struct — a struct assertion passes against wrong `json` tags.
- [x] **Changing `organizations.settings.password_policy` changes enforcement with no code change.** The integration test performs an `UPDATE` against a live row and re-reads through the same process, for two different fields.

## What Did Not Work

**The corpus response check counted lines, and an HTML error page is one line.** A captive portal or proxy answering `200` with "Access denied" produced zero suffix matches, one line, and therefore `clean` — the password was admitted with `OutcomeClean` and nothing anywhere recorded that no check had happened. That is worse than failing closed and worse than failing open: it is an always-open path that the metric justifying ADR-015 would have shown as healthy.

Caught by a test in the table of ways the service can fail, written before the parser was finished. The fix counts **well-formed entries** rather than lines: an entry is a 35-character hex suffix, and a body with none of them has not answered the question. Every prefix in this corpus has hundreds of suffixes, so zero is never a real answer.

This is the same shape as every vacuous check collected in these records, arriving from a new direction — not a test that did not run, but a *parser* whose success condition was satisfied by a failure.

**Test fixtures with hand-counted hash suffixes.** Once the parser required 35 characters, several fixtures written as strings of zeros were the wrong length and three unrelated tests failed for a reason that had nothing to do with what they asserted. The helper now pads and validates its input, so a fixture mistake is a panic naming itself rather than a confusing assertion failure.

**`AUTH_PASSWORD_BREACH_CHECK_ENABLED` reads as a control that does nothing yet.** No path sets a password until `P1-12` and `P1-19`, so the variable is currently observable only in the startup log. That was worth naming rather than hiding, because it is the same failure mode as `PG-13` — a value someone can set that the system does not act on. The startup log says explicitly that no password-set path exists yet, so an operator reading it is not misled into thinking the check is running.

**Two blank assignments in `main.go`.** The first wiring left `_ = breachChecker` and `_ = policyStore`, which is how partial wiring usually gets hidden. Replaced with a `passwordChecks` value that the startup log reports, so the state is visible rather than merely compiling.

**Deploying this found a dead backup.** The migration is additive and ran without incident, but the pre-deploy backup step did not: `P0-14` moved runtime state out of the code checkout on 2026-09-08 and the systemd unit's `ReadWritePaths` still named `/home/infra/auth/backups`. systemd refuses to start a unit whose `ReadWritePaths` does not exist, so the service exited `226/NAMESPACE` before `backup.sh` ran a line — every night since.

The quiet part is the lesson. `systemctl list-timers` reported the timer healthy throughout, because the *timer* was healthy: it fired on schedule every night and the service it triggered died instantly. Nothing distinguished that from a working backup except a `systemctl status` nobody had reason to run.

I also made it worse before making it better, twice. I ran the migration after the pre-deploy backup failed, when the correct action was to stop — the migration was additive so nothing was at risk, but the process exists precisely so that judgement is not required in the moment. Then I copied the repo's unit file over the installed one before pushing the fix, overwriting a hand-edited correction with the stale version. Both are recorded because the second is the more instructive: the repo and the running system had drifted, and I reached for the repo as the source of truth without checking which one was ahead.

Fixed, reinstalled, and a verified backup taken (90KB, 19 tables, row counts matching). `BL-01` is open for the freshness alert that would have caught it on day one.

## Follow-Ups and Open Questions

- `P1-12` and `P1-19` must call `Evaluate` and `CheckBreach` on every password-set path, record the returned `Outcome`, write `user.password.rejected` / `user.password.breach_check_skipped`, and set `password_changed_at`. Until then this is a library with no caller.
- `P1-11` must enforce `Expired` at login. The predicate and the column are ready.
- `docs/PLAN/04-DATA-MODEL.md` § `users` should be amended to list `password_changed_at`, through the deliberate plan-change process (`AGENTS.md` rule 9). Raised in `PG-13` rather than made here.
- `require_uppercase` cannot be satisfied in a caseless script. The remedy available today is that the rule is per organization. If the product targets such a market, `docs/PLAN/08` Part B's policy shape is worth revisiting.
- `BL-01`: nothing alerts on a backup that stops happening. The unit paths are fixed, but the health of the backup is currently only as good as somebody remembering to look.
- Re-checking passwords admitted during a fail-open window is possible from the audit events but not implemented. ADR-015 § Alternatives explains why the queue-for-recheck design was deferred.

## What to Watch

**`auth_password_breach_checks_total{outcome="skipped"}` climbing.** This is the number ADR-015 rests on. A sustained non-trivial rate means passwords are being accepted unchecked, and `PasswordBreachCheckFailingOpen` fires at a tenth over fifteen minutes. If that alert is ever silenced or the metric stops being scraped, the decision quietly becomes "no breach checking" and nothing in the code will complain.

**Outbound egress to the corpus.** The staging VM reaches it in 273ms today. A firewall change, an egress proxy, or the service adding authentication all present as a rising skip rate rather than an error anybody sees — which is the intended behaviour and also why the alert matters more than usual.

**A policy clamped without anyone noticing.** The WARN log fires once per policy read, so a below-floor configuration is loud. If those logs are filtered out, an administrator can believe a policy is in force that is not — and the only symptom is passwords being accepted that they expected to be refused.

**`password_changed_at` staying NULL.** The column exists and nothing writes it yet. If `P1-12` and `P1-19` land without setting it, `max_age_days` remains as unenforceable as `PG-13` found it, and the migration will have bought nothing.
