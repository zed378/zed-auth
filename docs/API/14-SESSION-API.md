# 14 - Session API (Sessions, MFA, Account Self-Service)

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P3-04, P3-09, P3-10, P3-12 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Document three related self/admin route groups that share one property — they act on a person, not a tenant-wide resource — and are grouped together because splitting them by resource name (`sessions` vs `mfa` vs `account`) would scatter the recent-authentication rule that governs several of them: `/v1/me/sessions*`, `/v1/me/mfa*` and the matching admin routes, and `/v1/me`/`/v1/me/password`.

## Scope

`/v1/me`, `/v1/me/password`, `/v1/me/mfa*`, `/v1/me/sessions*`, `/v1/organizations/{org_id}/users/{user_id}/mfa`, `/v1/organizations/{org_id}/users/{user_id}/mfa-reset`, `/v1/organizations/{org_id}/users/{user_id}/sessions*`. The MFA-mandate *impact preview* (`GET .../mfa-impact`) is organization settings, not session/MFA administration, and is documented in `10-ORGANIZATION-API.md`.

## As Built

### Caller's own account

**`GET /v1/me` exists so a client can show password requirements before a new one is typed, not only after it is refused.** It returns the caller's profile, their organization's name, and the password policy that applies to them (`MyPasswordPolicy`) — requires only a valid token; there is no role check, because the user comes from the token itself.

**`POST /v1/me/password` requires the current password even though the caller is already authenticated.** A live session is not proof that the person at the keyboard is the account's owner, and a stolen session that could change the password without it would let an attacker lock the real owner out. The current-password check shares the **same per-address brute-force bound as sign-in** — wrong guesses here count toward the same cooldown, so this is not a second, unthrottled way to guess a password; a refusal during a cooldown is `429`. On success, every **other** session is signed out and its refresh tokens revoked; the session the request came from is kept — a password change is what somebody does when they suspect somebody else knows it.

### Caller's own sessions

**`GET /v1/me/sessions` and its siblings act only on the caller's own sessions**, by construction — the user comes from the token and nothing in the request can name anyone else (`docs/PLAN/02-REQUIREMENTS.md` FR-5). `current` marks the session the caller's own token was issued through; a `client_credentials` token has no session and no entries.

**A session entry deliberately omits the IP address and the raw user agent, for the caller and for an administrator alike.** Only the browser family, the OS, and — when a geolocation database is configured — a city/country string. The audit log keeps the address for an investigation; a session list is not one.

**Ending a session is effective on the very next request, everywhere this service answers** (`/oauth/userinfo`, introspection, refresh, and this API) — including through the session cache. An application that validates access tokens purely locally keeps accepting an already-issued one until it expires (at most ten minutes); that is a property of stateless JWTs, not of this endpoint.

**`POST /v1/me/sessions/revoke-others` needs a token issued through an actual sign-in session.** A `client_credentials` token has no "current" session to keep and is refused with `400` rather than being interpreted as "end everything."

### A member's sessions (administrator)

**Same shape, `ORG_ADMIN` over the member's organization**, the same role level as deactivating the member — ending one session is strictly less than that. `current` is true only when the administrator is looking at their own sessions through this route.

### Caller's own MFA

**Every write here requires recent authentication — a session that authenticated within the last ten minutes — or `403 REAUTHENTICATION_REQUIRED`.** This applies to beginning a TOTP enrolment, confirming one, removing a factor, and regenerating recovery codes. A stolen session must not be able to add or remove a second factor on its own authority; that is exactly how a takeover becomes permanent and MFA-protected. A token with no session (`client_credentials`) can never manage factors at all. The client's remedy for `REAUTHENTICATION_REQUIRED` is to send the user through sign-in again (`prompt=login`); this is a distinct error code from `PERMISSION_DENIED` specifically so a client does not tell a user they may not manage their own account when the real answer is "sign in again first."

