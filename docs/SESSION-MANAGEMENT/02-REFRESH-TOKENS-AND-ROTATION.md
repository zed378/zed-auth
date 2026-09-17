# 02 - Refresh Token Rotation & Reuse Detection

> Category: **SESSION-MANAGEMENT** (`docs/SESSION-MANAGEMENT/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify refresh token rotation family algorithm and automatic reuse detection mitigation.

## Category Mandate

Prevents token theft replay attacks by invalidating token families upon detecting reused refresh tokens.

## Key Topics To Specify

- Every refresh token issuance creates or inherits a `family_id`.
- Upon refresh, old token is revoked and new token issued in same family.
- If a revoked refresh token is presented, ALL tokens in that `family_id` are immediately revoked (theft detected).

## Reference Architecture & Specification

Reuse Mitigation Pseudocode:
```go
if token.IsRevoked {
    RevokeAllTokensInFamily(token.FamilyID)
    return ErrTokenCompromisedReuseDetected
}
```

## Acceptance Criteria

- [x] Refresh token rotation workflow specified.
- [x] Token family revocation on reuse enforced.

## Open Questions

None.

## Related Documents

- `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`
