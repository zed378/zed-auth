# P1-13 — Login Rate Limiting and Account Lockout

Feature specification, per `docs/PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`. "Spec required — security control".

---

## 1. Business Objective

`docs/PLAN/17` § Phase 1 makes this an explicit acceptance criterion: **a simulated brute-force attempt is demonstrably blocked.**

The second half of the goal is the harder one. `docs/PLAN/05` says cooldown, **not permanent lockout**, because a permanent lockout converts a brute-force attempt into a denial of service against the victim — the attacker fails to guess the password and succeeds at locking the person out of their job, which is a cheaper win than the one they were going for.

## 2. Actors

| Actor | Interaction |
|---|---|
| An attacker guessing one account's password | Should be stopped after a few attempts, for a while |
| An attacker credential-stuffing many accounts from one host | Should be stopped by a different, looser bound |
| A user who mistypes their password twice | Should notice nothing |
| A user behind a corporate NAT | Shares an IP with hundreds of colleagues and must not be punished for it |
| An attacker who wants a specific user locked out | Must not be able to achieve it |

## 3. Functional Requirements

- **FR-1** Per-account limiting with a **cooldown**, self-clearing, never permanent.
- **FR-2** Per-IP limiting with a different, looser threshold.
- **FR-3** Progressive: the first few failures are free, then an increasing cooldown.
- **FR-4** Counters in Redis with automatic expiry.
- **FR-5** A reasoned, recorded decision about Redis being unavailable.
- **FR-6** Not bypassable by rotating a header.
- **FR-7** `X-RateLimit-*` headers where appropriate, but never in a way that helps an attacker calibrate their pacing on the login endpoint.
- **FR-8** Lockouts audited and counted.
- **FR-9** Counters reset on successful authentication.

## 4. Non-Functional Requirements

- **NFR-1** The check costs one Redis round trip, before the Argon2 computation — the point is to avoid the expensive work.
- **NFR-2** No address is written to a durable store. Redis keys expire; the audit log does not.

## 5. Dependencies

| Depends on | Why |
|---|---|
| `P1-12` | The login handler is where this is enforced |
| `P1-01` | Argon2's cost is the floor that makes the fail-open decision tolerable |
| `P0-12` | Audit |

## 6. Database Changes

None. Counters live in Redis, per FR-4.

## 7. API Contract

No new endpoint. `POST /login` gains a refusal path.

## 8. Frontend Changes

None beyond the login page's existing message.

## 9. Backend Changes

New package `internal/ratelimit`: a Redis-backed counter with a progressive cooldown, and the decision function. The login handler consults it before verifying a password and resets it after a success.

`config` gains a client-IP source (§11).

## 10. The key that is counted, and why it is the submitted address

**Per-account counters are keyed on the NORMALISED SUBMITTED ADDRESS, not on a resolved user id.**

That single choice is what makes the whole feature safe to be honest about. If the counter were keyed on a user id, it would only exist for addresses that have accounts — so a "too many attempts" response would confirm the account exists, and the enumeration defence `P1-12` spends its whole design on would be undone by its own rate limiter.

Keyed on the submitted address, the counter exists for `nobody@example.test` exactly as it does for a real user. A refusal then tells an attacker only about **their own behaviour**, which they already know, because they are the one who produced it.

This is what lets the refusal say something true and useful — "too many attempts, try again in a few minutes" — instead of forcing the uniform credential message onto a user who has done nothing wrong and would otherwise keep retrying.

## 11. The client IP, and a deployment problem this task must not paper over

FR-6 says the limit must not be bypassable by rotating a header. `P1-12` already refuses to read `X-Forwarded-For` at all and uses `RemoteAddr`.

**On the current staging deployment that is correct and useless.** `cloudflared` runs as a host service and reaches the published port, so `RemoteAddr` is the Docker gateway — **the same address for every user in the world**. A per-IP limiter computed from it is not a per-IP limiter; it is a global one, and a single attacker could use it to lock every user out of the service. That is a denial of service delivered by the security control.

So the client IP needs a configured source:

```
AUTH_CLIENT_IP_HEADER    e.g. CF-Connecting-IP.  Empty (default) = use RemoteAddr.
AUTH_TRUSTED_PROXY_CIDRS e.g. 172.16.0.0/12.     The peers whose header is believed.
```

The header is read **only** when the immediate peer is inside a trusted CIDR. That is what makes it unforgeable: a client that sets `CF-Connecting-IP` itself is not connecting from a trusted proxy, so its header is ignored. Both must be configured; either alone falls back to `RemoteAddr`.

**When no source is configured, the service warns loudly at startup** that per-IP limiting will treat every request as one client if anything is proxying in front of it. It does not silently disable itself: a limiter that quietly turns off is worse than one that is loudly misconfigured.

## 12. Thresholds

| Bound | Free attempts | Then | Cap |
|---|---|---|---|
| Per address | 5 in 15 minutes | cooldown doubling from 1 minute | 15 minutes |
| Per IP | 50 in 15 minutes | cooldown doubling from 1 minute | 15 minutes |

Per-address 5 is chosen against a person who mistypes: three attempts is a normal bad morning, five is unusual, and the cost of being wrong is a one-minute wait rather than a lockout.

Per-IP 50 is ten times that, because one IP legitimately serves many users behind NAT. It is deliberately not a strong control — its job is to make a wide credential-stuffing run expensive, not to be the thing that stops a targeted attack. The per-address bound is what does that.