**The TOTP secret is returned exactly once, at the moment enrolment begins**, and never again in any form — the factor verifies nothing until a code confirms it. One authenticator app per user: attempting to begin a second while one is already active is `409`; an unconfirmed earlier enrolment is simply replaced by a new one.

**Recovery codes are issued once, shown once, and regenerating them invalidates every earlier code — used or not — in a single statement.** Regenerating requires at least one active factor (`409` otherwise: a recovery code recovers access past a second factor, and without one there is nothing to recover past).

**Removing a factor is refused with `409` when it is the caller's last one and their organization requires MFA** — the next sign-in would have nothing to challenge with and would route the user straight back into enrolment. In an organization that does not require MFA, removing the last factor is allowed.

### A member's MFA (administrator)

**Read-only, by design.** `GET /v1/organizations/{org_id}/users/{user_id}/mfa` shows an `ORG_ADMIN` a member's factor types, labels, and dates — never a secret, credential id, or public key — and never lets the administrator enrol or remove a factor on the member's behalf. An administrator who could add a factor to somebody else's account could sign in as them; the only lever available is `mfa-reset`, a strictly destructive operation with no way to add a credential.

**`POST .../mfa-reset` destroys credentials and mints none.** It removes every enrolled factor and every recovery code, returning the user to "password alone signs them in" so they can enrol again — and returns **only counts** (`factors_removed`, `recovery_codes_removed`), never anything that could be used as a credential. This is explicitly the last resort, not the first: `deploy/RUNBOOK-mfa-recovery.md` documents the identity verification an administrator must perform out-of-band *before* calling this endpoint, and the endpoint itself has no way to enforce that verification happened — it can only refuse to hand back a working credential if it did not. Idempotent in effect: a user with nothing enrolled returns zero removed, not an error.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Recent-authentication window (MFA writes) | 10 minutes since the session's own authentication | `backend/internal/mfaapi/handler.go` |
| Session list fields | Device (browser family, OS), coarse location, `auth_methods`, timestamps, `current` — never IP or raw user agent | `openapi/openapi.yaml` `Session`, `SessionDevice` |
| Session revocation effect | Immediate on next request, including via session cache | `backend/internal/session/` |
| `POST /v1/me/sessions/revoke-others` without a session-backed token | `400` | `openapi/openapi.yaml` `revokeMyOtherSessions` |
| TOTP secret visibility | Returned once, at `beginMyTotpEnrolment` only | `openapi/openapi.yaml` `TotpEnrolment` |
| One active TOTP factor per user | Second attempt while one is active is `409` | `backend/internal/login/enrol.go` |
| Recovery codes | 10 single-use codes; regenerating invalidates all earlier codes; requires ≥1 active factor | `openapi/openapi.yaml` `RecoveryCodes` |
| Removing the last factor | `409` if the organization requires MFA; allowed otherwise | `openapi/openapi.yaml` `removeMyFactor` |
| Admin view of member MFA | Read-only; no enrol/remove on a member's behalf | `openapi/openapi.yaml` `getUserMfa` |
| Admin MFA reset | Destroys credentials, mints none; returns counts only | `openapi/openapi.yaml` `MfaReset`; `backend/internal/user/mfareset.go` |
| Password change | Requires current password; shares sign-in's brute-force bound; ends every other session | `openapi/openapi.yaml` `changeMyPassword` |
| Required role — self routes (`/v1/me*`) | none — `Member`/`ScopeSelf` | `backend/internal/management/policy.go` |
| Required role — admin routes (member MFA/sessions/reset) | `ORG_ADMIN` | `policy.go` |

## Interfaces

