# P2-06 — `POST /v1/authz/check`

**Task**: `TASKS/PHASE-2-RBAC-MULTITENANCY.md` § P2-06
**Depends on**: `P2-03`
**Plan refs**: `docs/PLAN/05-API-CONTRACT.md` § Authorization Check, `docs/PLAN/08-AUTHORIZATION.md` Part A, `docs/PLAN/12-PERFORMANCE.md`, `docs/PLAN/13-OBSERVABILITY.md`

---

## 1. Business Objective

`P2-04` put roles in the token. A token is a snapshot, and the gap between "this token says so" and "this is still true" is exactly where a revoked administrator keeps their access for another fifteen minutes.

This is the endpoint that closes it, and the one `docs/PLAN/08` tells consumers to prefer for anything sensitive. It is also the endpoint expected to carry more traffic than everything in Phase 1 combined, which is why `docs/PLAN/12` gives it the tightest targets in the document: p50 < 20ms, p95 < 80ms, p99 < 150ms.

## 2. Actors

| Actor | What they do |
|---|---|
| A consumer application | Asks "may this user do this?" about its **own** project, holding its own token |
| The console | Will use it for the same reason any consumer does |
| Phase 4b | Extends the answer with policy evaluation, without changing the shape |

## 3. Functional Requirements

- **FR-1** The documented request and response shape, exactly.
- **FR-2** The decision reads **live grant data**, never the caller's token claims.
- **FR-3** The permission asked about is `resource.type` + `:` + `action`.
- **FR-4** The decision is scoped to the caller's own organization and project.
- **FR-5** `matched_policy` and `reasons` are populated in RBAC terms today, and change content rather than shape when ABAC arrives.
- **FR-6** Raw `resource.attributes` never reach any log.
- **FR-7** A dependency failure denies. It never allows.

## 4. Non-Functional Requirements

- **NFR-1** One query per decision. `P1-28` found Postgres CPU to be the ceiling, and this endpoint is expected to outweigh the rest of the API.
- **NFR-2** Decision latency, allow/deny ratio and error rate are instrumented separately — an outage must not look like a deny spike.

## 5. Dependencies

| Needs | Provided by |
|---|---|
| Grants, live | `P2-03` |
| Roles and permission keys | `P2-01` |
| Caller authentication, quota, audit guard | `P1-15` |

## 6. The decision, in full

```
permission := resource.type + ":" + action
allowed    := ∃ role ∈ grants(subject, caller's project) : permission ∈ role.permission_keys
```

That is the whole of Phase 2's authorization semantics. `resource.id` and `resource.attributes` are accepted and **not used** — they are what Phase 4b's policies will read, and accepting them now means a consumer that sends them today does not change its code then.

**Why `resource.type:action` rather than a separate permission field.** `P2-01` chose `resource:action` for permission keys, from `docs/PLAN/08`'s own examples. The contract's example asks to `approve` a `purchase_request`; the permission that grants it is `purchase_request:approve`. Making the caller send the permission separately would let the two disagree, and a consumer that sends `action: "delete"` with `permission: "read"` has written a bug this endpoint cannot see.

## 7. API Contract

Exactly `docs/PLAN/05`'s example. `POST /v1/authz/check`, not nested under an organization: the organization comes from the caller's token, because an endpoint that takes an organization in the path is an endpoint somebody will eventually call with a different one.

## 8. Frontend Changes

None.

## 9. Backend Changes

`internal/authz`: a decision function with no I/O, a store read, and the handler. The decision function being pure is what lets Phase 4b add policy evaluation as a second step in one place rather than threading it through a query.

## 10. Authorization Rules

**The caller may only ask about its own organization and its own project.** Both come from the token, never from the body. This is the difference between an authorization endpoint and an authorization oracle: given a subject id and a free choice of project, a caller could map another organization's grants one request at a time.

