# Category: SESSION-MANAGEMENT

Session architecture, JWT issuance, JWKS key rotation, refresh token family rotation, and revocation lists.

## Category Mandate

The `SESSION-MANAGEMENT/` directory governs **token state and session lifecycles**. It specifies cryptographic signing, key rotation, token rotation security, and instant revocation mechanisms across stateless JWTs and stateful sessions.

## Documents in Category

| Document | Title | Description |
|---|---|---|
| `00-SESSION-ARCHITECTURE.md` | Session Architecture | Overview of sessions, access tokens, refresh tokens. |
| `01-JWT-ISSUANCE-AND-STRUCTURE.md` | JWT Issuance & Structure | Token claims, RS256/ES256 signatures, expiration TTLs. |
| `02-REFRESH-TOKENS-AND-ROTATION.md` | Refresh Token Rotation | Token family tracking & automatic reuse detection. |
| `03-JWKS-KEY-ROTATION.md` | JWKS Key Rotation | Asymmetric key generation, rotation schedule, active/passive keys. |
| `04-TOKEN-REVOCATION-AND-BLACKLISTING.md` | Token Revocation List | Redis revocation store, real-time token blacklisting. |
