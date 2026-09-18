# 00 - Identity Protocol Overview

> Category: **Identity Protocol** (`docs/IDENTITY-PROTOCOL/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: P1-04, P1-06, P1-07, P1-08, P3-01…P3-14 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Orients a reader across the protocol surfaces this service implements or plans to implement: OpenID Connect discovery, the OAuth 2.1 authorization server, UserInfo, SAML 2.0 federation, WebAuthn/passkeys, and multi-factor authentication. States which of these are running code today and which are design intent only.

## Scope

This document is a map, not a specification of any one surface — each linked document is authoritative for its own area. `docs/PLAN/03-ARCHITECTURE.md` and `docs/PLAN/07-BACKEND-ARCHITECTURE.md` are the design intent this category implements against.

## As Built

| Surface | Status | Document |
|---|---|---|
| OIDC discovery + JWKS | Implemented | `01-OIDC-DISCOVERY-AND-JWKS.md` |
| OAuth 2.1 authorization server (authorization code + PKCE, refresh, client credentials) | Implemented | `02-OAUTH21-AUTHORIZATION-SERVER.md` |
| UserInfo endpoint | Implemented | `03-USERINFO-ENDPOINT.md` |
| SAML 2.0 federation | Not built | `04-SAML-20-FEDERATION.md` (Draft specification) |
| WebAuthn / passkeys | Implemented, but only as a second factor and for hosted registration — not as a passwordless primary sign-in method | `05-WEBAUTHN-AND-PASSKEYS.md` |
| Multi-factor authentication (TOTP, WebAuthn, recovery codes, org mandate) | Implemented | `06-MULTI-FACTOR-AUTHENTICATION.md` |

All hosted, human-facing authentication (the login form, the MFA challenge, forced enrolment, passkey registration) is served by this service itself — never by the console — so signing in never depends on the console being deployed or reachable (`docs/PLAN/02-REQUIREMENTS.md` FR-1). Every capability described here is also reachable through the REST Management API where one applies (`docs/PLAN/02-REQUIREMENTS.md` FR-14) — see each document's Interfaces section.

The one identity decision made but not yet built anywhere in this category is **cross-organization sign-in**: `MEMORY/DECISIONS.md` ADR-025 specifies that when a Project Grant lets a user from one organization reach another organization's application, both organizations' sign-in policies apply and the stricter wins (MFA required if either requires it; sign-in methods are the intersection; session lifetime is the shorter). Today the authorization endpoint refuses any session whose organization does not match the client's outright — see `02-OAUTH21-AUTHORIZATION-SERVER.md` and `docs/SESSION-MANAGEMENT/00-SESSION-ARCHITECTURE.md`.

## Not Yet Built / Open Questions

- SAML 2.0 (`P4-07`–`P4-09`), social login and account linking (`P4-10`, `P4-11`), and SCIM (`P4-13`, gated) are all Phase 4 and unbuilt — see `TASKS/BACKLOG.md` and `MEMORY/records/2026-09-15-P3-15-phase-4-threat-review.md` for the constraints already decided against each.
- Cross-organization sign-in (ADR-025) is decided, unbuilt.

## Related Documents

- `docs/PLAN/16-IMPLEMENTATION-ROADMAP.md`
- `docs/SECURITY/00-ASSET-AND-TRUST-BOUNDARY-INVENTORY.md`
- `docs/SESSION-MANAGEMENT/README.md`
