# 06 - Multi-Factor Authentication

> Category: **Identity Protocol** (`docs/IDENTITY-PROTOCOL/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P3-01…P3-14 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describes the MFA framework: the two factor types this build implements (TOTP, WebAuthn), recovery codes, the organization-wide mandate with its 14-day grace period, and every point in the system where that mandate is actually enforced.

## Scope

Covers `backend/internal/mfa/`, `backend/internal/mfaapi/`, `backend/internal/authn/loginpolicy.go` and `mandatecheck.go`, and the MFA-specific parts of `backend/internal/login/`. WebAuthn ceremony mechanics are `05-WEBAUTHN-AND-PASSKEYS.md`. Session `amr` claim construction is `docs/SESSION-MANAGEMENT/01-JWT-ISSUANCE-AND-STRUCTURE.md`.

## As Built

### Framework shape

`mfa.Framework` (`backend/internal/mfa/framework.go`) is the single decision point both factor implementations plug into, so there are not two challenge steps, two rate limits, or two audit shapes. It answers two distinct questions, asked at different moments:

- **`Required`** — before any session exists: "must this login present a factor?" Returns no challenge at all if the user holds no confirmed, answerable factor (whether the *organization* may **demand** one is a separate question — the mandate, below).
- **`StepUp`** (a pure function, `sessionMethods` vs `required`, a strict subset test) — reports whether a *live* session already satisfies a step-up requirement. **This function exists and is tested but has no caller anywhere in this codebase outside its own tests** — no endpoint currently requests step-up authentication for a sensitive action.

### Factor types

| Type | `amr` value | Mechanism |
|---|---|---|
| `totp` | `otp` | RFC 6238 TOTP, hand-written (`backend/internal/mfa/totp.go`) — deliberately not a dependency, since it is thirty frozen lines (RFC 6238 is from 2011) and a supply-chain compromise of a TOTP library is a compromise of every second factor at once |
| `webauthn` | `hwk` | Library-based (`go-webauthn/webauthn`) — see `05-WEBAUTHN-AND-PASSKEYS.md` |

TOTP parameters: 6 digits, 30-second period, **±1 step** clock skew tolerance (not ±2 — every extra step widens the window a shoulder-surfed or phished code still works), SHA-1 (RFC 6238's default and what every authenticator app implements; SHA-1's *collision* weakness is irrelevant to HMAC-SHA1's security here), 160-bit (20-byte) generated secret.

`mfa.AuthMethods`/`AuthMethodsWithRecovery` build the session's `amr` from **what was actually used**, never from what is merely enrolled. `mfa` (RFC 8176, "multiple factors") is emitted only when two distinct factor *categories* were used — two passkeys is one category used twice, not `mfa`. A recovery-code login gets `mfa` (something known plus something held) but **never a possession-factor value like `otp`** — a consumer step-up check demanding `otp` correctly fails a recovery-code session, since the user no longer has the device the mandate exists to require.

### Recovery codes

10 codes per batch, 80 bits of entropy each, hashed with unsalted SHA-256 — a deliberate deviation from Argon2id (documented as `PG-39`): the input already has 80 bits of entropy and no candidate list to defend against, so a slow KDF here would only be a denial-of-service surface an attacker holding a valid password could drive at ten memory-hard computations per attempt. Codes are case-insensitive, separator-insensitive, and correct four specific, unambiguous misreadings (`0→O`, `1→I`, `8→B`, `9→G`); `2`/`Z` and `5`/`S` are deliberately left uncorrected because both members of each pair are valid codes and "correcting" either would risk spending the wrong one. Single-use (`Spend`), enforced by consumption in the same store call that checks validity. Low-water warning at 3 remaining. Recovery codes are issued **with the first confirmed factor**, not before — a user forced through enrollment with no recovery codes would have no way back if they lost the device immediately.

### The organization mandate and its 14-day grace

`authn.LoginPolicy.MFARequired` plus `MFARequiredSince` (from `organizations.settings`) drive `authn.RequireMFA`, a pure truth-table function:

| `MFARequired` | Has a confirmed factor | Grace status | Outcome |
|---|---|---|---|
| false | — | — | `MFANotRequired` |
| true | true | — | `MFANotRequired` |
| true | false | `MFARequiredSince` unset (pre-dates this field) or within 14 days | `MFAInGrace` — signed in, and told |
| true | false | 14 days elapsed | `MFAEnrolmentRequired` — no session until enrolled |

The grace period is **`authn.MFAGracePeriod` = 14 days**, measured from `MFARequiredSince` (when the mandate was activated), **never from a per-user last login** — measuring per-user would let a user who never signs in sit outside the policy forever, and would make "when does this take effect" unanswerable for an administrator.

### Where the mandate is enforced

The specification for forced enrollment (`P3-07`) named "the refresh grant re-reads the policy" as a required control, and initially nothing implemented it — this gap, and its fix, is recorded directly in `MEMORY/records/2026-09-15-P3-14-test-suite.md`. As of `P3-14`, `authn.MandateCheck` applies the identical `RequireMFA` rule at **every** issuing path, so no path can disagree with the others about who is past the grace:

| Path | Enforcement point | Outcome when mandate is unmet |
|---|---|---|
| Password sign-in | `login.Handler.authenticate` (`backend/internal/login/handler.go`) | Routed into forced enrollment (`/login/mfa/enrol`) |
| Silent `/oauth/authorize` | `authorize.Handler.session`, via the `Mandate` interface | Session treated as absent; a fresh interactive login is required |
| `refresh_token` grant | `token.Handler.refreshToken`, via the `Mandate` interface | Refused with `invalid_grant`, identical to any dead token; the refresh family is **not** revoked, since nothing is suspect — the user simply signs in again |

A mandate that cannot be read (a database error) **fails closed** on every path — refusing rather than silently disabling the policy, because failing open would let one query's outage switch the mandate off for the whole deployment. A deployment with no factor implementation wired (`Enroller == nil`) refuses **nothing** and logs at `ERROR` instead of locking every user out — forcing enrollment onto a build that cannot enroll anybody would be a lockout with no way forward.

### Enrollment flows

- **Self-service** (Management API, `backend/internal/mfaapi/handler.go`): `POST /v1/me/mfa/totp` begins, `POST /v1/me/mfa/totp/{factor_id}/confirm` proves it (bounded by the same per-user attempt counter the login challenge uses), `DELETE /v1/me/mfa/factors/{factor_id}` removes one, `POST /v1/me/mfa/recovery-codes` regenerates. Every write except confirmation requires **recent authentication** (`mfa.RecentAuthentication`, 10 minutes). Removing a user's **last** active factor while the mandate is on is refused outright, with a message explaining why, rather than allowed and discovered as a lockout at the next login.
- **Forced, at login** (`P3-07`): when the password step resolves `MFAEnrolmentRequired`, the login is diverted to `/login/mfa/enrol` (15-minute TTL — three times the ordinary challenge's, since setting up an authenticator app takes longer than reading six digits off one). No session exists until the user confirms a code; recovery codes are issued at this point if the user holds none (`P3-12`).
- **Administrator reset** (`POST /v1/organizations/{org_id}/users/{user_id}/mfa-reset`): the **only** administrator write on another user's factors, and it only ever removes — an administrator cannot add a factor to somebody else's account, because that would be equivalent to being able to sign in as them. Audited at elevated visibility.
- **Impact preview** (`GET /v1/organizations/{org_id}/mfa-impact`, `backend/internal/organization/mfaimpact.go`): reports counts only (`members`, `without_factor`, `mfa_required`, `grace_ends_at`) — deliberately never names which members lack a factor, since that list is exactly what an attacker holding a stolen administrator token would want.

### Attempt bounding

A single per-user counter (`mfa.MaxAttemptsPerWindow` = 10 per window; a single challenge additionally self-destructs after `mfa.MaxAttempts` = 5 guesses) is shared across **TOTP guesses, WebAuthn assertion failures, and recovery-code guesses** — one bound, not one per method, because separate bounds would let an attacker get the full guess allowance against each method independently. The bound is checked *before* the verifier runs, so a refused attempt costs no cryptographic work.

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| MFA grace period | 14 days from `MFARequiredSince` | `authn.MFAGracePeriod` |
| TOTP digits / period / skew | 6 / 30s / ±1 step | `mfa.TOTPDigits`, `TOTPPeriod`, `TOTPSkew` |
| Recovery codes per batch / entropy | 10 / 80 bits | `mfa.RecoveryCodeCount`, `RecoveryCodeBytes` |
| Recovery code hash | Unsalted SHA-256 (not Argon2) | `mfa.HashRecoveryCode`, rationale `PG-39` |
| Recovery low-water mark | 3 remaining | `mfa.RecoveryLowWaterMark` |
| Recent-authentication window | 10 minutes | `mfa.RecentAuthentication` |
| Forced-enrollment TTL | 15 minutes | `login.EnrolmentTTL` |
| Ordinary challenge TTL | 5 minutes | `mfa.ChallengeTTL` |
| Per-challenge attempt cap | 5 | `mfa.MaxAttempts` |
| Per-user attempt window cap | 10 per window | `mfa.MaxAttemptsPerWindow` |
| Mandate check failure mode | Fail closed | `authn.MandateCheck.Unmet` |
| Unenrollable build failure mode | Fail open (log `ERROR`, no lockout) | `login.Handler.authenticate` |

## Interfaces

| Method | Path | operationId | Notes |
|---|---|---|---|
| GET | `/v1/me/mfa` | `getMyMfa` | Own factors, `available_types`, recovery codes remaining, mandate/grace deadline |
| POST | `/v1/me/mfa/totp` | `beginMyTotpEnrolment` | Requires recent authentication |
| POST | `/v1/me/mfa/totp/{factor_id}/confirm` | `confirmMyTotpEnrolment` | Issues recovery codes on first confirmed factor |
| DELETE | `/v1/me/mfa/factors/{factor_id}` | `removeMyFactor` | Refuses removing the last factor under an active mandate |
| POST | `/v1/me/mfa/recovery-codes` | `regenerateMyRecoveryCodes` | Requires an active factor and recent authentication |
| GET | `/v1/organizations/{org_id}/users/{user_id}/mfa` | `getUserMfa` | Administrator, read-only |
| POST | `/v1/organizations/{org_id}/users/{user_id}/mfa-reset` | `resetUserMfa` | Administrator, removal only |
| GET | `/v1/organizations/{org_id}/mfa-impact` | `getMfaImpact` | Counts only, never member identities |
| GET/POST | `/login/mfa` | — (hosted page) | Login-time challenge |
| POST | `/login/mfa/webauthn` | — (hosted page) | Login-time passkey assertion |
| GET/POST | `/login/mfa/enrol` | — (hosted page) | Forced enrollment |

Audit events: `mfa.challenged`, `mfa.succeeded`, `mfa.failed`, `mfa.enrolled`, `mfa.enrolment_started`, `mfa.enrolment_forced`, `mfa.removed`, `mfa.codes_issued`, `mfa.recovery_used` (elevated visibility).

## Security Considerations

- **A6/A2-class attacks** (answering a challenge with another user's factor id, or a challenge issued for one login completing a different one) are closed structurally: the challenge is the authority on the user, and every `AnswerType`/`AnswerRecovery`/`AnswerWebAuthn` call checks the pending authorization request id matches before verifying anything.
- **Uniform failure responses**: a wrong code, a factor type the user does not hold, and a challenge that has expired are all answered with the same browser-visible message; only the audit log distinguishes them.
- **Recovery-code brute force** shares the same per-user bound as TOTP guesses (`docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §9).
- **`mfa-impact` counts, never names** — see As Built.

