# deploy/

Deployment configuration: local Docker Compose, container images, Kubernetes manifests, and observability dashboards.

**Governing documents**: `docs/PLAN/14-DEPLOYMENT.md`, `docs/PLAN/13-OBSERVABILITY.md`, `docs/PLAN/15-DISASTER-RECOVERY.md`.

## Rules That Are Not Negotiable

- **Every environment has separate signing keys and databases.** Production keys and data are never copied into staging (`docs/PLAN/13`, `docs/PLAN/14`).
- **Migrations run as a separate, reviewable step before application rollout** — never implicitly at service startup in production (`docs/PLAN/14`).
- Image versions are pinned. Never `:latest` (`docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §15).
- Containers run as a non-root user (`docs/SECURITY/02` §17).
