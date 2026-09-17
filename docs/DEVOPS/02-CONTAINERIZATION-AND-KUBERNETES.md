# 02 - Containerization & Kubernetes Specification

> Category: **DEVOPS** (`docs/DEVOPS/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Detail multi-stage Dockerfile builds, distroless base images, and Kubernetes Deployment manifests.

## Category Mandate

Delivers minimal, secure container images running on Kubernetes.

## Key Topics To Specify

- Multi-stage Docker build (`golang:1.22-alpine` build stage -> `gcr.io/distroless/static-debian12` runtime stage).
- Non-root container execution (`USER 65532:65532`).
- Kubernetes Liveness and Readiness probes (`/v1/admin/health`).

## Reference Architecture & Specification

Dockerfile Example:
```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 go build -o auth-server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /app/auth-server /auth-server
USER 65532:65532
ENTRYPOINT ["/auth-server"]
```

## Acceptance Criteria

- [x] Multi-stage Dockerfile defined.
- [x] Non-root container security enforced.

## Open Questions

None.

## Related Documents

- `docs/DEVOPS/00-DEVOPS-OVERVIEW.md`
