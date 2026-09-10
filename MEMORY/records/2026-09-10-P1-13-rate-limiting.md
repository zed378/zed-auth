# P1-13 — Login rate limiting and account lockout

**Date**: 2026-09-10
**Branch**: `feat/P1-13-rate-limiting`
**Spec**: [`MEMORY/specs/P1-13-rate-limiting.md`](../specs/P1-13-rate-limiting.md)
**Decision**: [ADR-017](../DECISIONS.md#adr-017--the-rate-limiter-fails-open-when-redis-is-unavailable-and-says-so-every-time)

---

## What this is

`docs/PLAN/17` § Phase 1 makes it an explicit acceptance criterion: a simulated brute-force attempt is demonstrably blocked. `docs/PLAN/05` constrains the answer: a **cooldown**, never a permanent lockout, because a permanent lockout converts a brute-force attempt into a denial of service against the victim — the attacker fails to guess the password and succeeds at locking somebody out of their job, which is a cheaper win than the one they came for.

## The choice everything else rests on: what gets counted

**Per-account counters are keyed on the NORMALISED SUBMITTED ADDRESS, not on a resolved user id.**

If the counter were keyed on a user id it would only exist for addresses that have accounts — so a "too many attempts" response would confirm the account exists, and the enumeration defence `P1-12` spends its entire design on would be undone by its own rate limiter.

Keyed on the submitted address, the counter exists for `nobody@example.test` exactly as it does for a real user. A refusal then tells an attacker only about **their own behaviour**, which they already know, because they produced it.

That is what makes it safe for the login page to say something **true and specific** here — "too many sign-in attempts, please wait a few minutes" — when every other refusal on that page is deliberately vague. Being vague here would be worse than useless: a user told "your email or password is incorrect" while actually in a cooldown will keep retrying, which is a worse experience and more load.

The property has its own integration test: an address with no account is rate-limited identically, and a counter exists for it.

## The deployment problem this could not paper over

FR-6 says the limit must not be bypassable by rotating a header, and `P1-12` had already answered that by refusing to read `X-Forwarded-For` at all and using `RemoteAddr`.

**On this service's own staging deployment that is correct and useless.** `cloudflared` runs as a host service and reaches the published port, so `RemoteAddr` is the Docker gateway — the same address for every user in the world. A per-IP limiter computed from it is not a per-IP limiter; it is a **global** one, and a single attacker could use it to lock every user out of the service. That is a denial of service delivered by the security control.

So the client IP got a configured source, and it is deliberately two settings rather than one:

```
AUTH_CLIENT_IP_HEADER      e.g. CF-Connecting-IP
AUTH_TRUSTED_PROXY_CIDRS   e.g. 172.16.0.0/12
```

The header is read **only** when the immediate peer is inside a trusted range. That is what makes it unforgeable: a client that sets `CF-Connecting-IP` itself is not connecting from a trusted proxy, so its header is ignored. **Both or neither** — a header believed from anywhere is a header anybody can write, and trusted peers with no header have nothing to read.

Two smaller decisions inside that:

- **A list is refused, not parsed.** A header a trusted proxy sets carries one address; a list means something appended to it, and picking an element out of a list somebody else can extend is how the bypass works. It falls back to the peer, which is at worst the shared address it would have used anyway.
- **A bad CIDR is reported at startup, not dropped.** Silently narrowing the trusted set to nothing looks identical to working and would quietly turn every user into one IP.

**With nothing configured the service warns loudly and does not disable itself.** A limiter that quietly turns off is worse than one that is loudly misconfigured, because the second gets fixed. `BL-05` tracks setting it on the VM.

## Redis unavailable — ADR-017

**Fail open, loudly.** Failing closed would stop every login in the estate, and Redis already backs the session cache, so an outage is already degrading — turning it into "nobody can authenticate at all" converts a cache failure into a total authentication outage. It also hands anybody who can make Redis unreachable a bigger win than the brute force the limiter exists to stop.

What makes it tolerable is a floor that does not depend on Redis: **Argon2id at 64 MiB / t=3 / p=4 costs 50-100ms of CPU per attempt** (`P1-01`). An attacker with no limiter is bounded by the service's own CPU to a few hundred attempts per second per core, against a twelve-character minimum and a breach check. The limiter raises that floor; it is not the only thing holding it.

`auth_rate_limit_unavailable_total` is the metric that makes the choice safe to have made. Without it an outage would silently remove a security control and look exactly like nothing happening — `P0-11`'s lesson about alerting on the absence of a signal, arriving in a third place.

An in-process fallback limiter was considered and rejected: it is a second code path that executes only during a Redis outage, so it would be exercised by nothing except the incident it exists for. Worth revisiting when there is more than one instance.

## What the mutation testing found

Thirteen mutations. Ten confirmed their test immediately; three did not, and two of those were my own error. The third was a real defect.

| Control removed | Test that failed |
|---|---|
| knocking during a cooldown extends it | `TestKnockingDuringACooldownDoesNotExtendIt` |
| the cooldown is uncapped | `TestTheCooldownGrowsAndIsCapped` |
| the free allowance is off by one | `TestTheAllowanceIsExactlyFree` |
| the address key is not lowercased | `TestTheAddressKeyIsNormalised` |
| a forwarding header is believed from any peer | `TestAnUntrustedPeersHeaderIsIgnored` |
| a forwarded list is parsed instead of refused | `TestAListIsRefusedRatherThanParsed` |
| the limit is checked after the password | `TestARefusedAttemptNeverReachesThePassword` |
| a lockout is audited on every attempt | `TestALockoutIsAuditedOncePerCooldown` |
| the limiter fails closed | `TestLoginsProceedWhenTheCounterStoreIsUnavailable` |
| a success clears the per-IP counter too | `TestASuccessfulLoginClearsTheAddressCounter` |

### The mutation that found dead code pretending to be a control

Flipping `<` to `<=` in `Evaluate`'s free-allowance check changed nothing. Looking at why:

```go
if s.Failures < p.Free {
    return Decision{Allowed: true}
}
return Decision{Allowed: true}
```

**Both branches returned the same thing.** The comparison read like the allowance check — it had a comment explaining the off-by-one reasoning — and decided nothing at all. The allowance is enforced in `Next`, which is where it belongs, and the branch in `Evaluate` was residue from an earlier shape.

That is worth more than the mutation it came from. A reader auditing this file would have found a plausible-looking bound in the function named `Evaluate` and stopped there, never noticing that the real one is elsewhere and could be changed without touching what they read. It is now deleted, and the comment in its place says what actually decides and why the attempt that spends the last free failure must still be tried.

The two mistakes of my own are worth naming too, because both are the same shape: **a mutation that does not remove the control proves nothing about the test.** Removing one of two cap checks left the cap in place; changing `Evaluate`'s comparison left `Next` in charge. Neither was a weak test.

## What is deliberately not done

**No `X-RateLimit-*` headers.** Step 7 asks for them "where appropriate, but not in a way that helps an attacker calibrate their pacing on the login endpoint" — and the login endpoint is the only thing this task limits. Emitting them there would tell an attacker exactly how many attempts remain and when the window resets, which is a calibration aid rather than a courtesy. They belong with `P1-15`'s `/v1` limiter, where the caller is a machine that needs them and is authenticated.

**`429` is not used.** The login page is consumed by a browser, not a client library: a status the browser renders as an error page loses the form, the CSRF token and the pending request. The message is what the user needs; the status is what a machine would need, and no machine posts this form.

**The refusal is faster than a real attempt**, because it happens before the Argon2 computation — which is the entire point. That is a timing signal about the limiter's own state and it discloses nothing, because the attacker produced that state.

## Verification

- `internal/ratelimit` at 89.7% with a floor added at 80%.
- The acceptance criterion has a test that spells it out: consecutive wrong passwords are refused, **and the correct password is refused too while the cooldown runs** — a limiter an attacker can step past by guessing right is not one.
- The cooldown clears with no administrative action, no unlock endpoint and no cleanup job: the key's TTL is the whole mechanism.
- Rotating the client IP every single attempt does not reset the per-address bound, and an untrusted peer cannot choose its own identity at all.

## What is still missing

- **`AUTH_CLIENT_IP_HEADER` is not set on staging**, so per-IP limiting there currently treats every request as one client. `BL-05`. The per-address bound — the one that stops a targeted attack — is unaffected.
- **Nothing alerts on `auth_rate_limit_unavailable_total`**, and ADR-017's consequences say it must. Recorded with `BL-01`, which is the same shape of gap.
- **`P1-14`** is what turns these events into the authentication audit trail proper.
