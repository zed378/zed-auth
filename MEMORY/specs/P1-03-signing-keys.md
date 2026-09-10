# Feature Specification — Signing Key Management, JWKS, and Rotation

**Task**: `P1-03`
**Date**: 2026-09-09
**Template**: `docs/PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`
**Spec required because**: cryptographic core (`CLAUDE.md`)

---

## 1. Business Objective

Every token this service issues is trusted because of a signature. This is the code that makes the signature mean something, and the code that lets the key behind it change without an outage.

Two properties have to hold at once, and they pull against each other:

- **Only this service can sign.** `docs/PLAN/02` § Constraints is absolute: no third party holds the private key.
- **Anyone can verify, without asking us.** Consumer services validate tokens against a published public key, so an authorization check does not depend on this service being reachable at that instant.

Rotation is where both get tested. A rotation that invalidates outstanding tokens logs every user out simultaneously; one that leaves the old key signing forever is not a rotation.

## 2. Actors

| Actor | Interest |
|---|---|
| The service | Signs with exactly one key; verifies against several |
| A consumer application | Fetches JWKS, verifies locally, caches the key set |
| An operator | Runs a rotation deliberately, during business hours |
| An attacker | Wants a token this service did not sign to be accepted |

## 3. Functional Requirements

**FR-1** — Generate RS256 or ES256 key pairs. Never HS256 (`docs/PLAN/07`).

**FR-2** — Private keys are stored via the `P0-14` secret reference (`file:`/`env:`). The `signing_keys` table holds a *reference*, never material — enforced by a `CHECK` constraint from `P0-07`.

**FR-3** — Four states, at most one `current` per purpose (enforced by a partial unique index):

| State | In JWKS | Signs |
|---|---|---|
| `next` | yes | no |
| `current` | yes | **yes** |
| `previous` | yes | no |
| `retired` | no | no |

**FR-4** — JWKS publishes every non-retired key, each with a stable distinct `kid`.

**FR-5** — Rotation is an explicit operator command, not a timer. `docs/PLAN/09`'s 90-day cadence is an operational expectation, not an automatic job. A rotation that fails at 03:00 is worse than one done deliberately at 11:00.

**FR-6** — Key state lives in the database, never in the binary or its config, so rolling the application back does not invalidate tokens signed under a newer key (`docs/PLAN/14` § Rollback Strategy).

**FR-7** — The key set is cached with a bounded TTL; a new key is picked up by every instance within it.

## 4. Non-Functional Requirements

- Verification must not hit the database on the hot path — `/v1/authz/check` is called on every protected request across every consumer app (`docs/PLAN/12`).
- Signing adds to every token issuance; the cache makes it a memory lookup plus the signature itself.

## 5. Dependencies

`P0-14` (secret references), `P0-07` (`signing_keys` table), `P0-08` (this table is instance-level, not tenant-scoped — keys belong to the deployment).

## 6. Database Changes

**None.** `P0-07` created `signing_keys` with the four-state model, the partial unique index on `current`, and the constraint refusing PEM material in the reference column. That was written for this task; nothing needs to change.

## 7. API Contract

None here. `P1-04` publishes `/.well-known/jwks.json` and the discovery document; this task provides the key set it serves.

## 8. Authorization Rules

Not tenant-scoped. Keys belong to the instance, so `signing_keys` has no `org_id` and no RLS policy. Access is by database privilege: the runtime role reads; only migrations and the rotation command write.

## 9. Validation

- Algorithm must be `RS256` or `ES256` (`CHECK`).
- `kid` unique and non-blank (`CHECK` + unique index).
- The private key reference must resolve, and resolution enforces `0400`/`0600` on `file:` (`P0-14`).
- An RSA key shorter than 2048 bits is refused at generation *and* at load.

## 10. Error Handling

A key that cannot be loaded is a startup failure, not a degraded mode. A service that starts without a signing key would accept requests and fail every login — refusing to start is the smaller outage and the one with an obvious cause.