## Verification

| Test | File |
|---|---|
| `TestTheMandateDecidesEveryCombination`, `TestTheDeadlineIsTheActivationPlusTheGrace`, `TestTheGracePeriodIsBounded` | `backend/internal/authn/mfapolicy_test.go` |
| `TestAnAbsentMandateIsOff`, `TestTheMandateIsReadFromSettings` | same |
| MFA mandate enforced on refresh and silent authorize (P3-14 closing P3-07's A-2) | `MEMORY/records/2026-09-15-P3-14-test-suite.md` |
| `TestEverySchemaFactorTypeIsImplementedHere`, `TestAMRIsBuiltInOnePlace`, `TestTheHandleIsMintedFromRandomnessAndNothingElse`, `TestNoSourceLogsFactorMaterial` | `backend/internal/mfa/architecture_test.go` |
| Answer-path unit tests | `backend/internal/mfa/answertype_test.go`, `answerrecovery_test.go`, `answerwebauthn_test.go` |
| `TestAnAnswerThatWasValidCostsWhatAWrongOneCosts` (timing uniformity) | referenced in `MEMORY/records/2026-09-15-P3-14-test-suite.md` |
| `TestRecoveryCodeGuessesShareThePerUserBound` | same |
| Admin MFA reset over HTTP (role/tenant boundary) | `backend/internal/user/mfareset_integration_test.go` |
| Named E2E: correct password + wrong TOTP; recovery-code sign-in | `console/e2e/mfa.spec.ts` |

## Not Yet Built / Open Questions

- `mfa.StepUp` is implemented and tested but has no production caller — no endpoint currently requires step-up authentication for a sensitive action, despite `docs/PLAN/08-AUTHORIZATION.md` § Least Privilege recommending it.
- Cross-organization MFA mandate stacking (ADR-025 — the stricter of two organizations' mandates applies to a delegated user) is decided but unbuilt.

## Related Documents

- `docs/IDENTITY-PROTOCOL/05-WEBAUTHN-AND-PASSKEYS.md`
- `docs/IDENTITY-PROTOCOL/02-OAUTH21-AUTHORIZATION-SERVER.md`
- `docs/SESSION-MANAGEMENT/00-SESSION-ARCHITECTURE.md`, `02-REFRESH-TOKENS-AND-ROTATION.md`
- `MEMORY/specs/P3-01-mfa-framework.md` through `P3-10-mfa-tab.md`
- `MEMORY/records/2026-09-15-P3-14-test-suite.md`
- `TASKS/BACKLOG.md` PG-39
