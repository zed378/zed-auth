# P3-06 — Refresh Token Rotation with Reuse Detection

Feature specification, per `docs/PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`.

| | |
|---|---|
| **Task** | `TASKS/PHASE-3-ADVANCED-SECURITY.md` § P3-06 |
| **Phase** | Phase 3 — Advanced Security |
| **Surface** | backend |
| **Plan refs** | `docs/PLAN/09-SECURITY.md` § Tokens & Keys, `docs/PLAN/04-DATA-MODEL.md` § refresh_tokens, `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 3, `docs/PLAN/10-THREAT-MODEL.md` |
| **Depends on** | `P1-07` (the token endpoint) |
| **Written** | 2026-09-13 |

---

## 1. Business objective

A refresh token is the longest-lived credential this service issues. An access
token expires in minutes; a refresh token is what turns one theft into
persistent access.

Rotation makes a stolen token **detectable**. Without it, a thief and the real
client can both refresh indefinitely and nothing distinguishes them. With it,
the two are on a collision course: whoever refreshes second presents a token
that has already been rotated away, and that presentation is the alarm.

`docs/PLAN/17`'s Phase 3 criterion is one sentence — *a rotated refresh token
cannot be reused, verified by automated test*.

## 2. Actors

| Actor | Interest |
|---|---|
| A legitimate client | Must not be logged out by a network hiccup |
| A client that retried after a timeout | Must be able to recover with the only token it holds |
| A thief holding a copied token | Must be detected, and must lose the whole family |
| An operator | Needs reuse to be an alert, not a log line among thousands |

## 3. Functional requirements

| # | Requirement |
|---|---|
| F-1 | Every refresh issues a successor and links the presented token to it, atomically |
| F-2 | Presenting an already-rotated token revokes the **entire family** |
| F-3 | Reuse is audited and alerted, distinctly from ordinary refresh failures |
| F-4 | A client retrying after a lost response is **not** treated as reuse |
| F-5 | A family has an absolute lifetime that refreshing cannot extend |
| F-6 | Only hashes are stored, and revocation is immediate rather than TTL-bound |
| F-7 | A refresh token is bound to the client it was issued to |

## 4. Non-functional requirements

- Rotation is on the hot path of every long-lived integration. It must be one
  round trip more than today, not several.
- A failed rotation must never leave a successor live with its predecessor also
  live — that is the exact state reuse detection exists to make impossible.

## 5. Dependencies

`P1-07`'s token endpoint and `RefreshStore`; the `refresh_tokens` columns
`family_id`, `replaced_by` and `family_expires_at`, which `20260908000005`
created **for this task** so that this phase would change behaviour rather than
storage shape.

## 6. Data model changes

**No column changes.** One new SECURITY DEFINER function,
`refresh_token_lineage`, and one index on `family_id`.

The function is a **second** read rather than a relaxation of
`refresh_token_by_hash`. That one deliberately returns only live tokens, so a
dead token cannot be acted on by a caller who forgot to check — right for the
happy path, and useless here, because reuse detection is precisely the question
"what happened to this token". Keeping them separate keeps the liveness filter
where the happy path depends on it.

`refresh_token_lineage` returns no user, no client and no scope. A caller
asking this question is deciding whether to kill a family; everything else
would be information handed to somebody holding a token that may be stolen.

## 7. API contract

Unchanged. `POST /oauth/token` with `grant_type=refresh_token` returns a
`refresh_token` in the response as it does today — it is simply a different one
each time.

## 8. Frontend changes

None.

## 9. Authorization rules

A refresh token is bound to its client (already enforced) and to its session
where one exists. Rotation changes neither.

## 10. Validation rules

| Input | Rule |
|---|---|
| `refresh_token` | Must resolve to a live token whose client matches the caller |
| A rotated token | Reuse, unless it passes the retry test in § 12 |
| A token from an aged-out family | Refused, and the refusal is not reuse |

## 11. Error handling

Every refusal answers `invalid_grant` with one message. Reuse, an unknown
token, an expired one and an aged-out family are indistinguishable to the
caller — distinguishing them would tell a holder of a dead token that it was
once real, and tell a thief that they were caught.

The **log** distinguishes all of them, and reuse is the one that alerts.

## 12. Edge cases

**The one that decides whether this feature is worth having.**

A client sends a refresh, this service rotates and answers, and the answer does
not arrive — a dropped connection, a proxy timeout, a process killed
mid-response. The client still holds the old token, because that is the only
token it has, and it retries.

Treat that as reuse and the client is logged out. Do it often enough and an
operations team disables the protection — at which point the control is gone
**and everybody believes it is there**, which is worse than never having built
it.

So a retry is admitted on **two** conditions, and the second is the one that
carries the weight:

| Condition | Why |
|---|---|
| Rotated within `RotationGrace` (30s) | Bounds the window. On its own it would be a 30-second hole: a thief has every reason to use a token immediately, so "recently rotated" describes theft at least as well as a retry |
| **The successor is untouched** | A retry happens *because* the successor never arrived. If it has been used, the client did receive it, and the old token is in somebody else's hands |

Thirty seconds is longer than a failed request plus its retry, and shorter than
any human-scale interval. Longer widens the window a stolen token works in;
shorter starts logging out clients on slow networks, which is the failure that
gets the control switched off.

Other cases:

| Case | Behaviour |
|---|---|
| Two requests rotate the same token simultaneously | One wins. The `WHERE replaced_by IS NULL` makes it a losable race rather than two successors from one predecessor; the loser is reuse, because the winner already holds the successor |
| The family ages out mid-rotation | Refused as not-found, before a successor is written with an expiry already past |
| A successor would outlive its family | Its expiry is capped at the family's |
| Reuse of a token whose family is already revoked | Refused. The family is already dead; there is nothing to kill twice |
| A token this service never issued | **Not** reuse. Answering otherwise would let anybody kill a family by guessing |

## 13. Abuse cases

| # | Scenario | Control |
|---|---|---|
| A-1 | Replay after rotation | F-2: the family dies |
| A-2 | Stolen token used in parallel with the real client | Whoever refreshes second is detected; the family dies, which logs out the thief **and** the victim — deliberately, because the alternative is guessing which is which |
| A-3 | A refresh token used by a different client | Already enforced; asserted here so it stays |
| A-4 | Indefinite extension by continuous refreshing | F-5: the family's absolute expiry is **inherited, not recomputed** |
| A-5 | Kill somebody's family by presenting a guessed token | An unknown token is not reuse |
| A-6 | Widen the retry window by never using a successor | Bounded by `RotationGrace` regardless |

## 14. Logging and audit

A new audit event, `token.refresh.reuse_detected`, carrying the family, the
client and how many tokens were revoked — never a token or a hash.

`docs/PLAN/13` § Alerting: this is a metric an operator pages on. Ordinary
refresh failures are routine (expired tokens, clients that never clean up);
reuse is **never** routine. It means a credential was copied or a client is
broken, and both want a human.

## 15. Security controls

- Rotation and its link are one transaction.
- The link is conditional (`WHERE replaced_by IS NULL`), so concurrency cannot
  produce two live successors.
- Family revocation is a single `UPDATE`, so it is immediate rather than
  waiting for a TTL.
- Only SHA-256 hashes are stored; the plaintext exists in one response.
- The family's absolute expiry is inherited on every rotation.

## 16. Testing strategy

| Level | What |
|---|---|
| Unit | The retry test's truth table, which is where the subtlety is |
| Integration | Real Postgres: rotation, reuse killing a family, the legitimate retry, the concurrent race, the absolute lifetime, client binding |
| Security | A-1 through A-6, each failing when its control is reverted |
| Mutation | Every control above |

## 17. Acceptance criteria

The card's Definition of Done, including `docs/PLAN/17`'s literal sentence as
an automated test.

## 18. Implementation sequence

1. The migration.
2. `LookupLineage` and the retry test.
3. `Rotate`, with the inherited family expiry.
4. The refresh grant: rotate, detect, kill, alert.
5. Tests, then the mutation run.

## 19. Rollback strategy

Additive migration. A rolled-back build stops rotating and stops detecting;
tokens issued before the rollback keep working until they expire. Worth knowing
before rolling back: **rotation without detection is the dangerous half**, so a
build that rotates and cannot detect is worse than one that does neither.

## 20. Technical risks

**Two bugs already in the tree, which this task exists to fix and which are
worth naming as findings rather than as work.**

- `issue()` passes `familyID: ""` on every path, so **a refresh starts a new
  family**. There is no lineage, and `replaced_by` has never been written. Reuse
  detection was impossible, not merely absent.
- `Issue()` recomputes `family_expires_at` as `now + FamilyLifetime` on every
  issuance. Combined with the above, **continuous refreshing extends a session
  forever** — which is card step 6's abuse case, live in Phase 1 and Phase 2.

Neither is visible from the outside: both produce working refreshes. The second
is a real weakening of session lifetime that nothing would have caught, because
nothing asked.

**Killing a family logs out the victim as well as the thief.** That is the
correct trade — the alternative is deciding which of two identical presentations
is genuine — but it is a real cost, and the alert is what makes it actionable
rather than merely disruptive.

## 21. What this task does not deliver

- **Binding a refresh token to a device or a DPoP key.** `docs/PLAN/05` does not
  ask for it, and it would change the token contract for every integration.
- **A console view of token families.** `P3-11`'s sessions screen is the place
  for that if it is wanted.
