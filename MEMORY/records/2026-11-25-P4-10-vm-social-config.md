# P4-10 — VM social configuration and recovery runbook

Date: 2026-11-25. Parent P4-10 remains WIP; live-provider acceptance and P4-11
authenticated linking remain pending.

Review found that both VM Compose manifests explicitly allowlisted environment
variables but omitted the seven social provider settings. Setting credentials in
the operator's environment file therefore never enabled a provider in the service.
Both manifests now forward optional provider IDs, secret references and the
Microsoft tenant UUID. Disabled installations keep empty defaults. The existing
read-only secrets mount and owner/application database credential split remain
the deployment boundary.

`deploy/vm/env.example` documents the optional settings, and
`deploy/vm/RUNBOOK-social-login.md` covers exact callbacks, file-secret ownership,
live acceptance evidence, password/MFA fallback, suspension, rotation and rollback.
It explicitly records that authenticated linking is pending and forbids manual
database linking as a workaround. No deployment or real provider registration was
performed.

The sole testing agent added `scripts/check-vm-social-config.py` and verified all
four renders (both manifests, disabled/configured providers). Each render checks
configuration propagation and isolation of sentinel owner/migration credentials;
configured renders also require a read-only secrets mount. CI now runs this check
in its existing deployment validation step. Fixtures ignore workstation `AUTH_*`
values and `.env` files and never print rendered credentials.

Plan references: `docs/PLAN/05-API-CONTRACT.md` social login,
`docs/PLAN/08-AUTHORIZATION.md` organization policy, and
`docs/PLAN/11-TESTING.md` configuration/security verification. The reference plan
documents were not edited.