| Method & path | `operationId` | Required role |
|---|---|---|
| `GET /v1/me` | `getMe` | none |
| `POST /v1/me/password` | `changeMyPassword` | none |
| `GET /v1/me/mfa` | `getMyMfa` | none |
| `POST /v1/me/mfa/totp` | `beginMyTotpEnrolment` | none + recent auth |
| `POST /v1/me/mfa/totp/{factor_id}/confirm` | `confirmMyTotpEnrolment` | none |
| `DELETE /v1/me/mfa/factors/{factor_id}` | `removeMyFactor` | none + recent auth |
| `POST /v1/me/mfa/recovery-codes` | `regenerateMyRecoveryCodes` | none + recent auth |
| `GET /v1/me/sessions` | `listMySessions` | none |
| `DELETE /v1/me/sessions/{session_id}` | `revokeMySession` | none |
| `POST /v1/me/sessions/revoke-others` | `revokeMyOtherSessions` | none |
| `GET /v1/organizations/{org_id}/users/{user_id}/mfa` | `getUserMfa` | `ORG_ADMIN` |
| `POST /v1/organizations/{org_id}/users/{user_id}/mfa-reset` | `resetUserMfa` | `ORG_ADMIN` |
| `GET /v1/organizations/{org_id}/users/{user_id}/sessions` | `listUserSessions` | `ORG_ADMIN` |
| `DELETE .../sessions/{session_id}` | `revokeUserSession` | `ORG_ADMIN` |

Full schemas: `public-site/docs/api-reference/get-me.api.mdx`, `change-my-password.api.mdx`, `get-my-mfa.api.mdx`, `begin-my-totp-enrolment.api.mdx`, `confirm-my-totp-enrolment.api.mdx`, `remove-my-factor.api.mdx`, `regenerate-my-recovery-codes.api.mdx`, `list-my-sessions.api.mdx`, `revoke-my-session.api.mdx`, `revoke-my-other-sessions.api.mdx`, `get-user-mfa.api.mdx`, `reset-user-mfa.api.mdx`, `list-user-sessions.api.mdx`, `revoke-user-session.api.mdx`.

Idempotency: `Idempotency-Key` accepted on `changeMyPassword`, `beginMyTotpEnrolment`, `regenerateMyRecoveryCodes`, `revokeMyOtherSessions`, `resetUserMfa`.

Audit events: `user.password.changed`, `session.created`, `session.revoked`, `user.mfa.enrolment_started`, `user.mfa.enrolled`, `user.mfa.removed`, `user.mfa.codes_generated`, `user.mfa.reset_by_admin`, `user.mfa.success`/`user.mfa.failed`/`user.mfa.challenged` (written by the login path, not this API, but relevant to the same factors), `user.mfa.recovery_used`.

## Security Considerations

- The recent-authentication requirement on every MFA-mutating self-service route is the primary defence against a stolen access token making a takeover permanent by disabling the second factor — see `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`.
- `403 REAUTHENTICATION_REQUIRED` is a distinct code from `403 PERMISSION_DENIED` specifically so a client can tell "you may not" apart from "you may, but not right now" — see `04-ERROR-HANDLING.md`.
- Read-only admin access to a member's MFA, with reset as the only lever, is the control against an `ORG_ADMIN` using their role to add their own credential to somebody else's account.
- `deploy/RUNBOOK-mfa-recovery.md`'s out-of-band verification step is not, and cannot be, enforced by this endpoint — the endpoint's contribution is refusing to ever hand back a usable credential, which bounds the damage if the runbook step is skipped.

## Verification

- `backend/internal/account/account_integration_test.go`.
- `backend/internal/mfaapi/mfaapi_integration_test.go`.
- `backend/internal/sessionapi/sessionapi_integration_test.go`.
- `backend/internal/user/mfareset_integration_test.go`.

## Not Yet Built / Open Questions

None specific to this resource group.

## Related Documents

- `docs/API/09-USER-API.md`, `docs/API/10-ORGANIZATION-API.md` (`mfa-impact`)
- `docs/SESSION-MANAGEMENT/00-SESSION-ARCHITECTURE.md`
- `docs/IDENTITY-PROTOCOL/06-MULTI-FACTOR-AUTHENTICATION.md`
- `deploy/RUNBOOK-mfa-recovery.md`
