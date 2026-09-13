# P3-04 — Recovery Codes and the Lost-Device Process

Feature specification, per `docs/PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`.

| | |
|---|---|
| **Task** | `TASKS/PHASE-3-ADVANCED-SECURITY.md` § P3-04 |
| **Phase** | Phase 3 — Advanced Security |
| **Surface** | backend + Management API + docs |
| **Plan refs** | `docs/PLAN/04-DATA-MODEL.md` § `user_recovery_codes`, `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 3, `docs/SECURITY/04-INCIDENT-RESPONSE-PLAYBOOKS.md` |
| **Depends on** | `P3-02` (factors), `P3-03` (the challenge step) |
| **Written** | 2026-09-13 |

---

## 1. Business objective

`P3-03` made an enrolled factor able to stop a login. This is the task that
decides what happens when it stops the *wrong* login — the real user, holding a
phone that is lost, wiped, or in a drawer at the office.

A factor with no recovery path produces one of two outcomes, and both are worse
than the factor: permanently locked-out users, or an ad-hoc support process
invented under pressure by whoever answers the phone. The second is the
dangerous one, because an undocumented reset path **is** an authentication
mechanism — one with no threat model, no audit, and no one accountable for its
rules.

`docs/PLAN/17` accepts a manual process. It does not accept an undocumented one.

## 2. Actors

| Actor | Interest |
|---|---|
| A user who lost their device | Needs back in, without a support queue |
| A user who still has their device | Must not be weakened by the existence of the path |
| An administrator | Needs a defined procedure, and protection from being socially engineered |
| An incident reviewer | Needs every recovery use and every admin reset findable |
| An attacker | Will attack the recovery path precisely because it is the weakest one |

## 3. Functional requirements

| # | Requirement |
|---|---|
| F-1 | A batch of single-use recovery codes is generated for a user, returned exactly once, and stored hashed |
| F-2 | A recovery code answers the login challenge in place of a factor |
| F-3 | Consuming a code marks it used; a used code never works again |
| F-4 | The remaining count is reported so the user can be warned as it runs low |
| F-5 | Regeneration invalidates every outstanding code |
| F-6 | An administrator with the right manager role may clear another user's factors, through a documented, audited endpoint |
| F-7 | Recovery attempts are bounded as strictly as password attempts |

## 4. Non-functional requirements

- Verifying a submitted code is **O(1) work**, not O(number of codes). See § 20.
- A recovery code is typed by a person under stress, from paper or a password
  manager. Formatting, case and separators must not be able to make a correct
  code wrong.

## 5. Dependencies

`P3-03`'s challenge step and its per-user attempt bound; `P3-02`'s factor store;
`P2-xx`'s manager-role authorization for the administrator path.

## 6. Data model changes

`user_recovery_codes`, as `docs/PLAN/04` specifies it, plus two columns the plan
does not name and the behaviour requires:

| Column | Why |
|---|---|
| `org_id` | Every other tenant-scoped table has one, and RLS is keyed on it. Without it this table would be the one place a recovery code could be read across tenants |
| `batch_id` | Regeneration invalidates a whole batch (F-5). A batch id makes that one statement and makes "which batch did this code come from" answerable in an incident |

`code_hash` is **`bytea`, not `text`** — see § 20.

## 7. API contract

| Endpoint | Who |
|---|---|
| `POST /v1/organizations/{org_id}/users/{user_id}/mfa-reset` | `ORG_ADMIN`, organization scope |

It clears the user's factors and outstanding recovery codes, and returns
nothing. It is deliberately **not** an endpoint that returns new codes: an
administrator who could mint a working credential for another user's account
could take over that account, and the whole point of the documented process is
that the administrator returns the user to enrolment rather than into a session.

The self-service endpoints that show and regenerate one's own codes belong with
`P3-12`'s account screen, for the reason `P3-02`'s enrolment endpoint does: they
need "requires recent authentication", and there is no self-service surface yet.

## 8. Frontend changes

The challenge page gains a way to answer with a recovery code. Nothing else —
the console screens are `P3-12`'s.

## 9. Authorization rules

- A recovery code authenticates **the user the challenge names**, never a user
  the request names.
- `mfa-reset` requires `ORG_ADMIN` **in the target user's organization**,
  enforced server-side against the manager-role tables rather than the token's
  claim snapshot.
- An administrator may reset; an administrator may **not** read, mint, or use
  another user's codes. There is no endpoint that returns them.

## 10. Validation rules

| Input | Rule |
|---|---|
| A submitted code | Normalised — upper-cased, separators and whitespace stripped — before comparison |
| A code's shape | Checked before any store read, so a malformed value costs no query |
| `mfa-reset` | The target must be a user in the named organization |

## 11. Error handling

A wrong recovery code, a used one, and one belonging to another user all answer
identically — the same message the challenge step gives a wrong TOTP code. The
distinction between "wrong" and "already used" is a fact about the user's own
history, and on this page the caller has proven a password, but an attacker who
has *also* proven a password is exactly who this control is for.

## 12. Edge cases

| Case | Behaviour |
|---|---|
| The last code is consumed | The login succeeds and the count is zero. Warning the user is F-4's job, not a refusal |
| A user with codes and no factor | No challenge is issued at all — recovery codes are a way to answer a challenge, not a reason to raise one |
| Regeneration mid-challenge | The live challenge's codes stop working, because the batch is gone. The user restarts |
| An administrator resets a user mid-challenge | Same: the challenge's factors no longer exist |
| Two browsers submit the same code at once | One wins. The consume is conditional on `used_at IS NULL`, in the UPDATE |

## 13. Abuse cases

| # | Scenario | Control |
|---|---|---|
| A-1 | Brute-force recovery codes | 80 bits of entropy, plus `P3-03`'s per-user bound counting recovery guesses in the same window as code guesses |
| A-2 | Reuse a code seen over a shoulder | Single-use, enforced in the `WHERE` clause rather than by a read-then-write |
| A-3 | Use another user's code | The challenge names the user; the lookup is scoped to them |
| A-4 | Social-engineer the administrator | **Procedural.** § 14 and the runbook name the verification requirement |
| A-5 | An administrator takes over an account | The reset endpoint returns no credential and creates no session. It can only return somebody to enrolment |
| A-6 | Harvest codes from a database dump | Hashed, and the input has 80 bits of entropy — see § 20 |

## 14. Logging and audit

Three events, all at elevated visibility because these are the rows an incident
review looks for first (`docs/SECURITY/04`):

| Event | When |
|---|---|
| `user.mfa.recovery_used` | A recovery code completed a login. Payload: the remaining count |
| `user.mfa.codes_generated` | A batch was issued. Payload: the count and the batch id, never a code |
| `user.mfa.reset_by_admin` | An administrator cleared another user's factors. Payload: the actor, the target, how many factors and codes were destroyed |

No event ever carries a code, a hash of one, or a batch a code could be
recovered from.

## 15. Security controls

- Codes are generated from the CSPRNG, 80 bits each, and never logged.
- The plaintext exists in one response and nowhere else — not in a column, not
  in an audit payload, not in a log line.
- Consumption is a conditional `UPDATE`, so a race cannot spend one code twice.
- Regeneration deletes the previous batch in the same transaction that inserts
  the new one, so there is no window where both work or neither does.
- The administrator path destroys credentials and creates none.

## 16. Testing strategy

| Level | What |
|---|---|
| Unit | Formatting and normalisation; the hash; the "one code, one use" logic |
| Integration | Real Postgres: consumption, the race, regeneration, RLS confinement, the admin reset |
| Security | A-1 through A-6, each failing when its control is reverted |
| E2E | The lost-device walkthrough, recorded — `docs/PLAN/17`'s actual criterion |
| Mutation | Every control above |

## 17. Acceptance criteria

The card's Definition of Done. The fifth item — "an end-to-end lost-device
recovery has been walked through and recorded" — is satisfied by a runbook that
was *executed*, not merely written, with its transcript in the record.

## 18. Implementation sequence

1. The migration.
2. Code generation, formatting and hashing.
3. The store: issue, consume, count, regenerate, clear.
4. The challenge step's recovery answer.
5. The administrator endpoint, its policy entry and its audit event.
6. The runbook, walked through against the local stack.
7. Tests, then the mutation run.

## 19. Rollback strategy

The migration is additive — a new table and nothing else — so a rollback leaves
a previous build working, with the table present and unread. Codes issued before
a rollback stay valid after it.

## 20. Technical risks

**`docs/PLAN/04` says `code_hash text`, "hashed with the same rigor as a
password". This implementation uses SHA-256 into `bytea`, and that is a
deliberate, visible deviation.**

"The same rigor as a password" reads as Argon2, and Argon2 is the wrong
primitive here for two reasons:

- **It buys nothing.** A slow KDF exists because passwords are low-entropy and
  human-chosen, so an offline attacker can enumerate the plausible ones. A
  recovery code is 80 bits from the CSPRNG. There is no candidate list. SHA-256
  and Argon2 are equally uncrackable against an input like that, and the
  stronger-sounding one is not stronger.
- **It costs a denial of service.** A user holds ten codes and a submitted code
  is compared against all of them. At Argon2id's tuned parameters that is ten
  memory-hard computations — hundreds of megabytes touched — per attempt, on a
  path an attacker who holds a password can drive.

So the rigor the plan is asking for is delivered by the **entropy of the code**
rather than by the cost of the hash, which is the property that actually decides
whether a dump is exploitable.

Recorded as `PG-39` rather than made silently.

**The administrator path is the weakest link, and it is procedural.** No
technical control in this task prevents an administrator from being talked into
resetting the wrong person's factors. What the code can do — and does — is make
the path narrow (it destroys, it never mints), make it permissioned, and make it
loud in the audit log. The rest is the runbook, and the runbook is the
deliverable.

## 21. What this task does not deliver

- **Self-service display and regeneration** of one's own codes. `P3-12`, with
  the account screen and its "requires recent authentication".
- **Automatic generation at enrolment** (card step 1's wording). There is no
  enrolment endpoint yet — `P3-02` § 21 — so the mechanism lands here and the
  call site lands with `P3-12`.
- **The low-count warning in a user interface** (F-4). The count is exposed;
  the place that shows it is `P3-12`'s.
