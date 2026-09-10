# P1-02 — Password Policy and Breached-Password Rejection

Feature specification, per `docs/PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`. `CLAUDE.md` requires one for anything touching authentication.

---

## 1. Business Objective

Reject passwords that are weak by rule and passwords that are weak by evidence, and read the rules from organization settings rather than from the binary — so that Phase 2's per-organization policy editor plugs into a mechanism that already exists instead of replacing a hard-coded one.

The second half is the part that is easy to get wrong now and expensive to fix later. A constant in Go and a value in `organizations.settings` are both "the minimum length"; only one of them can be changed by an administrator, and once enforcement reads the constant, every later attempt to make policy configurable has to find and re-route every call site.

The two halves address different failure modes and neither substitutes for the other. Composition rules bound the search space an attacker must cover. The breach corpus catches the password that satisfies every rule and is nonetheless the first thing tried, because forty million other people also chose it. `P4$$w0rd2026!` passes a twelve-character, mixed-case, digit-and-symbol policy and appears in the corpus hundreds of thousands of times.

## 2. Actors

| Actor | Interaction |
|---|---|
| End user | Chooses a password at signup, invite acceptance, reset, or change |
| Organization administrator | Edits `password_policy` in organization settings (the editing UI is `P2-14`; the enforcement it drives is this task) |
| The service | Evaluates a candidate password against the org's policy and against the breach corpus |
| The breach corpus service | Answers a hash-prefix query. Third party, outside our trust boundary, and unavailable sometimes |
| An unauthenticated attacker | Probes the password endpoints to learn policy detail, tenant existence, or whether an address has an account |
| A network observer between us and the corpus service | Sees every query we make |

## 3. Functional Requirements

- **FR-1** Evaluate a candidate password against `min_length`, `require_uppercase` and `max_age_days` read from `organizations.settings.password_policy` (`docs/PLAN/08` Part B).
- **FR-2** Evaluation is a pure function of (password, policy) — no I/O, no clock, no database — so it is exhaustively testable and cannot behave differently under load.
- **FR-3** Policy values come from the organization row. No rule value is a compile-time constant. Changing the row changes enforcement with no deploy (`P1-02` DoD item 5).
- **FR-4** A policy that is absent, partial or malformed falls back to a documented **secure default**, and the fallback is visible rather than silent.
- **FR-5** Check the candidate against a breached-password corpus using k-anonymity: send a hash prefix, never the password and never the full hash (`docs/PLAN/09` § Passwords).
- **FR-6** Behaviour when the corpus service is unreachable is a single documented decision, recorded as an ADR (`P1-02` step 4). See §12.
- **FR-7** Return every violation at once, not the first one — a form that reveals one problem per submit is a form the user fights.
- **FR-8** Violations are returned as structured values. The *caller* decides how much of the structure reaches the response, because the answer differs between an authenticated change form and an unauthenticated login (`P1-02` step 6).
- **FR-9** Serialize violations into `docs/PLAN/05`'s error envelope with per-field `details[]`, so the console maps them to fields (`docs/UI-UX/15` § Error Presentation).
- **FR-10** Expose password age as a pure predicate over (`password_changed_at`, policy, now), so `P1-11`/`P1-12` can enforce expiry at login without re-deriving the rule.

## 4. Non-Functional Requirements

- **NFR-1** Policy evaluation adds no measurable latency — it is string inspection over a bounded input.
- **NFR-2** The breach check is bounded by an explicit timeout. Measured from the VM: 273ms and 77KB for one prefix against the live API. A password change may wait; a login may not, which is why expiry checking at login reads a timestamp and never calls out.
- **NFR-3** The corpus client never blocks shutdown: it honours request context cancellation.
- **NFR-4** No password, and no full password hash, is ever written to a log, a metric label, an error message, or an outbound request (`CLAUDE.md`, `docs/PLAN/13`).
- **NFR-5** The corpus response is treated as untrusted input — it is a third-party document, parsed defensively, and a malformed one is an outage, never a pass.

## 5. Dependencies