The cooldown **doubles per consecutive cooldown** and resets when a login succeeds, so a determined attacker reaches the fifteen-minute cap in four rounds while a user who eventually remembers their password pays a minute once.

## 13. Error Handling

A refusal is `200` re-rendering the login form with a distinct message, **not** `429`.

`429` would be more correct HTTP and is the wrong answer here, because the login page is consumed by a browser, not a client library: a status the browser renders as an error page loses the form, the CSRF token and the pending request. The message is what the user needs; the status is what a machine would need, and no machine posts this form.

The refusal happens **before** the password is verified, so a rate-limited attempt costs no Argon2 computation. That is the point of the feature — but it means the refusal path is measurably faster than a real attempt, which is a timing signal about the limiter's own state. It discloses nothing, because the attacker produced that state.

## 14. Abuse Cases

| # | Abuse case | Source | Control |
|---|---|---|---|
| A-1 | Brute-forcing one account | `docs/SECURITY/02` §10 | Per-address cooldown |
| A-2 | Credential stuffing across many accounts | `docs/SECURITY/02` §13 | Per-IP bound |
| A-3 | **Locking a victim out deliberately** | `docs/PLAN/05` | Cooldown is minutes and self-clearing; there is no permanent state to reach |
| A-4 | Rotating `X-Forwarded-For` to reset the count | `docs/SECURITY/02` §10 | The header is read only from a trusted peer, and the per-address bound does not use the IP at all |
| A-5 | Using the refusal to enumerate accounts | `docs/SECURITY/02` §12 | The counter is keyed on the submitted address, so it exists for addresses that have no account |
| A-6 | Taking Redis down to disable the limiter | — | §16, and it is an accepted cost with a named floor |
| A-7 | Filling Redis with counters for invented addresses | — | Keys expire; the address is bounded before it becomes one |

## 15. Logging / Audit

| Event | When |
|---|---|
| `user.lockout` | The first refusal of a cooldown, not every attempt during it |

Auditing every refused attempt during a cooldown would let an attacker write to an append-only table as fast as they can send requests. One entry per cooldown, with the reason class and **not the address**, for the reason `P1-12` gives about not turning the audit log into a list of addresses somebody tried.

`auth_login_attempts_total{outcome="rate_limited"}` extends the existing metric rather than adding one.

## 16. Redis unavailable — the decision FR-5 asks for

**Fail open, loudly.** Recorded as an ADR.

Failing closed means a Redis outage stops every login in the estate. Redis already backs the session cache, so an outage is already degrading; turning it into "nobody can authenticate at all" converts a cache failure into a total authentication outage, and the blast radius is every user of every application.

Failing open means an attacker who can take Redis down gets unlimited attempts. What makes that tolerable is a floor that does not depend on Redis: **Argon2id at the current parameters costs ~50-100ms of CPU per attempt** (`P1-01`), so the service's own CPU bounds an attacker to a rate far below what an offline attack achieves. The limiter raises that floor; it is not the only thing holding it.

It fails **loudly**: a `WARN` per occurrence naming the failure, and a metric, so an outage that silently removed the control is not possible.

**Considered and rejected: an in-process fallback limiter.** Per-instance memory would degrade gracefully rather than open, and with one instance today it would be equivalent to the real thing. It was rejected because it is a second code path that only executes during a Redis outage — so it would be exercised by nothing except the incident it exists for, which is the shape of code that turns out not to work when it finally runs. `P3-xx` may revisit it when there is more than one instance and the difference starts to matter.

## 17. Testing Strategy

| Layer | Coverage |
|---|---|
| Unit | The progression: free attempts, then a growing cooldown, then the cap |
| Unit | A success resets the counter |
| Unit | The decision is a pure function of (count, last cooldown, now) |
| Integration | **A simulated brute-force is blocked** — the literal acceptance criterion |
| Integration | The cooldown clears on its own, with no administrative action |
| Integration | An address with **no account** is rate-limited identically |
| Integration | Rotating `X-Forwarded-For` does not reset the count |
| Integration | A refused attempt performs **no** Argon2 computation (measured) |
| Integration | The lockout is audited once per cooldown, not once per attempt |
| Integration | Redis unavailable → logins still work, and the failure is counted |

## 18. Acceptance Criteria

1. A simulated brute-force attempt is demonstrably blocked.
2. Lockout is temporary and self-clearing; no administrative action restores a user.
3. Rotating `X-Forwarded-For` does not reset the limit.
4. The Redis-unavailable decision is in `MEMORY/DECISIONS.md`.
5. Lockouts appear in the audit log and in metrics.

## 19. Implementation Sequence

1. The pure decision function and its progression tests.
2. The Redis store.
3. The client-IP source and its configuration.
4. Wiring into the login handler, before the password check.
5. Audit, metric, ADR.

## 20. Rollback Strategy

No schema change. Rolling back removes the limiter; Argon2's cost floor remains.

## 21. Technical Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| The per-IP limiter locks everyone out behind a proxy | **High if unconfigured** | High | §11: a configured source, a loud startup warning, and a deployment note |
| The counter is later keyed on a user id | Medium | High | The test that an address with no account is limited identically |
| The check moves after the password verification | Low | Medium | The test that a refused attempt performs no Argon2 work |
| Fail-open hides a Redis outage | Medium | Medium | A metric and a WARN per occurrence |
