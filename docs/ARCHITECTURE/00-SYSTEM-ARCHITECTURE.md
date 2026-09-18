# 00 - System Architecture

> Category: **Architecture** (`docs/ARCHITECTURE/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-02, P0-10, P1-15, P1-29 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describe the deployable units, what runs where, and how a request reaches a handler. `docs/PLAN/03-ARCHITECTURE.md` is the design intent; this is the shape the code and the compose files actually take.

## Scope

The system as a whole. Internals of the Go service are `02-BACKEND-ARCHITECTURE.md`; the console and public site are `03` and `04`; environments and deployment mechanics are `docs/DEVOPS/`.

## As Built

```
        browser                          consumer application / service
           |                                          |
           |  hosted login, console, public site      |  OIDC + REST
           v                                          v
   +-------------------------------------------------------------+
   |                    authservice (Go, one binary)              |
   |   /oauth/*  ·  /.well-known/*  ·  /login  ·  /v1/*  ·  probes |
   +------------------+--------------------------+---------------+
                      |                          |
                      v                          v
                 PostgreSQL                    Redis
        (tenant data, RLS, audit)     (authz cache, rate limits)
                      |
                      v
                  Mailpit / SMTP
```

Separately deployed static artefacts:

- **Console** — a React bundle (`console/`), served as static files, calling `/v1` with a bearer token obtained through the same OIDC flow any other client uses.
- **Public site** — a Docusaurus-based static site (`public-site/`), including the API reference generated from `openapi/openapi.yaml`.
- **Demo applications** — two small consumers (`deploy/demo`) used to prove SSO between applications end to end.

One Go binary serves every protocol surface (`backend/cmd/authservice`). Three further binaries exist for operations: `migrate`, `keyctl` and `passwordhash`.

### How a request is handled

`backend/internal/httpserver/server.go` builds a chi router with this middleware order: request id → panic recovery → access log → metrics instrumentation → security headers → CORS → timeout. Health probes are registered on a bare sub-router with no access logging, because probe traffic would otherwise drown the log.

Routes are **not** registered by hand: the generated router from `openapi/openapi.yaml` is mounted, so served paths are the documented paths by construction (ADR-013). A handful of OAuth endpoints are registered explicitly before it — `/oauth/authorize`, `/oauth/token`, `/oauth/introspect`, `/oauth/revoke`, `/oauth/userinfo` (GET and POST, as OIDC Core 5.3.1 requires) — and still pass through the router-level middleware.

Everything under `/v1` additionally passes the management chain (`backend/internal/management/chain.go`): authentication, permission check, per-client rate limiting, idempotency, body buffering and the audit guard. See `05` of `docs/API/`.

### Storage

- **PostgreSQL** holds every tenant's data behind row-level security. The application connects as a restricted role; migrations run as the owner role (`docs/DATABASE/`).
- **Redis** holds the authorization cache and rate-limit counters. Both degrade to correct-but-slower behaviour when it is unavailable.
- **Mail** is delivered through SMTP; the local and E2E stacks use Mailpit.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Served paths equal documented paths | Generated strict server; a spec path with no handler fails to compile | `backend/internal/api/`, ADR-013 |
| Public ports on the staging host | Above 10000, exposed through a Cloudflare tunnel | `deploy/vm/`, `docs/DEVOPS/01-ENVIRONMENTS-AND-CONFIGURATION.md` |
| Health endpoints | `/healthz`, `/readyz`, no access logging, no store | `backend/internal/httpserver/server.go` |
| Admin surface (metrics) | Separate listener on its own port | `backend/cmd/authservice/main.go` |
| CORS | Per-application origins; `/oauth/token` is `*` without credentials; `/oauth/userinfo` is per-application | `httpserver.CORS`, `TASKS` P1-29, `PG-17` |

## Interfaces

- Protocol surface: `docs/IDENTITY-PROTOCOL/`.
- Management API: `docs/API/`.
- Configuration: environment variables read by `backend/internal/config/`.

## Security Considerations

- The trust boundaries this diagram crosses are enumerated in `docs/SECURITY/00-ASSET-AND-TRUST-BOUNDARY-INVENTORY.md`; the console and the public site are separate origins from the issuer, which is what makes passkey registration a hosted-page concern (`TASKS/BACKLOG.md` PG-40, PG-43).
- No component shares code between the public site and the console, and a CI gate asserts it (`scripts/check.sh`).

## Verification

- `backend/internal/httpserver/*_test.go` — middleware order, headers, CORS.
- `backend/internal/oidc/discovery_test.go` — only implemented endpoints are advertised.
- `console/e2e/sso.spec.ts` — SSO across two demo applications against a real service.
- `scripts/check.sh` — the gates that keep these properties true.

## Not Yet Built / Open Questions

- Kubernetes is the intended production target (`docs/PLAN/14-DEPLOYMENT.md`, ADR-011); today every environment runs Docker Compose.
- Horizontal scale-out has not been exercised; the service is stateless apart from Postgres and Redis, but no multi-instance run is recorded.

## Related Documents

- `docs/PLAN/03-ARCHITECTURE.md`, `docs/PLAN/07-BACKEND-ARCHITECTURE.md`.
- `01-SERVICE-BOUNDARIES.md`, `02-BACKEND-ARCHITECTURE.md`, `docs/DEVOPS/`.
