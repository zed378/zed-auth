# 03 - Application Integration Guide

> Category: **DEVELOPER** (`docs/DEVELOPER/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Provide a step-by-step integration guide for third-party application developers.

## Category Mandate

Guides external developers on connecting their applications to the Centralized Auth Service.

## Key Topics To Specify

- Registering a new application in the Management Console.
- Configuring OIDC redirect URIs and scopes.
- Integrating Go or TypeScript SDKs into consumer applications.

## Reference Architecture & Specification

Integration Steps:
1. Register Application in Console -> Obtain Client ID.
2. Add SDK package to consumer application.
3. Wrap main application in Auth Provider.
4. Protect routes with auth middleware.

## Acceptance Criteria

- [x] Step-by-step integration workflow documented.
- [x] Code snippets for consumer app integration provided.

## Open Questions

None.

## Related Documents

- `docs/SDK/00-SDK-ARCHITECTURE.md`
