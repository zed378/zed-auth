# 02 - OAuth 2.1 Authorization Server

> Category: **IDENTITY-PROTOCOL** (`docs/IDENTITY-PROTOCOL/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Detail OAuth 2.1 grant types, PKCE requirement, code exchange, and client authorization semantics.

## Category Mandate

Guarantees robust authorization code handling without legacy insecure grants (Implicit grant forbidden).

## Key Topics To Specify

- Authorization Code Flow + PKCE (`code_challenge` / `code_verifier`).
- Client Credentials Grant for M2M communication.
- Strict redirect URI exact matching.
- Token exchange grant type for cross-service delegation.

## Reference Architecture & Specification

PKCE Workflow:
`Client generates verifier -> sends S256 challenge in /authorize -> submits verifier in /token`

## Acceptance Criteria

- [x] OAuth 2.1 grant rules specified.
- [x] PKCE enforcement documented.

## Open Questions

None.

## Related Documents

- `docs/API/08-AUTH-API.md`
