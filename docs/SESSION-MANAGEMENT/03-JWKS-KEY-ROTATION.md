# 03 - JWKS Key Rotation

> Category: **Session Management** (`docs/SESSION-MANAGEMENT/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-03, P1-04 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describes the lifecycle of the asymmetric keys this service signs tokens with, and how rotation is performed without invalidating tokens that are already in flight.

## Scope

Covers `backend/internal/signing/` (key generation, the in-memory key set and cache, signing and verification) and `backend/cmd/keyctl` (the operator tool). JWT/claim structure that gets signed with these keys is `01-JWT-ISSUANCE-AND-STRUCTURE.md`. Discovery advertisement of `jwks_uri` is `docs/IDENTITY-PROTOCOL/01-OIDC-DISCOVERY-AND-JWKS.md`.

## As Built

Keys move through four states, tracked in the `signing_keys` table (`backend/migrations/20260908000005_sessions_and_tokens.up.sql`) and modeled by `signing.Status`:

```
next → current → previous → retired
```

- **`next`** — published in the JWKS document but does not sign yet, so consumers can fetch it into their cache before the first token signed with it arrives.
- **`current`** — the one key that signs. A partial unique index (`signing_keys_one_current_per_purpose`) enforces exactly one current key per `purpose` at the database level, so "two keys signing at once" is unrepresentable rather than merely avoided by convention.
- **`previous`** — still verifies, no longer signs. This is the overlap window that keeps tokens issued in the seconds before a rotation valid afterwards.
- **`retired`** — removed from JWKS entirely; tokens it signed stop verifying.

`Rotate` (`backend/internal/signing/store.go`) is one transaction that demotes the old `current` to `previous` **before** promoting the selected `next` key — ordering that matters because of the partial unique index (promoting first would violate it while a current key still exists). Retirement is a **separate, explicit** operator action (`Retire`) on a `previous` key, never automatic and never bundled into `Rotate` — retiring in the same step as promotion would invalidate every token issued in the seconds before the rotation, which is exactly the outage the overlap window exists to prevent.

**Rotation is a deliberate operator action, not a scheduled job** (`backend/cmd/keyctl/main.go`). The documented cadence is 90 days (`docs/PLAN/09-SECURITY.md` § Tokens & Keys), tracked in a runbook (`deploy/vm/RUNBOOK-key-rotation.md`) and a calendar, not a timer — the reasoning recorded in `keyctl`'s own doc comment is that a rotation failure (an unresolvable key, a consumer whose JWKS cache has not refreshed) is a failure mode a person should be watching for, and a scheduled job that fails at 3am is worse than a deliberate one at 11am.

`keyctl` subcommands: `list`, `generate` (creates a key in `next` and writes its private half to disk), `rotate` (promotes `next` to `current`), `retire <kid>`, `jwks` (prints the published set). It connects with the schema-owner DSN (`AUTH_MIGRATE_DSN`), never the runtime role, because rotation is schema-adjacent and the running service's own database role must not be able to promote a key.

**Private key material never enters the database.** `signing_keys.private_key_ref` stores only a reference (e.g. `file:<path>`); the column has a `CHECK` constraint (`signing_keys_private_key_is_a_reference`) that refuses any value resembling a PEM private key header, so a private key ending up in that column fails to insert rather than silently violating `docs/PLAN/02-REQUIREMENTS.md` § Constraints (no third party, including the database, holds the private key). The private key file itself is written with mode `0400`, owned by the service account.

`kid` is derived, not random — an RFC 7638 SHA-256 JWK thumbprint (`signing.Thumbprint`) — so it is stable across restarts (a random kid regenerated at boot would invalidate every consumer's cache on every deploy) and cannot be chosen by an attacker to collide with a legitimate key.

**Verification order is the defense.** `signing.Verifier.Verify` checks the JWS header's algorithm against a closed allowlist (`RS256`, `ES256`) *before* any key lookup — this is what makes `alg: none` and RSA/HMAC algorithm-confusion attacks structurally impossible rather than merely checked: the parser (`jose.ParseSigned`) is handed the allowlist directly and rejects anything outside it before a key is even fetched. There is no "try every key" fallback when `kid` is missing or unknown (`ErrMissingKID`, `ErrUnknownKID`) — iterating keys would be a timing oracle for how many keys exist and is how a retired key can end up accepted by a careless implementation.

The in-memory `signing.Cache` bounds verification to a memory lookup: it refreshes from the database at most every 5 minutes (`DefaultCacheTTL`), and a load failure returns the last-known-good snapshot rather than erroring, so a brief database outage does not fail every token verification in the deployment. `Invalidate()` lets the `rotate` command force an immediate reload on the instance an operator is watching.

`Purpose` (`oidc` vs `saml`) partitions key sets in the schema so a future SAML assertion-signing key rotation (`docs/IDENTITY-PROTOCOL/04-SAML-20-FEDERATION.md`, unbuilt) can never touch OIDC token keys — the column and the partial unique index already exist, unused, for the `saml` purpose.

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Algorithms | `RS256` (RSA ≥ 2048 bits), `ES256` (P-256 only) — never `HS256`, never `none` | `signing.Algorithm.Valid`, `allowedAlgorithms` |
| `kid` derivation | RFC 7638 SHA-256 JWK thumbprint | `signing.Thumbprint` |
| One current key per purpose | Enforced by partial unique index, not application logic alone | `signing_keys_one_current_per_purpose` |
| Key cache TTL (service memory) | 5 minutes | `signing.DefaultCacheTTL` |
| JWKS HTTP cache | `max-age=300` (matches the memory cache TTL) | `oidc` package, `jwksMaxAge` |
| Discovery document HTTP cache | `max-age=3600` | `oidc` package, `discoveryMaxAge` |
| Rotation cadence (operational, not enforced in code) | ~90 days | `docs/PLAN/09-SECURITY.md`, `deploy/vm/RUNBOOK-key-rotation.md` |
| Private key at rest | Reference only in DB; file mode `0400` | `signing.Store.Insert`, `signing_keys_private_key_is_a_reference` |

## Interfaces

| Method | Path | Notes |
|---|---|---|
| GET | `/.well-known/jwks.json` | Public halves of every non-retired key, for the applicable `purpose` |

Configuration: `AUTH_MIGRATE_DSN` (schema-owner connection for `keyctl`), `AUTH_SECRETS_DIR` (default `/etc/zed-auth/secrets`, where `keyctl generate` writes the private key file).

## Security Considerations

- **`alg: none`**: rejected before key lookup — `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §1.
- **RSA/HMAC algorithm confusion** (using a public RSA key as an HMAC secret): impossible because `HS256` is never in the allowlist.
- **Key compromise**: response is `keyctl generate` (new `next` key), wait for consumer JWKS caches to refresh, `keyctl rotate`, then `keyctl retire` the compromised key once its overlap window has passed — this is a manual runbook procedure, not an automated incident response.
- **Rollback safety**: an application rollback to an older version cannot invalidate tokens signed under a newer key, because retired keys are a separate, deliberate step from rotation (`docs/PLAN/14-DEPLOYMENT.md` § Rollback Strategy).

## Verification

| Test | File |
|---|---|
| `TestAlgNoneIsRejected`, `TestAlgorithmConfusionIsRejected`, `TestTheAlgorithmAllowlistIsExactlyTwoAsymmetricAlgorithms` | `backend/internal/signing/sign_test.go` |
| `TestOnlyCurrentKeySigns`, `TestTokenSignedByPreviousKeyStillVerifies`, `TestTokenSignedByRetiredKeyIsRejected` | same |
| `TestForgedTokenWithLegitimateKIDIsRejected`, `TestTamperedSignatureIsRejected`, `TestTokenWithoutKIDIsRejected`, `TestUnknownKIDIsDistinctFromRetired` | same |
| `TestJWKSPublishesEveryVerifyingKeyAndNoPrivateMaterial`, `TestKIDIsDerivedFromTheKeyAndIsStable`, `TestSignatureEncodingIsMalleable` | same |
| `TestKeySetRefusesTwoCurrentKeys`, `TestJWKSOrderingIsDeterministic`, `TestCurrentKeyIsPublishedFirst` | `backend/internal/signing/keyset_test.go` |
| `TestCacheServesTheLastGoodSetWhenReloadFails`, `TestCacheFailsWhenItHasNeverLoaded`, `TestCacheRespectsItsTTL` | same |
| `TestErrorsNeverContainKeyMaterial` | same |
| `TestJWKSExposesNoPrivateParameters`, `TestJWKSFailureRevealsNothing` | `backend/internal/oidc/discovery_test.go` |

## Not Yet Built / Open Questions

- SAML assertion-signing key rotation (the `saml` purpose partition exists in the schema; no code path uses it yet — owning tasks `P4-07`–`P4-09`).

## Related Documents

- `docs/SESSION-MANAGEMENT/01-JWT-ISSUANCE-AND-STRUCTURE.md`
- `docs/IDENTITY-PROTOCOL/01-OIDC-DISCOVERY-AND-JWKS.md`
- `docs/PLAN/09-SECURITY.md` § Tokens & Keys
- `docs/PLAN/14-DEPLOYMENT.md` § Rollback Strategy
- `MEMORY/specs/P1-03-signing-keys.md`
- `deploy/vm/RUNBOOK-key-rotation.md`
