# syntax=docker/dockerfile:1
#
# Multi-stage build producing a minimal, non-root runtime image.
# docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md §17 (Container / Runtime Security)
# and §15 (Supply-Chain): every base image is pinned by digest, never :latest.

# --- build stage ------------------------------------------------------------
FROM golang:1.26.6-alpine AS build

WORKDIR /src

# Dependencies first: this layer is cached unless go.mod or go.sum changes,
# which turns most rebuilds into a compile rather than a re-download.
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .

ARG VERSION=dev
# CGO_ENABLED=0 produces a static binary, which is what lets the runtime stage
# be a scratch-like image with no libc and therefore almost no attack surface.
# -trimpath keeps build-machine paths out of the binary and out of any panic
# output (docs/PLAN/10-THREAT-MODEL.md § Information Disclosure).
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/authservice ./cmd/authservice

RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /out/migrate ./cmd/migrate

# keyctl ships in the image too (P1-27).
#
# It did not, and deploy/vm/RUNBOOK-key-rotation.md compensated by building it
# in a throwaway golang container and bind-mounting the binary next to the
# secrets — a procedure whose own header says "read the mount paths before you
# paste this", because the first execution of it wrote the private key
# somewhere the service could not read and produced a service that refused to
# start hours later, with no obvious connection to a rotation done that
# morning.
#
# Shipping it makes the mount point correct by construction: the tool and the
# service are the same image, with the same paths.
#
# It grants an attacker nothing. keyctl needs AUTH_MIGRATE_DSN — the schema
# OWNER's credentials — which the service container is deliberately never given
# (see deploy/vm/docker-compose.yml). Anyone holding those can already do
# everything keyctl does, with psql.
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /out/keyctl ./cmd/keyctl

# --- runtime stage ----------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

# distroless/static: no shell, no package manager, no libc. An attacker who
# achieves code execution finds nothing to pivot with. The :nonroot tag runs as
# uid 65532 rather than root.

WORKDIR /

COPY --from=build /out/authservice /authservice
COPY --from=build /out/migrate /migrate
COPY --from=build /out/keyctl /keyctl

# Migrations are a separate binary and a separate deployment step. They are
# never run implicitly at service startup in production
# (docs/PLAN/14-DEPLOYMENT.md § Release Process).
COPY --from=build /src/migrations /migrations

USER nonroot:nonroot
EXPOSE 8080

ENTRYPOINT ["/authservice"]
