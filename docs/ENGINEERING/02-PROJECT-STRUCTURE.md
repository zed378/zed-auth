# 02 - Project Structure

> Category: **Engineering Practice** (`docs/ENGINEERING/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-01, P0-02, P0-17, P0-18 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

Say where every kind of file lives, across all four deployable surfaces and the
supporting directories.

## Scope

The repository root.

## As Built

```
backend/          the Go service
  cmd/            authservice, migrate, keyctl, passwordhash
  internal/       one package per domain noun
  migrations/     numbered SQL, up and down
  tests/          cross-package suites (security, isolation, RLS plans)
console/          the React management console         → docs/FRONTEND/
public-site/      marketing, docs, generated API reference  → docs/WEBSITE/
demo/             two sample OIDC clients, zero dependencies
openapi/          openapi.yaml — the contract both sides generate from
deploy/           compose files, nginx, runbooks, VM layout
scripts/          the gate, the E2E stack, hooks, load tests
docs/             reference documentation (this)
TASKS/            the roadmap, the board, the backlog
MEMORY/           specs, records, decisions — the audit trail of the work
brand/            brand source assets
```

### Four binaries, and why each is separate

| Binary | Job | Why not part of the service |
|---|---|---|
| `backend/cmd/authservice` | the service | — |
| `backend/cmd/migrate` | apply migrations | Runs as the **owner** role; the service runs as `auth_app` and must never hold DDL rights |
| `backend/cmd/keyctl` | signing-key operations | Touches key material; kept out of the request path entirely |
| `backend/cmd/passwordhash` | hash a password for seeding | A one-shot tool used by `scripts/e2e-up.sh`, so a seed never needs a hashing snippet pasted somewhere |

### `internal/` is one package per domain noun

`account`, `anomaly`, `api` (generated), `application`, `audit`, `auditlog`, `authn`,
`authz`, `config`, `docsdrift`, `grant`, `grantsql`, `httpserver`, `login`, `mail`,
`management`, `mfa`, `mfaapi`, `oauth`, `observability`, `oidc`, `organization`,
`project`, `projectgrant`, `ratelimit`, `role`, `session`, `sessionapi`, `signing`,
`storage`, `testsupport`, `user`.

Three of these are not domain nouns and each earns its place:

- **`api`** — generated from the contract. Nothing is hand-edited in it.
- **`management`** — the Management API's cross-cutting layer: the middleware chain, the
  policy table, pagination, idempotency, the error envelope, and the **only** place a
  permission is decided.
- **`grantsql`** — a dependency-free leaf holding SQL that `authz` and `grant` both need,
  which exists to break an import cycle in tests.

`testsupport` and `docsdrift` are test infrastructure: container startup and factories,
and a test that fails when published documentation drifts from the code it describes.

### A package's files follow one shape

```
projectgrant/
  store.go                        SQL and row mapping
  handler.go                      the lifecycle endpoints
  delegated.go  owners.go  received.go      one file per sub-surface
  *_integration_test.go           behind the `integration` build tag
```

Tests sit beside their subject. Integration tests carry `//go:build integration` and a
package-level `TestMain` that starts containers once.

### Migrations are numbered, paired and immutable once merged

`backend/migrations/<timestamp>_<slug>.{up,down}.sql`, 79 files at the time of writing.
`scripts/check.sh` fails if an `up` has no `down`, and if a destructive migration carries
no expand/contract note.

### `MEMORY/` is the audit trail of the work, not of the system

- `MEMORY/specs/` — the feature spec for a card, written before the code.
- `MEMORY/records/` — what was actually built and what was found, written after.
- `MEMORY/DECISIONS.md` — ADRs, including every deliberate departure from the plan.
- `MEMORY/CHANGELOG.md`.

`docs/` describes the system; `MEMORY/` describes the decisions that produced it. A reader
who wants to know *why the schema looks like this* goes to `MEMORY/`; one who wants to
know *what the schema is* goes to [`../DATABASE/`](../DATABASE/).

### `scripts/` holds things that must run from a fresh clone

Which is why the gate checks the **executable bit in the git index**, not on disk: a clone
gets the index mode, and this surfaced when the VM deployment moved to `git clone` and
`secrets.sh` failed with "command not found" at the moment it was needed to regenerate a
signing key.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Migrations paired | every `up` has a `down` | `scripts/check.sh` |
| Destructive migrations | carry an expand/contract note | `scripts/check.sh` |
| Scripts executable in the index | required | `scripts/check.sh` |
| Integration tests | behind the `integration` build tag | `backend/internal/**/*_integration_test.go` |
| Generated code | `backend/internal/api/`, `console/src/lib/api/*.gen.ts` | `scripts/check.sh` diffs them |
| Demo module | no dependencies | `scripts/check.sh` |

## Verification

- `scripts/check.sh` — migration pairing, executable bits, generated-code diffs.
- `backend/tests/security/` — the cross-package suites that no single package could host.

## Not Yet Built / Open Questions

- **`backend/tests/` and `backend/internal/*_test.go` overlap** in intent. The split is
  "needs several packages" versus "belongs to one", and it is a convention rather than a
  rule anything checks.

## Related Documents

- [`03-NAMING-CONVENTIONS.md`](./03-NAMING-CONVENTIONS.md)
- [`../ARCHITECTURE/02-BACKEND-ARCHITECTURE.md`](../ARCHITECTURE/02-BACKEND-ARCHITECTURE.md)
- [`../FRONTEND/01-APPLICATION-STRUCTURE.md`](../FRONTEND/01-APPLICATION-STRUCTURE.md)
