# P3-09 — Session Management API

| | |
|---|---|
| **Date** | 2026-09-13 |
| **Task** | `TASKS/PHASE-3-ADVANCED-SECURITY.md` § P3-09 |
| **Phase** | Phase 3 — Advanced Security |
| **Surface** | backend (Management API) |
| **Branch** | `feat/P3-09-session-management-api` |
| **Status** | Complete |

**Spec**: [`MEMORY/specs/P3-09-session-management-api.md`](../specs/P3-09-session-management-api.md)

---

## Five routes, and the one line that differs

```
GET    /v1/me/sessions                          any valid token
DELETE /v1/me/sessions/{session_id}             any valid token
POST   /v1/me/sessions/revoke-others            a token with a session
GET    /v1/organizations/{org_id}/users/{user_id}/sessions               ORG_ADMIN
DELETE /v1/organizations/{org_id}/users/{user_id}/sessions/{session_id}  ORG_ADMIN
```

Both entry points share every piece of behaviour except where the user id comes
from: the token, or the path under an administrator's scope. That difference is
the security property, so it is the only thing each decides for itself. A
session id is always resolved **together with** its owner in one statement, so
another person's session is the same `404` as a session that does not exist.

`docs/PLAN/05` lists only the organization path. The self-service one follows
`P2-13`'s `/v1/me/organizations` — a route about the caller that needs no role
because it cannot describe anybody else.

## What the list leaves out

No IP address and no user agent string, for the user or the administrator. The
browser family and operating system are enough to recognise "my phone"; the city
is enough to recognise "not me". The address stays in the audit log, where an
investigation needs it. The device and place come from `P3-08`'s parser and
locator, so the sessions screen and the anomaly notice describe a login the same
way — and writing the test for the place found the API returning `jakarta|id`,
`P3-08`'s comparison key, instead of `Jakarta, ID`. `Location.Describe` is now the
one display form.

## Three bugs found beside the work

**1. Every successful revocation would have been reported as unaudited.** The
first version audited inside `session.Manager`, the way `Revoke` does. The
Management API's `AuditGuard` only sees events written through
`management.Audit`, so every DELETE would have logged an ERROR and incremented a
metric documented as permanently zero — and the events would have carried no
request id and no client address. The handler now records the revocation in the
same transaction, so one that cannot be recorded does not happen (tested: the
session and its refresh token survive a failed audit write).

**2. Correct no-ops were already tripping the same guard.** A profile update
that changes nothing, a project renamed to its own name, and a second
deactivation all rightly write no event — and every one was reported as an
unaudited mutation. A signal that fires on correct behaviour is a signal people
learn to ignore, which is the worst thing that can happen to one whose whole
point is that any value is a defect. `management.Unchanged` declares a no-op on
the branch that has established it; the guard still reports anything that
neither audited nor declared.

**3. The session cache defeated its own write throttle.** On a database read,
`Lookup` cached the row as read — with its old `last_seen_at` — and then recorded
the activity. Every cache hit for the next minute still looked a minute stale,
so every one wrote again: a database write per request for a minute at a time,
which `touchInterval` exists to prevent. This task is the first to show that
column to people, which is why it was looked at. The cached copy now carries the
activity being recorded; a sentinel test proves a cache hit no longer writes.

## Immediacy, stated precisely

A revoked session is refused on the next request through everything this service
answers: the session cookie (through the warmed cache), the Management API (its
`sid` check), userinfo, introspection and refresh. Refresh tokens issued through
the session are revoked in the same transaction, for every client — unlike a
single application's logout, which must not cost other applications theirs.

An access token already issued to a consumer that validates JWTs locally stays
valid there until it expires, at most ten minutes. That is inherent to JWTs, the
same window deactivation has, and the contract says so rather than implying it
away.

## The mutation run

Twenty-three controls; twenty-one red on the first run. One anchor had been
realigned by `gofmt`. The other was a real weakness: the "refuse an empty keep"
guard survived because Postgres also errors on `''` as a uuid, so a test
asserting only "an error" could not tell a deliberate refusal from a type
accident a schema change could remove. It now asserts the refusal by name.

## Verified

| | |
|---|---|
| Unit | The caller carries the token's `sid`; a declared no-op is not reported; every route has a declared permission |
| Integration | 16 through the real `/v1` chain against Postgres and Redis — own list and paging, coarse location, no IP or raw agent in the body, cross-user and cross-tenant refusals, a member refused the admin route, immediacy through a warmed cache, refresh revocation, the current token ending with its session, revoke-others keeping exactly one, the no-`sid` refusal, audit actor/target/reason, rollback when auditing fails. 7 more on the session store, 1 on the guard in the user API |
| Mutation | 23 controls, each turning its own test red |
| Contract | `openapi.yaml`, generated server, console client and API reference regenerated |

## What this does not deliver

- **The console screens** — `P3-11` and `P3-12`.
- **A hierarchy check on the administrator's route.** An `ORG_ADMIN` can end an
  `ORG_OWNER`'s session, exactly as an `ORG_ADMIN` can already deactivate one.
  Consistent with the existing user operations rather than a new rule; if the
  hierarchy should constrain user operations, it should constrain all of them.

Not yet on staging: the VM at 10.1.200.13 has been unreachable since the deploy
key was lost with a session scratchpad.
