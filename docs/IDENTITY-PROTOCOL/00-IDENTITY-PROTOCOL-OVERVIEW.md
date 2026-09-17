# 00 - Identity Protocol Overview

> Category: **IDENTITY-PROTOCOL** (`docs/IDENTITY-PROTOCOL/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify identity standards compliance for OIDC, OAuth 2.1, SAML 2.0, and WebAuthn.

## Category Mandate

Ensures interoperability with standard OIDC clients, enterprise IdPs, and modern web browsers.

## Key Topics To Specify

- OIDC Core 1.0 specification adherence.
- OAuth 2.1 draft specifications (PKCE mandatory for all interactive flows).
- SAML 2.0 Enterprise IdP/SP integration.
- FIDO2 / WebAuthn W3C standard compliance.

## Reference Architecture & Specification

Supported Flow Matrix:
- Web SPAs & Mobile: OAuth 2.1 Authorization Code + PKCE
- Machine-to-Machine: OAuth 2.1 Client Credentials
- Enterprise SSO: SAML 2.0 / OIDC Federation

## Acceptance Criteria

- [x] Identity protocols enumerated.
- [x] Standards compliance matrices documented.

## Open Questions

None.

## Related Documents

- `docs/IDENTITY-PROTOCOL/01-OIDC-DISCOVERY-AND-JWKS.md`
- `docs/IDENTITY-PROTOCOL/02-OAUTH21-AUTHORIZATION-SERVER.md`
