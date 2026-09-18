# 01 - Local Development Setup

> Category: **Developer** (`docs/DEVELOPER/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-02, P0-15, P1-27 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Get a working stack, run the tests, and know which command answers which question.

## Scope

Local development. CI and staging are `docs/DEVOPS/`.

## As Built

### Prerequisites

Go (the version in `backend/go.mod`), Node (the version the console and public site expect), Docker with Compose, and Python 3 for a few repository scripts. Integration tests need a working Docker socket: they start real Postgres and Redis through testcontainers.

### The stack

```
bash scripts/e2e-up.sh     # bring up Postgres, Redis, Mailpit and the service,
                           # seed an instance, organization, administrator and
                           # console client, register two demo applications,
                           # and build the console against that stack
source .e2e.env            # the environment it printed
bash scripts/e2e-down.sh   # stop everything
```

`deploy/docker-compose.yml` alone is enough if all you need is a database and the service; `e2e-up.sh` is what produces a stack you can actually sign in to.

Mail is delivered to Mailpit, whose UI is where invitation and reset links are read during development.

### Tests

| Command | Runs |
|---|---|
| `cd backend && go test ./...` | Unit tests |
| `cd backend && go test -tags integration ./...` | Integration tests against real Postgres and Redis |
| `cd console && npm test` | Component tests (Vitest, Testing Library, axe) |
| `cd console && npm run e2e` | Playwright E2E against the running stack (needs `.e2e.env`) |
| `bash scripts/check.sh` | The full local gate, in the order CI runs it |
| `CHECK_FULL=1 bash scripts/check.sh` | Additionally the public-site build with its audits and the E2E suite |

### Regenerating what is generated

The contract drives three artefacts, all committed and all gated:

```
cd backend && go generate ./internal/api/     # server interface
cd console && npm run api:generate            # typed client
cd console && npm run patterns:generate       # validation patterns shared with the server
cd public-site && npm run api:generate        # published API reference
```

A change to `openapi/openapi.yaml` without regenerating fails CI.

## Rules and Defaults

| Rule | Value | Notes |
|---|---|---|
| Integration tests | Behind the `integration` build tag | keeps `go test ./...` fast and Docker-free |
| E2E tests | Need a real stack and a browser | skipped by the default gate |
| Race detector | CI runs it (cgo available); the local gate says when it skips | `scripts/check.sh` § Go |
| Ports | Local services bind their conventional ports; staging binds above 10000 | `deploy/` |

## Security Considerations

- The seeded local administrator and demo clients exist only in the E2E stack and must never be copied into a deployed environment.
- `.e2e.env` contains local credentials: it is generated, not committed.

## Verification

- `scripts/e2e-up.sh` verifies each step as it goes (migrations applied, a token obtainable through the real hosted flow, demo applications healthy) and prints what it seeded.

## Not Yet Built / Open Questions

- There is no devcontainer or single "make dev" entry point; the scripts above are the interface.

## Related Documents

- `docs/DEVOPS/01-ENVIRONMENTS-AND-CONFIGURATION.md`, `docs/TESTING/`.
