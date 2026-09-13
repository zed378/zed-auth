# P3-06 — Refresh Token Rotation with Reuse Detection

| | |
|---|---|
| **Date** | 2026-09-13 |
| **Task** | `TASKS/PHASE-3-ADVANCED-SECURITY.md` § P3-06 |
| **Phase** | Phase 3 — Advanced Security |
| **Surface** | backend |
| **Branch** | `feat/P3-06-refresh-rotation` |
| **Status** | Complete |

**Spec**: [`MEMORY/specs/P3-06-refresh-rotation.md`](../specs/P3-06-refresh-rotation.md)

---

## Two bugs that were already shipping

Both produced perfectly working refreshes, which is why neither was noticed.

**A refresh started a new family.** `issue()` is called from two places and
passed an empty `familyID` on both, so every refresh began a fresh lineage and
`replaced_by` was **never written in the history of this service**. Reuse
detection was not merely absent — it was impossible. The schema has said since
`20260908000005` that this is how it works, with a comment explaining that the
columns exist so Phase 3 would "change behaviour rather than storage shape".
The storage shape was right and nothing used it.

**The absolute family lifetime was recomputed on every issuance.**
`family_expires_at` was set to `now + FamilyLifetime` each time, so a client
refreshing continuously held a session that never aged out. That is card step
6's own abuse case, live since Phase 1 and through all of Phase 2.

Neither is visible from outside. The second is a real weakening of session
lifetime that nothing would have caught, because nothing asked.

## The part that decides whether the feature survives production

Reuse detection is easy. Telling reuse apart from a client retrying after a lost
response is not, and getting it wrong in the strict direction logs real users
out — until an operations team disables the protection, at which point the
control is gone **and everybody believes it is there**. That outcome is worse
than never having built it.

So a retry is admitted on two conditions, and the second carries the weight:

| Condition | Why |
|---|---|
| Rotated within 30 seconds | Bounds the window |
| **The successor is untouched** | A retry happens *because* the successor never arrived. If it has been used, the client received it, and this presentation is somebody else's copy |

The time bound alone would be a thirty-second hole: a thief has every reason to
use a stolen token immediately, so "recently rotated" describes theft at least
as well as a retry. Two tests make the pair explicit —
`TestARetryAfterALostResponseIsNotReuse` and
`TestTheSamePresentationIsTheftOnceTheSuccessorIsUsed` differ **only** in
whether the successor was spent, and both are well inside the window.

## Three things the first implementation got wrong

All caught by the integration tests, none by the unit tests.

1. **Double-minting.** `issue()` still created its own refresh token on the
   refresh path while `rotate` created a successor, so every refresh produced
   two tokens — one in the right family, one orphan starting a new one. Three
   refreshes produced four families. Fixed with a `mintRefresh` flag rather than
   by deleting the branch, because the authorization-code path still needs it.
2. **The retry could not complete.** `Rotate`'s `WHERE replaced_by IS NULL` is
   what makes concurrent rotation a losable race — and it also refused the
   legitimate retry, which by definition re-rotates an already-rotated
   predecessor. The retry path now supersedes: it revokes the successor the
   client never received and moves the link, conditionally on the link still
   pointing where it expects, so a retry cannot race another retry either.
3. **A successor could outlive its family.** Its expiry is now capped at the
   family's, which the `refresh_tokens_family_outlives_token` constraint would
   otherwise have refused with an error reading like a bug.

## The mutation run found two tests proving less than their names

Twelve controls. Nine red immediately; **three were green**, and two of those
were my errors rather than gaps.

**The absolute-lifetime test was too weak to catch its own bug.**
`FamilyLifetime` is ninety days, so a recomputed `now + FamilyLifetime` lands
microseconds from the inherited one when the refreshes happen in the same
second — and the test compared them with a one-second tolerance. The bug it
exists for would have passed straight through. It now asserts that the family
has exactly **one distinct** `family_expires_at`, which no clock skew can
satisfy by accident, plus a row count so the assertion is not vacuous if
rotation stops producing rows.

**A comment overstated a guard.** The unknown-token branch was documented as
stopping somebody killing a family by guessing. Mutating it changed nothing,
which is how the overstatement was found: a kill needs a real rotated lineage,
so an invented token could never reach one however that branch answered. The
guard stays — a function reporting an unknown token as reuse would be lying to
its caller — but the comment now says what is true.

**The third was defence in depth, not a gap.** Disabling detection entirely left
`TestARotatedRefreshTokenCannotBeReused` green, because `Rotate`'s conditional
`UPDATE` independently refuses a second rotation. What detection adds on top is
the family kill and the alert, so the mutation was retargeted there. Both facts
are now in the source.

## A bypass the architecture test made me declare

`LookupLineage` reads through `db.SQL()` rather than inside `WithTenant`, and
`tests/security/tenancy_test.go` failed the build for it.

Correctly. The read genuinely has to bypass tenant scope — reuse detection must
see a token whose family may **already be revoked**, which is precisely what
`refresh_token_by_hash` hides on purpose, and there is no tenant to scope to
until the token resolves to one. But the test's point is not that such reads are
forbidden; it is that each one is **named with its reason**, so a bypass can
never appear by accident or by copy-paste.

So the entry says what the function may see and what it deliberately does not
return: the family and the tenant, never the user, the client or the scope,
because a caller asking this question may be holding a stolen token.

## The alert

`auth_refresh_reuse_detected_total`, unlabelled and deliberately so. A label
invites a dashboard that breaks it down by client and a threshold per series;
this is a counter whose correct alert threshold is **any increase at all**,
because a refresh token existing in two places is never routine. Labels would
make it look like something to tolerate at a low rate.

It is a separate metric from `auth_token_errors_total` for the same reason:
token errors have a non-zero baseline — expired tokens, clients that never clean
up — and this has none.

A second presentation of an already-dead token does not re-alert. An attacker
retrying should not be able to generate one page per attempt.

## Verified

| | |
|---|---|
| Unit | The retry truth table — 8 cases, each with the reason it answers as it does |
| Integration | 11 against real Postgres: the criterion, the family kill, both halves of the retry distinction, the two fixed bugs, client binding, hashes only |
| Mutation | 12 controls, each turning its own test red |

## What this does not deliver

- **Binding a refresh token to a device or a DPoP key.** `docs/PLAN/05` does not
  ask for it and it would change the contract for every integration.
- **A console view of token families.** `P3-11`'s sessions screen if it is
  wanted.

Not yet on staging: the VM at 10.1.200.13 has been unreachable since the deploy
key was lost with a session scratchpad.
