# P0-14 — Staging state lost again, recovered, and `main` deployed

Date: 2026-11-26. Status: staging recovered and deployed; cause not established.
Previous occurrence: [2026-09-11](./2026-09-11-staging-auth-state-deleted.md).

## Found

`/home/infra/auth-state` had lost `.env`, `secrets/`, `artifacts/` and every
backup. The secrets directory the service had mounted was emptied
2026-11-16 07:58 UTC. A `sudo su` session for `infra` had opened at 07:53; this
is proximity in time, not an established cause.

- The service kept running on keys held in memory. A restart would have failed.
- Console and public site answered 403.
- The nightly backup failed from 2026-11-17 with `.env not found`. Nobody was
  alerted (`BL-01` again). The newest surviving dump was from 2026-09-21.

## Recovered

1. Manual database dump, restore-tested, before any change.
2. `auth-state` ownership returned to `infra`, mode 700.
3. `.env` rebuilt from the running containers. Compose config hashes for
   `authservice`, `postgres`, `redis` and `mailpit` matched the running ones, and
   no declared environment value differed.
4. New metrics token and MFA seal key. No MFA factors or SAML service providers
   existed, so no stored data depended on the lost keys.
5. Backup unit run and verified before migrating.
6. Image `zed-auth:main-8fbdb38`, migrations 45 to 47, new OIDC and SAML signing
   keys rotated in, preflight container healthy, then rollout. Rollback target was
   `zed-auth:p4-09`.
7. Console and public site rebuilt locally and copied into `artifacts/`.

## Verified

- Service healthy, no restarts; health, readiness, discovery, JWKS and SAML
  metadata answer 200. Console and public site answer 200.
- `acceptance-phase3.sh` 18 passed (the 19th recorded check is the optional
  latency measurement), `acceptance-delegation.sh` 14, `acceptance-saml.sh` 39;
  no failures and no fixtures left.
- On the live database: `user_identities` has row-level security and no `UPDATE`
  for `auth_app`; `events` and its partitions are not writable by it.
- Social sign-in start answers 404.
- Backup verified after the deploy: 29 tables.
- The one `ERROR` line after rollout is the refresh-token reuse detection that the
  acceptance run triggers.

Console sign-in was not clicked through in a browser; the authorize redirect and
the CORS preflight for the console origin answer correctly.

## Open

- The previous OIDC and SAML keys are still `previous`, so they still verify.
  Their private halves are gone and their custody on 2026-11-16 is unknown.
  Retire them as on 2026-09-11 (`RUNBOOK-key-rotation.md`).
- `PG-29`: the secrets directory is still not backed up.
- Backups are local only (`AUTH_BACKUP_REMOTE` unset), on the same disk.
- Nothing prevents or alerts on the next deletion of `auth-state`.
