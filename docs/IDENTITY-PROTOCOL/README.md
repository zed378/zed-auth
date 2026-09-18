# Identity Protocol

This category covers the standards-facing surface of the Auth Service: OpenID Connect discovery and JWKS, the OAuth 2.1 authorization server, the UserInfo endpoint, multi-factor authentication (TOTP, WebAuthn, recovery codes, the organization mandate), and — as a draft specification only, since it is not built — SAML 2.0 federation. It relates to `docs/PLAN/03-ARCHITECTURE.md` and `docs/PLAN/07-BACKEND-ARCHITECTURE.md` (design intent), `docs/PLAN/08-AUTHORIZATION.md` (the claims these protocols carry), and `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` (the abuse cases each control closes). `docs/SESSION-MANAGEMENT/` covers the session and token machinery these protocols sit on top of.

## Documents

| File | Topic | Status |
|---|---|---|
| [`00-IDENTITY-PROTOCOL-OVERVIEW.md`](./00-IDENTITY-PROTOCOL-OVERVIEW.md) | Map of what is built vs. planned across this category | Partially implemented |
| [`01-OIDC-DISCOVERY-AND-JWKS.md`](./01-OIDC-DISCOVERY-AND-JWKS.md) | The discovery document (derived from running capability) and JWKS | Implemented |
| [`02-OAUTH21-AUTHORIZATION-SERVER.md`](./02-OAUTH21-AUTHORIZATION-SERVER.md) | Authorization code + PKCE, refresh, client credentials; refused grants | Implemented |
| [`03-USERINFO-ENDPOINT.md`](./03-USERINFO-ENDPOINT.md) | `/oauth/userinfo` claim mapping and scope gating | Implemented |
| [`04-SAML-20-FEDERATION.md`](./04-SAML-20-FEDERATION.md) | SAML 2.0 as an Identity Provider | Draft specification |
| [`05-WEBAUTHN-AND-PASSKEYS.md`](./05-WEBAUTHN-AND-PASSKEYS.md) | WebAuthn as a second factor and hosted registration; not passwordless sign-in | Partially implemented |
| [`06-MULTI-FACTOR-AUTHENTICATION.md`](./06-MULTI-FACTOR-AUTHENTICATION.md) | TOTP, recovery codes, the 14-day mandate grace, and every enforcement point | Implemented |
