# Session Management

This category covers everything that happens after a user proves who they are: the browser session cookie that carries single sign-on, the JWTs and refresh tokens minted from it at `/oauth/token`, the signing keys behind those JWTs, and what "revoked" actually means for each of those credential types. It relates to `docs/PLAN/04-DATA-MODEL.md` (schema), `docs/PLAN/09-SECURITY.md` (token/key lifetime requirements this implementation satisfies), and `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` (the abuse cases these mechanisms close). `docs/IDENTITY-PROTOCOL/` covers the protocol surface (OIDC, OAuth, MFA) that sits around this session and token machinery.

## Documents

| File | Topic | Status |
|---|---|---|
| [`00-SESSION-ARCHITECTURE.md`](./00-SESSION-ARCHITECTURE.md) | The session cookie, its PostgreSQL+Redis storage, and immediate revocation | Implemented |
| [`01-JWT-ISSUANCE-AND-STRUCTURE.md`](./01-JWT-ISSUANCE-AND-STRUCTURE.md) | ID and access token claim shapes, lifetimes, and type separation | Implemented |
| [`02-REFRESH-TOKENS-AND-ROTATION.md`](./02-REFRESH-TOKENS-AND-ROTATION.md) | Refresh token rotation, the grace window, and reuse detection | Implemented |
| [`03-JWKS-KEY-ROTATION.md`](./03-JWKS-KEY-ROTATION.md) | Signing key states, the `keyctl` operator tool, and verification defenses | Implemented |
| [`04-TOKEN-REVOCATION-AND-BLACKLISTING.md`](./04-TOKEN-REVOCATION-AND-BLACKLISTING.md) | What revocation achieves per credential type — there is no access-token blacklist | Implemented |
