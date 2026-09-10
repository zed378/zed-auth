# 14 — Deployment

## Deployment Model

- **Containerized** (Docker), run on Kubernetes for production; Docker Compose is fine for local dev/small staging.
- **Stateless service**: Auth Service instances scale horizontally freely — all state lives in PostgreSQL/Redis (`07-BACKEND-ARCHITECTURE.md`).
- **Health check** endpoints (`/healthz`, `/readyz`) for the load balancer & orchestrator.
- **Graceful shutdown**: finish in-flight requests before terminating a pod, since this sits on the critical path of many other services.

## Scalability

| Component | Scaling strategy |
|---|---|
| Auth Service (stateless) | Horizontal Pod Autoscaler based on CPU/latency |
| PostgreSQL | Read replicas for read-heavy queries; single primary for writes |
| Redis | Cluster mode if session volume is large |

## Release Process

- CI/CD via GitHub Actions: build → test (`11-TESTING.md`) → SAST/dependency scan (`09-SECURITY.md`) → deploy to staging → smoke test → promote to production.
- Database migrations run as a separate, reviewable step before application rollout, never implicitly on service startup in production.
- Feature flags for anything security-sensitive (e.g. enabling a new grant type, enabling ABAC) so a rollout can be paused without a full rollback.

## Environment Strategy

| Environment | Purpose | Notes |
|---|---|---|
| `local` | Development | docker-compose |
| `staging` | Integration testing with consumer applications | Mirrors production topology at smaller scale |
| `production` | Live | Multi-AZ minimum |

Every environment has **separate signing keys & databases** — never share production keys/data with staging (`09-SECURITY.md`).

## Rollback Strategy

- Application rollback: standard blue/green or rolling-update rollback via Kubernetes, since the service is stateless.
- Database migrations must be written to be backward-compatible for at least one release (expand/contract pattern), so an application rollback never requires a matching DB rollback.
- Signing key rotation rollback: keep the previous key in the JWKS during any rotation window (`09-SECURITY.md`) so a rollback of the application doesn't invalidate tokens signed under a newer key.

Continue to [15 — Disaster Recovery](./15-DISASTER-RECOVERY.md).
