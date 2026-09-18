# 02 - Containerization (and Kubernetes)

> Category: **DevOps** (`docs/DEVOPS/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: P0-02, P0-13 built; Kubernetes unbuilt (ADR-011) &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describe the images this project builds, the properties they are built for, and the state of the intended Kubernetes target.

## Scope

Images and orchestration. Environments are `01-ENVIRONMENTS-AND-CONFIGURATION.md`; deployment order is `00-DEVOPS-OVERVIEW.md`.

## As Built

### The service image (`backend/Dockerfile`)

- **Multi-stage.** A `golang:1.26.6-alpine` build stage compiles with `CGO_ENABLED=0`, `-trimpath` and `-ldflags="-s -w"`, producing a static binary; build-machine paths therefore never appear in the binary or in panic output.
- **Dependencies are downloaded and verified** in their own layer (`go mod download && go mod verify`), so most rebuilds are a compile rather than a re-download.
- **Base images are pinned**, never `:latest` — the supply-chain rule from `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §15.
- **The runtime stage is minimal and runs as a non-root user**, which is §17's container-security requirement.
- The same build also produces the `migrate` binary, so the migration entrypoint ships with the service rather than being built separately.

### Front-end artefacts

The console and the public site are built to static files and served by nginx (`deploy/console`, `deploy/public-site`). On staging they are bind-mounted from the runtime state directory rather than baked into an image, so a redeploy of static assets does not require a rebuild. A bind mount resolves to an inode: unpacking to a new directory and moving it into place leaves the container serving the directory that was moved away, so artefacts are extracted **over** the existing directory (or the container is recreated).

### Compose

- `deploy/docker-compose.yml` — local development: Postgres, Redis, Mailpit, the service.
- `deploy/vm/docker-compose.yml` and `docker-compose.tunnel.yml` — staging, the latter exposing services only through the Cloudflare tunnel.
- `deploy/vm/docker-compose.metrics-port.yml` — an overlay that exposes the admin/metrics listener.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Base images | Pinned, not `:latest` | `backend/Dockerfile`, gate in `scripts/check.sh` |
| Runtime user | Non-root | `backend/Dockerfile` |
| Binary | Static, stripped, `-trimpath` | `backend/Dockerfile` |
| Compose files | Parse and pass the deployment gate | `scripts/check.sh` § Deployment |
| Image tag in staging | Explicit per release (`AUTH_IMAGE`), never `latest` | the staging environment file (not committed) and the deployment procedure |

## Interfaces

- Build: `docker build -t zed-auth:<tag> -f backend/Dockerfile backend`.
- Run: the compose files above, with configuration from the environment file.

## Security Considerations

- A container that runs as root, or an unpinned base image, would undo controls the rest of the system relies on; both are checked rather than remembered.
- The service container receives no database owner credentials — a dedicated gate asserts it, because "the app can run migrations" is how an application compromise becomes a schema compromise.

## Not Yet Built / Open Questions

- **Kubernetes**: named as the production target in `docs/PLAN/14-DEPLOYMENT.md` and ADR-011, with no manifests, no Helm chart and no cluster. Everything that exists is Compose.
- Open questions for that work: how the signing key reaches a pod without leaving the owner's infrastructure (`docs/PLAN/02` constraint), how migrations run as a job with the owner role, readiness gating during rollout, and whether the admin listener is exposed as a separate service.
- Image scanning in CI is limited to the checks in `scripts/check.sh`; no registry-side scanning exists.

## Related Documents

- `docs/PLAN/14-DEPLOYMENT.md`, `MEMORY/DECISIONS.md` ADR-011.
- `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §15, §17.
