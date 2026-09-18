# 01 - Environments and Configuration

> Category: **DevOps** (`docs/DEVOPS/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-02, P0-13, P0-14 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

List the environments that exist, what differs between them, and how the service is configured.

## Scope

Environment topology and configuration keys. Secret handling is `04-SECRET-MANAGEMENT.md`.

## As Built

| Environment | What it is | Brought up by |
|---|---|---|
| Local development | Postgres, Redis, Mailpit and the service in containers | `deploy/docker-compose.yml` |
| E2E stack | The local stack plus a seeded bootstrap, two demo applications and a console build | `scripts/e2e-up.sh`, torn down by `scripts/e2e-down.sh` |
| CI | Ephemeral containers per job; integration tests use testcontainers | `.github/workflows/ci.yml` |
| Staging | A single private VM behind a Cloudflare tunnel | `deploy/vm/docker-compose.tunnel.yml` |
| Production | Does not exist yet | — |

### Configuration

All configuration is environment variables read by `backend/internal/config/config.go`, prefixed `AUTH_`. The loader distinguishes required from optional values and fails startup on a missing required one, rather than defaulting silently.

Representative keys (the file is the complete list):

| Key | Default | Meaning |
|---|---|---|
| `AUTH_ENV` | `local` | Environment name; some behaviour is stricter outside local |
| `AUTH_ISSUER` | required | The issuer URL every token and discovery document carries |
| `AUTH_HTTP_ADDR` | `:8080` | Public listener |
| `AUTH_HTTP_READ_HEADER_TIMEOUT` | 5s | Header read timeout |
| `AUTH_HTTP_READ_TIMEOUT` | 15s | Request read timeout |
| `AUTH_HTTP_WRITE_TIMEOUT` | 30s | Response write timeout |
| `AUTH_HTTP_IDLE_TIMEOUT` | 60s | Keep-alive idle timeout |
| `AUTH_HTTP_SHUTDOWN_TIMEOUT` | 20s | Graceful shutdown budget |
| `AUTH_TRUST_PROXY_HEADERS` | `false` | Whether `X-Forwarded-*` may be believed — a claim the deployment makes about itself |
| `AUTH_CLIENT_IP_HEADER`, `AUTH_TRUSTED_PROXY_CIDRS` | empty | Client-IP resolution when behind a proxy |
| `AUTH_SMTP_URL`, `AUTH_MAIL_FROM`, `AUTH_SMTP_ALLOW_CLEARTEXT` | empty / `false` | Mail delivery |

Database, Redis, signing and MFA-seal settings follow the same pattern; secret material is supplied as a reference to a file or secret store rather than inline (`04-SECRET-MANAGEMENT.md`).

### What differs between environments

- **Trust in proxy headers** is off unless the deployment sets it; on staging the tunnel terminates TLS, so it is enabled there deliberately.
- **Mail**: Mailpit locally and in E2E; a real SMTP endpoint elsewhere.
- **Origins**: allowed CORS origins are per registered application, so each environment's console has its own registration.
- **Image tag**: staging pins `AUTH_IMAGE` to the tag built for the task being deployed.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Missing required configuration | Startup fails loudly | `backend/internal/config/config.go` |
| Runtime state on staging | Outside the git checkout, so a re-clone cannot destroy keys or backups | `deploy/vm/`, operational notes |
| The application's database role | Restricted; owner credentials go only to `migrate` | gate in `scripts/check.sh` |

## Security Considerations

- Configuration is the place where a wrong value is silently insecure: trusting proxy headers when nothing strips them, or allowing cleartext SMTP. Both are explicit booleans that default to the safe value.
- No environment file is committed. `deploy/vm/env.example` documents the shape without values.

## Verification

- `backend/internal/config/*_test.go` — required/optional handling and defaults.
- `scripts/check.sh` § Deployment — compose files parse, the service container receives no owner credentials.

## Not Yet Built / Open Questions

- No production environment, and therefore no environment-promotion process beyond "staging, then eventually Kubernetes".

## Related Documents

- `docs/PLAN/14-DEPLOYMENT.md`; `02-CONTAINERIZATION-AND-KUBERNETES.md`; `04-SECRET-MANAGEMENT.md`.
