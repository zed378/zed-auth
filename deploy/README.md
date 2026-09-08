# deploy/

Deployment configuration: local Docker Compose, container images, Kubernetes manifests, and observability dashboards.

**Governing documents**: `PLAN/14-DEPLOYMENT.md`, `PLAN/13-OBSERVABILITY.md`, `PLAN/15-DISASTER-RECOVERY.md`.

## Rules That Are Not Negotiable

- **Every environment has separate signing keys and databases.** Production keys and data are never copied into staging (`PLAN/13`, `PLAN/14`).
- **Migrations run as a separate, reviewable step before application rollout** — never implicitly at service startup in production (`PLAN/14`).
- Image versions are pinned. Never `:latest` (`SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §15).
- Containers run as a non-root user (`SECURITY/02` §17).