## 11. Edge Cases

| Case | Behaviour |
|---|---|
| No `current` key | Startup fails with a message naming the rotation command |
| Two `current` keys | Impossible — partial unique index |
| Token references an unknown `kid` | Rejected, no fallback to "try every key" |
| Token has no `kid` | Rejected. Trying all keys turns a verification failure into an oracle |
| A `previous` key is asked to sign | Refused; only `current` signs |
| Clock skew across instances | Not this task's concern; `P1-07` sets `nbf`/`exp` with leeway |

## 12. Abuse Cases

From `docs/SECURITY/02` §1 and the task card. Each gets a test.

**A-1 — `alg: none`.** A token whose header claims no algorithm must be rejected. Verification pins the expected algorithm rather than reading it from the header.

**A-2 — Algorithm confusion.** A token signed with HMAC using the *RSA public key* as the shared secret must be rejected. This is the classic library bug: the verifier reads `alg: HS256`, looks up the key, and treats an RSA public key as an HMAC secret — and the public key is, by definition, public.

**A-3 — `kid` collision.** A token signed with an attacker's own key, carrying a `kid` that matches a legitimate one, must fail. The `kid` selects which key to try; it never confers trust.

**A-4 — Signature stripping.** A token with a valid header and payload but an empty or truncated signature must be rejected.

**A-5 — A retired key.** A token signed by a key since retired must fail verification, or retirement means nothing.

## 13. Logging and Audit

- Rotation writes an instance-level audit event (`P0-12` `WriteInstanceLevel`).
- The private key must never appear in a log line, an error, a metric label, or an API response. `P0-09`'s redaction covers key names; this package additionally never puts material in an error.
- A verification failure logs the `kid` and the reason, never the token.

## 14. Security Controls

- Algorithm pinned at verification (A-1, A-2).
- Private key material never leaves the process that resolved it; it is not stored in the struct that gets logged.
- RSA minimum 2048 bits.
- `kid` is derived from the public key thumbprint (RFC 7638), so it is stable, unpredictable, and cannot be chosen by an attacker to collide.

## 15. Testing Strategy

| Layer | What |
|---|---|
| Unit | State machine, JWKS shape, algorithm pinning, all five abuse cases |
| Integration | Rotation against a real database; a token issued before rotation still verifies after |
| Security | Abuse cases live in `tests/security/`, per `P0-15` |

The rotation test is the one that matters: sign, rotate, verify. If that passes and the abuse cases fail correctly, the task is done.

## 16. Acceptance Criteria

`P1-03`'s Definition of Done, unchanged. The one to watch is "restarting the service does not change the key set" — a service that generated a key at startup would pass every other test and break every consumer on every restart.

## 17. Implementation Sequence

1. `internal/signing`: key generation, PEM/JWK encoding, thumbprint `kid`.
2. Keyset loading from the database, private keys via `P0-14` references.
3. Signer (`current` only) and Verifier (any non-retired, pinned algorithm).
4. Cache with bounded TTL.
5. Rotation as a state transition, in one transaction, with an audit event.
6. `cmd/keyctl` for the operator.
7. Runbook, executed once against staging.

## 18. Rollback

Rotation is reversible while the old key is still `previous`: promote it back. Once retired, tokens it signed are dead — which is the point of retirement, and why the runbook keeps a key in `previous` for a full token lifetime plus margin before retiring it.

## 19. Technical Risks

**The overlap window is the whole design.** Getting it wrong is not subtle: too short and tokens die mid-session; absent and every rotation is an outage.

**A `kid` that is not stable across restarts** would break every consumer's cache. Deriving it from the key material rather than generating it randomly makes this structural.

**A cache TTL that is too long** delays a new key reaching all instances; too short and every token issuance hits the database. Bounded, and measured in minutes rather than seconds or hours.