| Depends on | Why |
|---|---|
| `P1-01` (Argon2id) | Password setting is the operation this validates; the two run in sequence at every set-password call |
| `P0-07` (schema) | `organizations.settings` exists with the policy shape and defaults already in place |
| `P0-08` (RLS) | Reading an organization row goes through the tenant-scoped storage API |
| `P0-12` (audit writer) | A skipped breach check and a policy rejection are both audit events |
| `P0-11` (metrics) | The fail-open decision in §12 is only defensible if the skipped checks are counted |

Depended on by: `P1-11`/`P1-12` (login and hosted login page — expiry enforcement), `P1-19` (management API, user creation), `P2-10`/`P2-14` (per-org policy enforcement and its editor), `PF-*` (the password field's inline validation).

## 6. Database Changes

**One additive migration.** `users` gains:

```sql
ALTER TABLE users ADD COLUMN password_changed_at timestamptz;
```

Nullable, no default, no backfill. Backward-compatible per `docs/PLAN/14`'s expand/contract rule: a previous-version instance running against this schema neither reads nor writes the column.

**This closes a plan gap, flagged rather than absorbed silently** (`CLAUDE.md`). `docs/PLAN/08` Part B specifies `max_age_days` in the policy shape, and `docs/PLAN/04` § `users` has no column recording when a password was last set — so as specified, `max_age_days` is unenforceable. It is registered as `PG-13` in `TASKS/BACKLOG.md` and `docs/PLAN/04` should be amended through the deliberate plan-change process (`AGENTS.md` rule 9).

The column is added now rather than with the login work that enforces it, because it accumulates data. Adding it in `P1-11` would mean every password set between now and then has no timestamp, and a NULL is indistinguishable from "set before we started recording".

**NULL means "unknown", and unknown is not expired.** A user whose password predates this column is not locked out by a policy change; expiry begins applying at their next password change. Treating NULL as infinitely old would turn deploying this migration into a mass lockout, which is a self-inflicted outage disguised as a security improvement.

## 7. API Contract

No new endpoints. This task supplies validation that `P1-19` (`POST .../users`), the reset and change flows, and `P1-12`'s hosted form all call.

The contract addition is the **error shape**, which already exists in `openapi/openapi.yaml` (`Error` + `ErrorDetail`) and needs no schema change:

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "The password does not meet this organization's policy",
    "details": [
      { "field": "password", "issue": "must be at least 12 characters" },
      { "field": "password", "issue": "must contain an uppercase letter" }
    ]
  }
}
```

Multiple `details[]` entries share `field: "password"` — that is deliberate and matches `docs/UI-UX/15`, which maps `details[].field` to a field and renders the issues beneath it.

## 8. Frontend Changes

None in this task. It supplies the server half that `PF-*`'s password field renders: on-blur client-side hinting for the composition rules, and the server's `details[]` shown inline on submit. The client rules are a convenience and never the enforcement (`CLAUDE.md`: authorization and validation are server-side, always) — the console cannot check the breach corpus, and a client-side length check is a hint the user can delete from the DOM.

## 9. Backend Changes

New package `internal/authn/policy` — or `policy.go` within `internal/authn`, decided at implementation time by whether the breach client belongs beside the pure evaluator. Provisional shape:

```go
type Policy struct {
    MinLength        int
    RequireUppercase bool
    MaxAgeDays       int
}

type Violation struct {
    Rule    string // stable machine key: "min_length", "require_uppercase", "breached"
    Message string // human text, safe for a UI, no policy value beyond what the rule states
}

