# P2-07 — Authorization Decision Caching

| | |
|---|---|
| **Date** | 2026-09-12 |
| **Task** | `TASKS/PHASE-2-RBAC-MULTITENANCY.md` § P2-07 |
| **Phase** | Phase 2 — RBAC & Multi-Tenancy |
| **Surface** | backend, public docs |
| **Decision** | [ADR-022](../DECISIONS.md) |
| **Branch** | `feat/P2-07-decision-caching` |
| **Status** | Complete — not yet on staging |

---

## A cache here is a correctness risk wearing a performance improvement's clothes

The failure it introduces is that a revoked permission keeps working, and the size of that window is the first thing a security reviewer asks about. So the design puts invalidation first and the TTL second, rather than the other way round.

**Invalidation is the mechanism.** A grant change drops that user's entry for that project; a role edit drops the project's role definitions. A revocation is honoured on the next check.

**The TTL is 30 seconds and it is a backstop**, covering the two cases invalidation cannot: Redis unreachable at the moment of the change, and a grant changed outside the API where no code runs to invalidate anything.

So the window is stateable: **immediate normally, at most 30 seconds if an invalidation was lost.** `docs/PLAN/12` and the card both ask for that sentence to reach consumers, and it is now on the public authorization page with the reason attached — including the observation that if a step cannot be undone, 30 seconds is the window being accepted.

## Inputs, never decisions

`P2-07` step 3 says so, and Phase 4b is why. Once a policy can read `resource.attributes`, a decision depends on values that differ per request, and a cached decision would be served for a resource it was never computed against. Caching what a user holds and what a role carries stays correct whatever the decision function grows into.

## Two keys, because one edit should cost one delete

Grants and role definitions change for different reasons and are invalidated by different events. A single combined entry would mean one role edit invalidating every user who holds it — a scan, or a guess about who they are, and neither belongs in a request path.

```
grants:{org}:{user}:{project}  → the role keys the user holds there
roles:{org}:{project}          → every role in the project, with what it carries
```

The second is per-project rather than per-role because a decision looks up several roles at once, and N lookups to answer one question is how a cache makes things slower.

## The bug the fault-injection test found

`P2-07` step 6: a cache backend failure falls through to the database, never to an allow.

It did not. Pointing the cache at a dead port made the check answer **`503 TIMEOUT`** — the Redis client spent the request's entire budget on its own dial-and-retry, and the request timed out before reaching the database at all. A cache outage had become a service outage, which is exactly what the step exists to prevent.

Each cache operation now gets **50 milliseconds**. Generous for a local Redis — `P1-28` measured the whole token endpoint at a p50 of 9.9ms — and small enough that paying it on every request during an outage is invisible beside the database read that follows.

The test asserts **both halves**: a permission the subject holds is still allowed during a cache outage, and one they do not hold is still denied. A cache outage that denied everything would satisfy "never allow" while being an outage of its own.

## Observing staleness rather than asserting it

`authz_cache_entry_age_seconds` records how old an entry was **when it was used**. A TTL is an upper bound anybody can read off a constant; this is what the fleet actually served.

An invalidation that fails to land is the only thing that makes the TTL load-bearing, so it is counted and logged at `ERROR` rather than swallowed. And a cache that cannot be reached is its own outcome in the lookup counter rather than a miss, because "the cache is down" and "this user was not cached" have different remedies.

## Verified

| | |
|---|---|
| Integration | 4, through the real HTTP path: invalidation on revocation, invalidation on a role edit, fall-through during an outage, and entry age |
| Mutation | 4 controls reverted one at a time |
| Existing suites | All 25 integration packages still green |

| Reverted | Went red |
|---|---|
| Invalidation on revocation | `TestARevocationIsInvalidatedRatherThanWaitedOut` |
| Invalidation on a role edit | `TestChangingARolesPermissionsInvalidatesTheProject` |
| The operation timeout | `TestACacheOutageFallsThroughToTheDatabase` |
| The entry's timestamp | `TestACachedEntryReportsItsAge` |

The invalidation tests do not wait. The TTL is 30 seconds and the tests take milliseconds, so the only thing that can make them pass is the invalidation actually landing.

## Not deployed

The VM is unreachable this session. The cache changes how the decision endpoint reads, so this needs staging — and the hit rate it produces there is the number that would justify revisiting the TTL.