`ORG_ADMIN` is **not** required. The caller is a consumer application asking about its own users — requiring an administrative role would mean every service that checks a permission holds an administrative one, which is the opposite of least privilege. What it needs is a valid token for a client in the project it is asking about.

## 11. Validation

| Field | Rule |
|---|---|
| `subject.user_id` | A UUID. Not required to exist — see §12 |
| `action` | Non-empty, and `type:action` must form a valid permission key |
| `resource.type` | Non-empty |
| `resource.attributes` | Accepted, bounded, never logged, never used in Phase 2 |
| `context` | Accepted and unused |

## 12. The oracle problem, and what the answer must not reveal

A subject who does not exist and a subject with no matching role must produce the **same response**. Not merely the same `allowed` — the same `reasons`, byte for byte. Otherwise the endpoint answers "does user X exist in this organization?" for anybody with a valid client token, which is `docs/SECURITY/02` §12's enumeration, dressed as an authorization question.

The distinction is recorded **server-side**, in the log, where an operator can see it and a caller cannot.

## 13. Error Handling and failing closed

A dependency failure returns **`503` with the standard error envelope**, and the documentation says a consumer must treat any non-`2xx` as denied.

Not `200 {"allowed": false}`, which would be a lie with consequences: it says "we checked and the answer is no", so a consumer may cache it, an operator watching the allow/deny ratio sees a policy change rather than an outage, and the error rate that `docs/PLAN/13` wants alerting on stays flat while every decision in the fleet fails.

`docs/PLAN/13`'s rule — "availability of a decision is never a reason to weaken security posture" — is satisfied: nothing is allowed. What is added is that the failure is *visible as a failure*.

## 14. Abuse Cases

| # | Scenario | Control | Test |
|---|---|---|---|
| A-1 | §2 Asking about a subject in another organization | The organization comes from the token | `TestACheckCannotCrossOrganizations` |
| A-2 | §12 Enumerating users by watching the answer | A nonexistent subject and an unauthorized one are byte-identical | `TestANonexistentSubjectIsIndistinguishableFromAnUnauthorizedOne` |
| A-3 | §12 Enumerating projects by choosing one | The project comes from the caller's client | `TestTheProjectComesFromTheTokenNotTheBody` |
| A-4 | Attribute injection reaching a log or a query | Attributes are never logged and never interpolated | `TestResourceAttributesNeverReachTheLog` |
| A-5 | §1 Deciding from the caller's token claims | The decision reads grants | `TestARevocationIsHonouredOnTheNextCheck` |
| A-6 | Fail-open on dependency failure | 503, never allowed | `TestADependencyFailureDenies` |

## 15. Logging / Audit Requirements

**Not audited.** A decision is a read, it happens on every protected request in every consumer application, and writing an audit row per decision would multiply the audit log by the traffic of the entire fleet — turning the log an investigator reads into a firehose nobody can query.

What is recorded is a **metric**: decision count by outcome, latency, and error rate, separately, so an outage cannot hide inside a deny rate.

The log line carries subject, action, resource **type**, outcome and duration. It never carries `resource.id`, and never `resource.attributes` — `docs/PLAN/13` and `CLAUDE.md` both name the attributes explicitly, and the id is a consumer's own business identifier.

## 16. Security Controls

Scope from the token; identical answers for absent and unauthorized; attributes never logged; fail closed; and a bound on request size so attributes cannot be used as a memory amplifier.

## 17. Testing Strategy

Unit tests for the pure decision. Integration for the live-data property, the oracle equivalence and tenancy. Fault injection — a closed database — for failing closed. And a load measurement against `docs/PLAN/12`'s targets, using `scripts/loadtest`.

## 18. What is deferred

`matched_policy` in Phase 2 names the **role** that granted the permission, because in RBAC the role *is* the policy that matched. Phase 4b replaces its content with a policy name. The field exists now so that change is content rather than shape.

Caching is `P2-07`, deliberately: a cache over a decision that is not yet measured is a cache tuned against a guess.