func Evaluate(password string, p Policy) []Violation      // pure
func Expired(changedAt *time.Time, p Policy, now time.Time) bool  // pure; nil is not expired
func Parse(settings []byte) (Policy, []string)            // policy + the defaults it had to substitute
```

The breach check is a separate interface so it can be faked in tests and disabled by configuration:

```go
type BreachChecker interface {
    // Breached reports whether the password appears in the corpus.
    // The error is a *service* failure, distinct from a false answer — the
    // caller's fail-open handling in §12 keys on exactly this distinction.
    Breached(ctx context.Context, password string) (bool, error)
}
```

`Evaluate` deliberately does not take the checker: keeping the pure function pure is what makes DoD item 1 (table-driven tests including boundaries) meaningful. Composition happens one layer up.

## 10. Authorization Rules

Policy is read from the organization the user belongs to, through the RLS-scoped storage API (`P0-08`) — so a caller cannot evaluate against another tenant's policy. There is no direct authorization decision in this task; editing policy is `P2-14` and requires `ORG_ADMIN` there, not here.

## 11. Validation

| Input | Rule |
|---|---|
| Password | Non-empty; at most `maxPasswordBytes` (1024, `P1-01`'s existing bound) before any rule runs — an unbounded input is a memory-cost multiplier |
| `min_length` | Integer, 8–1024. Below 8 is rejected as a policy value, so an administrator cannot configure a policy weaker than the floor |
| `require_uppercase` | Boolean |
| `max_age_days` | Integer, 0–3650. Zero means no expiry |
| Length | Counted in **runes**, not bytes — a twelve-character passphrase in a non-Latin script is twelve characters, and a byte count would silently impose a different rule per language |
| Unicode | Normalized (**NFC**) before the length is counted. Not NFKC: compatibility folding is built for search and identifier matching and rewrites characters in ways that are surprising in a length rule. Not applied to the string that reaches the hasher — `P1-01` stores what the user typed, and normalizing on the way in while verifying the raw input would reject correct passwords |

**The 8-character floor is a policy on policies.** `docs/PLAN/08`'s example is 12; that is a default, not a minimum. Without a floor, "min_length": 1 is a valid configuration and the mechanism built for administrator control becomes the mechanism by which an administrator disables it.

## 12. Error Handling

**The decision `P1-02` step 4 demands: fail open on breach-service unavailability, loudly.**

Recorded as an ADR in `MEMORY/DECISIONS.md`. The reasoning:

Failing closed makes a third-party service a hard dependency of password *changes*. The moment that matters most is the worst possible moment for it: during a security incident, users are told to change their passwords, traffic to this path spikes, and if the corpus service is down or rate-limiting us, fail-closed blocks the exact remediation the incident calls for. We would have converted someone else's outage into our own, in the direction of keeping known-compromised passwords in place.

The risk accepted is bounded and remediable: one password admitted without a corpus check, still subject to every composition rule, still Argon2id-hashed. It can be re-checked later. The risk refused is not bounded — a locked password-change path during an incident has no ceiling.

So: **fail open, never silently.** Every skipped check increments a counter, writes an audit event naming the user and the reason, and is visible on the dashboard. A fail-open that nobody can see is indistinguishable from a breach check that was never wired up — which is the vacuous-verification failure this project has hit repeatedly (`MEMORY/MEMORY-INDEX.md` § lessons). An alert fires when the skip rate is non-trivial, because "the breach check has been failing for three weeks" must be a page, not an archaeology finding.

Fail-open applies **only** to service failure. A definitive "this password is in the corpus" is always a rejection, and a malformed response is a service failure, not an answer.

Other errors:

| Condition | Behaviour |
|---|---|
| Policy JSON malformed or partial | Substitute the secure default per field, log at WARN naming the field, continue. An unparseable policy must not become no policy |
| `min_length` configured below the floor | Clamp to the floor, log at WARN. Same reasoning |
| Password exceeds the byte bound | Reject before any rule runs |
| Corpus service times out | Fail open per above |
| Corpus response malformed | Fail open per above, logged distinctly from a timeout — the two have different causes and different fixes |

**Disclosure asymmetry** (`P1-02` step 6): on an authenticated password-change or reset form, the full `details[]` is returned — the user is choosing a password and needs to know why theirs was refused. On the unauthenticated login path, no policy detail is ever returned; login says only that authentication failed. Policy detail at login tells an unauthenticated caller the composition rules for a tenant, which narrows a credential-stuffing search space for free.

## 13. Edge Cases

| Case | Handling |
|---|---|
| Password is exactly `min_length` | Accepted. Boundary is inclusive, and both sides of it are tested |
| Password is `min_length - 1` runes but more bytes | Rejected. Runes are the unit |
| Uppercase requirement, non-cased script (e.g. Japanese) | `require_uppercase` cannot be satisfied in a script with no case. Documented as a known consequence of `docs/PLAN/08`'s rule; the mitigation is that an org can turn the rule off, and this is why the rule is configurable rather than constant |
| `max_age_days: 0` | No expiry. Not "expires immediately" — a zero that locks out every user is the kind of off-by-one that reads as a security control |
| `password_changed_at` NULL | Not expired. §6 |
| Clock skew makes `changed_at` future-dated | Not expired. Never negative-age arithmetic |
| Corpus returns the prefix with zero suffix matches | Not breached. A legitimate and common answer, distinct from an error |
| Corpus returns a suffix list not containing ours | Not breached |
| Same password submitted twice concurrently | Both evaluated independently; the function is pure and holds no state |
| Policy row changes between validation and hashing | Accepted. The window is milliseconds and the failure mode is one password admitted under the previous policy |

## 14. Abuse Cases

Cross-referenced with `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` and `docs/PLAN/10-THREAT-MODEL.md`.

| # | Abuse case | Control |
|---|---|---|
| A-1 | Attacker submits a known-breached password that satisfies every composition rule | Corpus check; the case that motivates the feature |
| A-2 | Attacker probes the login endpoint to learn a tenant's policy | No policy detail on unauthenticated paths (§12) |
| A-3 | Attacker observes our outbound traffic to infer a user's password | k-anonymity: 5-character prefix only; the query is shared with hundreds of thousands of other hashes. Asserted by an outbound-request test |
| A-4 | Attacker who compromises the corpus service reads our users' passwords | Same control — the service never receives enough to identify a password |
| A-5 | Attacker causes the corpus service to fail, expecting fail-closed to become a denial of service on password changes | Fail open (§12). The attack has no effect beyond a counter increment |
| A-6 | Attacker causes the corpus service to fail, expecting fail-open to admit a breached password | Accepted and bounded (§12): composition rules still apply, the event is audited and counted, and an alert fires on a sustained skip rate |
| A-7 | Administrator configures `min_length: 1`, disabling the control | Floor of 8, clamped and logged (§11) |
| A-8 | Attacker submits a 10MB password to exhaust memory through Argon2 | 1024-byte bound before any rule runs (`P1-01`) |
| A-9 | Timing differences between "policy rejected" and "breach rejected" leak which rule fired | Both are evaluated before responding; the response does not vary in shape by which rule failed on unauthenticated paths |
| A-10 | The password reaches the corpus service through a proxy, log or error message | No password or full hash leaves the process; asserted by a test that inspects the outbound request and by the existing log-redaction gate |

## 15. Logging / Audit Requirements

| Event | Level | Contains | Never contains |
|---|---|---|---|
| Password rejected by policy | Audit event | user id, org id, the rule keys that failed | the password, any part of it, its hash |
| Password rejected as breached | Audit event | user id, org id | the password, its hash, the corpus match count |
| Breach check skipped (fail open) | Audit event + counter + alert | user id, org id, reason (`timeout` / `malformed` / `disabled`) | the password |
| Policy field defaulted | WARN log, once per read | org id, field name, substituted value | — |
| `min_length` clamped to floor | WARN log | org id, configured value, floor | — |

The corpus match count is deliberately excluded. "This password appears 4.7 million times" is a true and interesting fact whose presence in an audit log tells a reader of that log something about the password.

Metrics: `password_policy_rejections_total{rule}`, `password_breach_checks_total{outcome}` where outcome ∈ {`clean`, `breached`, `skipped_timeout`, `skipped_malformed`, `disabled`}.

## 16. Security Controls

- k-anonymity prefix query; the full hash never leaves the process.
- SHA-1 is used **only** as the corpus service's index, never as a password hash. `P1-01`'s Argon2id remains the only thing written to `users.password_hash`. This deserves the note because a SHA-1 call inside an authentication package is exactly what a reviewer should stop on.
- TLS-verified outbound, no proxy that could terminate it.
- Timeouts and context cancellation on every outbound call.
- Composition floor that an administrator cannot configure below.
- Rune-based length, so the rule means the same thing in every script.
- No policy disclosure on unauthenticated paths.
- Fail-open is observable, alertable and audited — the control that makes §12's trade defensible rather than merely convenient.

## 17. Testing Strategy

| Layer | Coverage |
|---|---|
| Unit | Table-driven `Evaluate` across every rule and both sides of every boundary (`min_length ± 1`, exactly at). Rune-vs-byte length. NFKC normalization. All-violations-at-once. `Expired` across NULL, zero, boundary, and future-dated. `Parse` across absent, partial, malformed, out-of-range, and below-floor policies |
| Unit | An outbound-request test asserting the corpus request carries exactly 5 hex characters and that neither the password nor the full hash appears anywhere in the request — URL, headers or body |
| Unit | Fail-open: a checker returning an error admits the password; a checker returning `true` rejects it. The two paths must be distinguishable, because conflating them is how fail-open silently becomes always-open |
| Unit | A control test proving the breach path is not vacuous: a checker that reports `breached` must actually cause a rejection, verified by watching the assertion fail when the wiring is removed |
| Integration | Policy read from a real `organizations.settings` row; changing the row changes enforcement with no code change (DoD item 5), asserted by updating the row mid-test |
| Integration | A malformed settings JSON in the row falls back to defaults rather than failing the request |
| Security | Every abuse case A-1…A-10 |

Coverage floor: `internal/authn` is already at 95.9% with a floor applied (`P0-15`). New code lands inside that floor.

**Not tested against the live corpus service in CI.** A test that requires a third-party service is a test that fails on their bad day, and a suite whose failures nobody believes is worse than a slower one (`P1-03`'s lesson). The client is exercised against a local stub asserting the wire format; the live service is verified once, by hand, on the VM, and recorded.

## 18. Acceptance Criteria

1. A password below `min_length` is rejected, and raising `min_length` in the organization row changes that boundary with no deploy.
2. A password meeting every composition rule but present in the corpus is rejected.
3. With the corpus service unreachable, the same password is accepted, an audit event is written, and the counter increments.
4. The outbound request contains a 5-character prefix and nothing else derived from the password.
5. A rejection at an authenticated change form returns per-field `details[]`; a login failure returns no policy detail.
6. A user whose `password_changed_at` is NULL is never reported expired.

## 19. Definition of Done

The task's own five items, plus the global DoD from `TASKS/00-TASK-CONVENTIONS.md`. Item 3 (the ADR) is satisfied by §12 being written into `MEMORY/DECISIONS.md` as a numbered ADR, not by this specification alone.

## 20. Implementation Sequence

1. `Policy`, `Violation`, `Evaluate`, `Expired` — pure, with the table-driven tests. Nothing else compiles against them yet.
2. `Parse`, with the default/clamp/log behaviour and its tests.
3. The migration adding `password_changed_at`, and the `PG-13` backlog entry.
4. Reading policy from the organization row through the scoped storage API; the integration test that changes the row.
5. The `BreachChecker` interface and its stub-based tests, including the fail-open paths and the non-vacuity control.
6. The k-anonymity HTTP client, with the outbound-request assertion.
7. Metrics, audit events and the alert rule.
8. The ADR.
9. Live verification against the corpus service from the VM, recorded.

Steps 1–2 are the whole DoD item 1 and can be reviewed before any I/O exists.

## 21. Rollback Strategy

The migration is additive and its down migration drops one nullable column, so rolling back the schema loses only the recorded change times.

Rolling back the code returns to `P1-01`'s behaviour: passwords are hashed, unvalidated. No stored data becomes unreadable, and no user is locked out — which is the property that matters, since a rollback that logs everyone out is a rollback nobody performs.

The breach check has its own disable switch (`disabled` is a first-class outcome in the metric, not an absence of data) so it can be turned off without a deploy if the corpus service becomes a problem. Turning it off is visible in exactly the same place as it failing.

## 22. Technical Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Fail-open goes unnoticed and every password is admitted unchecked for months | Medium | High | The alert in §15 is the mitigation, and it is the reason the decision is defensible. Without it, this row is the most likely way this feature quietly stops existing |
| The corpus service adds rate limiting or a paid tier | Medium | Medium | The client is behind an interface with a disable switch; swapping to a self-hosted corpus is a new implementation of one method |
| 273ms added to every password set is felt in the invite flow | Low | Low | Measured, bounded by timeout, and off the login path entirely |
| Normalization changes an existing user's password | — | — | Removed as a risk. Normalization applies only to the count inside `Evaluate`; nothing on the hashing path is touched, so `P1-01`'s `Verify` behaviour is unchanged by construction rather than by care. Where NFC changes the count it lowers it, so the normalization can only make the rule stricter |
| `require_uppercase` is meaningless in non-cased scripts and effectively bans them | Low | Medium | Documented in §13; the rule is configurable per organization, which is the available remedy. Worth raising with the plan owner if the product targets such a market |
