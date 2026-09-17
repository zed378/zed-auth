# 04 - SAML 2.0 Federation Specification

> Category: **IDENTITY-PROTOCOL** (`docs/IDENTITY-PROTOCOL/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify SAML 2.0 Identity Provider (IdP) and Service Provider (SP) integration, XML metadata, and assertion signing.

## Category Mandate

Enables enterprise single sign-on with Azure AD, Okta, and PingFederate.

## Key Topics To Specify

- SAML 2.0 Web Browser SSO Profile.
- IdP Metadata XML generation.
- Assertion digital signature (RSA-SHA256).
- Attribute Statements mapping (email, NameID, groups).

## Reference Architecture & Specification

SAML Flow: `User -> SP Redirect to Auth Service -> Enterprise IdP Login -> SAML Response Post -> Session Established`

## Acceptance Criteria

- [x] SAML metadata structure specified.
- [x] Assertion signing requirements defined.

## Open Questions

Verify SAML single-logout (SLO) binding support.

## Related Documents

- `docs/IDENTITY-PROTOCOL/00-IDENTITY-PROTOCOL-OVERVIEW.md`
