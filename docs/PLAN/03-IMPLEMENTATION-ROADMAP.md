# 03 - Implementation Roadmap

> Category: **PLAN** (`docs/PLAN/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Define the sequential implementation roadmap across Phase 1 (Core MVP), Phase 2 (Enterprise & Delegation), Phase 3 (Public Site & Advanced ABAC), and Phase 4 (Scale & Hardening).

## Category Mandate

Prevents scope creep and ensures early roadmap phases are fully validated before starting subsequent phases.

## Key Topics To Specify

- Phase 1: Core Auth, PostgreSQL RLS, RBAC, REST API, Console UI.
- Phase 2: Project Grants cross-org delegation, SAML 2.0, WebAuthn/Passkeys, Webhooks.
- Phase 3: ABAC evaluation engine, Public marketing/docs site, Advanced audit logs.
- Phase 4: Multi-region deployment, automated disaster recovery, zero-trust hardening.

## Reference Architecture & Specification

```
Phase 1 (MVP Auth & RBAC) -> Phase 2 (Delegation & SAML) -> Phase 3 (ABAC & Public Site) -> Phase 4 (Enterprise Scale)
```

## Acceptance Criteria

- [x] Roadmap phases clearly defined.
- [x] Dependency order between phases enforced.

## Open Questions

Verify readiness gate for Phase 2 SAML federation.

## Related Documents

- `docs/PLAN/02-REQUIREMENTS.md`
- `docs/PLAN/04-ACCEPTANCE-CRITERIA.md`
