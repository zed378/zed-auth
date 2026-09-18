# 02 - Refresh Tokens and Rotation

> Category: **Session Management** (`docs/SESSION-MANAGEMENT/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-07, P3-06, P3-14 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describes how refresh tokens are issued, rotated on every use, and how reuse of a rotated (stolen or replayed) token is detected and answered — the mechanism `docs/PLAN/17-ACCEPTANCE-CRITERIA.md`'s Phase 3 criterion ("a rotated refresh token cannot be reused") refers to.

## Scope

Covers `backend/internal/oauth/token/refresh.go` and `rotation.go`. Access/ID token structure is `01-JWT-ISSUANCE-AND-STRUCTURE.md`. What "revoked" means for a refresh token specifically, alongside sessions and access tokens, is consolidated in `04-TOKEN-REVOCATION-AND-BLACKLISTING.md`.

## As Built

A refresh token is **opaque**, never a JWT — a deliberate choice (`backend/internal/oauth/token/refresh.go`): it must be revocable (which requires a lookup, defeating a stateless JWT), and separately, an earlier finding (`P1-03`) showed that a JWT's compact serialization is not canonical (multiple valid encodings verify), which would let reuse detection keyed on the token string be defeated by re-encoding one character. An opaque 256-bit random value hashed with unsalted SHA-256 has exactly one representation.

**Rotation**: every successful `refresh_token` grant issues a successor in the same rotation *family* and marks the presented token's `replaced_by` column, inside one transaction (`RefreshStore.Rotate`). The link is written with a conditional `UPDATE ... WHERE replaced_by IS NULL` (or, on the legitimate-retry path below, `WHERE replaced_by = <the untouched successor>`), so two concurrent rotations of the same predecessor cannot both succeed — the loser gets `ErrRefreshReuse`. The family's absolute expiry (`family_expires_at`) is **inherited, not recomputed**, on every rotation, which is what stops continuous refreshing from extending one authentication indefinitely.

**Reuse detection** (`detectReuse` in `handler.go`) runs *before* the ordinary liveness lookup, because rotation deliberately leaves the predecessor row present (only `replaced_by` set, not immediately revoked) — by the time an ordinary liveness filter would exclude it, the evidence of what happened is gone. Presenting a token that has already been rotated is answered one of three ways:

1. **Legitimate retry** — the successor is still untouched (`!Revoked && !SuccessorSpent`) and the presentation arrives within the grace window. Treated as an ordinary retry: the existing successor is re-served (`supersede` path in `Rotate`), no alert.
2. **Reuse** — the successor has already been used or revoked, or the grace window has passed. The **entire family** is revoked (`RevokeFamily`) — every token descended from the original issuance, including the successor the legitimate client is currently holding. An `audit.EventRefreshReuseDetected` event is written (`family_id`, `client_id`, `tokens_revoked`, `seconds_after_rotation`), logged at `ERROR`, and a dedicated `RefreshReuse()` metric is incremented — deliberately separate from the generic "denied" counter, because ordinary refresh denials have a non-zero baseline (expired tokens, sloppy clients) and reuse does not; an operator is meant to page on any non-zero reuse count.
3. **Already-dead family, presented again** — refused with the same `invalid_grant` answer, but **not** re-revoked and **not** re-alerted, so an attacker retrying a dead token cannot generate one alert per attempt.

A token this service never issued (unknown hash) is answered "not reuse" — an ordinary `invalid_grant` — because there is no real lineage to attack, and answering "reuse" to a guess would let anybody trigger a family kill by guessing.

The liveness of the **session** behind a refresh token is re-checked on every refresh (`h.Sessions.IsLive`): a refresh token whose originating session has been revoked is refused, even though the refresh token row itself is not marked revoked by that act alone (see `04-TOKEN-REVOCATION-AND-BLACKLISTING.md` for how sessionapi and logout close that gap explicitly).

**The MFA mandate is re-read on every refresh** (`authn.MandateCheck`, added at P3-14 to close a gap the P3-07 specification had named — "the refresh grant re-reads the policy" — but that nothing had implemented, per `MEMORY/records/2026-09-15-P3-14-test-suite.md`). A refresh for a user whose organization now mandates MFA past its grace, and who has not enrolled, is refused with the same `invalid_grant` as any dead token; the family itself is **not** revoked, since nothing about it is suspect — the user simply signs in again, which routes them into enrolment.

Scope on refresh may only **narrow**, never widen (`NarrowScope`) — a refresh cannot become more powerful than the authorization that created it.

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Refresh token lifetime (per token) | 14 days | `token.RefreshTokenLifetime` |
| Family absolute lifetime (per rotation lineage) | 90 days, inherited across rotations | `token.FamilyLifetime`, `RefreshStore.Rotate` |
| Rotation grace window | 30 seconds | `token.RotationGrace` |
| Legitimate-retry admission condition | Grace window **and** successor untouched | `Lineage.LegitimateRetry` |
| Token entropy | 256 bits, unsalted SHA-256 hash stored | `newRefreshPlaintext`, `HashRefresh` (ADR-016) |
| Reuse outcome | Entire family revoked; dedicated alert/metric; no refresh token minted on the `client_credentials` grant | `killFamily` |
| MFA mandate check | Re-read every refresh; fails closed if unreadable | `authn.MandateCheck`, `token.Handler.Mandate` |

## Interfaces

Consumed only via `POST /oauth/token` with `grant_type=refresh_token` — see `docs/IDENTITY-PROTOCOL/02-OAUTH21-AUTHORIZATION-SERVER.md`. No standalone refresh-token endpoint exists.

Database: `refresh_tokens` table (`backend/migrations/20260908000005_sessions_and_tokens.up.sql`) — columns `family_id`, `replaced_by`, `family_expires_at` existed from Phase 1 so that P3-06 changed behavior, not schema. `refresh_token_lineage` (`backend/migrations/20260913000032_refresh_reuse_detection.up.sql`) is a second `SECURITY DEFINER` function, added specifically for reuse detection — it deliberately returns dead/rotated tokens too, unlike the happy-path `refresh_token_by_hash`, which filters them out.

Audit event: `refresh.reuse_detected` (`audit.EventRefreshReuseDetected`).

## Security Considerations

- **Stolen refresh token**: the thief's use (or the legitimate client's subsequent use) of the rotated-away predecessor triggers a full family revocation, logging out both. This is the deliberate trade named in the code: it cannot be known which of two identical presentations is the thief, so both lose access. `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §10.
- **Network-retry false positive**: closing this without breaking legitimate retries is the entire reason for the 30-second grace *combined with* the untouched-successor check — grace alone would also admit a thief who used the token within 30 seconds, so both conditions are required together.
- **Alert fatigue avoidance**: a killed family answers subsequent presentations without re-alerting, so an attacker cannot manufacture unlimited pages by retrying a token they know is dead.

## Verification

| Test | File |
|---|---|
| `TestTheRetryWindowAdmitsOnlyAnUntouchedSuccessor`, `TestTheGraceWindowIsShort`, `TestRotatedReportsTheLink` | `backend/internal/oauth/token/rotation_test.go` |
| `TestARotatedRefreshTokenCannotBeReused` | `backend/internal/oauth/token/rotation_integration_test.go` |
| `TestReuseRevokesTheWholeFamily`, `TestReuseIsAudited`, `TestReuseAlertsOncePerFamily` | same |
| `TestARetryAfterALostResponseIsNotReuse`, `TestTheSamePresentationIsTheftOnceTheSuccessorIsUsed` | same |
| `TestRefreshingStaysInOneFamily`, `TestRefreshingCannotExtendTheAbsoluteLifetime` | same |
| `TestAnUnknownTokenIsRefusedWithoutAnAlert`, `TestRotationStoresOnlyHashes`, `TestARefreshTokenIsBoundToItsClient` | same |
| `TestARefreshTokenFromARevokedSessionIsRefused` | `backend/internal/oauth/token/lifetime_integration_test.go` |
| `TestConcurrentRefreshesLeaveExactlyOneLiveToken` (8 concurrent racers, exactly one live successor) | referenced in `MEMORY/records/2026-09-15-P3-14-test-suite.md` |
| MFA mandate re-checked on refresh | same record; mutation-tested by removing the refusal and the factor-type filter |

## Not Yet Built / Open Questions

- Cross-organization (delegated) refresh, where a session's originating organization differs from the granting organization's policy, is decided (ADR-025) but unbuilt — owning task `P4-04`.

## Related Documents

- `docs/SESSION-MANAGEMENT/00-SESSION-ARCHITECTURE.md`
- `docs/SESSION-MANAGEMENT/04-TOKEN-REVOCATION-AND-BLACKLISTING.md`
- `docs/IDENTITY-PROTOCOL/06-MULTI-FACTOR-AUTHENTICATION.md`
- `MEMORY/specs/P3-06-refresh-rotation.md`
- `MEMORY/records/2026-09-15-P3-14-test-suite.md`
- `MEMORY/DECISIONS.md` ADR-016
