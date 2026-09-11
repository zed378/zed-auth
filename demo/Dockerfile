# syntax=docker/dockerfile:1
#
# One image, two binaries: the demo consumer applications (P1-26).
#
# The same posture as the service's own image — distroless, non-root, static —
# because these applications are the reference a consumer team copies, and a
# reference implementation that runs as root in a Debian base teaches that too.
#
# Which application runs is chosen by the compose entrypoint, not by an
# environment variable inside a single binary. Two hostnames, two client IDs,
# two containers: `docs/PLAN/01`'s definition of done says SSO has to be proven
# across genuinely separate applications, and one process serving both would
# quietly share a session and prove nothing.

# --- build stage ------------------------------------------------------------
FROM golang:1.26.6-alpine AS build

WORKDIR /src

# No go.sum: this module has no dependencies outside the standard library, on
# purpose (see go.mod). If one is ever added, this COPY has to grow a go.sum
# and `go mod verify` alongside it.
COPY go.mod ./
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/webapp ./webapp
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/spa ./spa

# The tests run in the image build, not only on a developer machine. These are
# the SSO fixture for P1-27 — an image that ships with a broken verifier would
# turn every E2E failure into a hunt in the wrong repository.
RUN go vet ./... && go test ./...

# --- runtime stage ----------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

WORKDIR /

COPY --from=build /out/webapp /webapp
COPY --from=build /out/spa /spa

USER nonroot:nonroot

# No ENTRYPOINT. Each service in deploy/demo/docker-compose.yml names the
# binary it runs, so the image cannot be started ambiguously.
