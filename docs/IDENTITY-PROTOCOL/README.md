# Category: IDENTITY-PROTOCOL

OpenID Connect (OIDC), OAuth 2.1 Authorization Server, SAML 2.0 Federation, WebAuthn/Passkeys, and MFA specifications.

## Category Mandate

The `IDENTITY-PROTOCOL/` directory defines the platform's **federated identity contracts**. It specifies strict compliance with OIDC Core 1.0, OAuth 2.1 draft, SAML 2.0 Web Browser SSO profile, FIDO2/WebAuthn, and Multi-Factor Authentication.

## Documents in Category

| Document | Title | Description |
|---|---|---|
| `00-IDENTITY-PROTOCOL-OVERVIEW.md` | Protocol Overview | Standards compliance & protocol suite layout. |
| `01-OIDC-DISCOVERY-AND-JWKS.md` | OIDC Discovery & JWKS | `/.well-known/openid-configuration` & `/.well-known/jwks.json`. |
| `02-OAUTH21-AUTHORIZATION-SERVER.md` | OAuth 2.1 Server | Authorization Code Flow with PKCE, Client Credentials. |
| `03-USERINFO-ENDPOINT.md` | UserInfo Endpoint | `/userinfo` response claims & scope mapping. |
| `04-SAML-20-FEDERATION.md` | SAML 2.0 Federation | IdP & SP metadata, assertion signing, assertion consumer. |
| `05-WEBAUTHN-AND-PASSKEYS.md` | WebAuthn & Passkeys | FIDO2 registration, assertion verification, passkeys. |
| `06-MULTI-FACTOR-AUTHENTICATION.md` | MFA & TOTP | TOTP enrollment, verification, recovery code strategy. |
