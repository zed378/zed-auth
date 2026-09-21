# 01 - Go Coding Standards

> Category: **Engineering Practice** (`docs/ENGINEERING/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-02, P0-15, and every backend card &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

State the Go conventions this repository follows, and the few places it deliberately
departs from common practice.

## Scope

`backend/`, `demo/`.

## As Built

### Idiomatic Go, and no framework in a security path

`chi` and `net/http`, chosen over heavier frameworks because `docs/PLAN/07` wants no
framework magic where a mistake is a security incident. Routing is generated from the
OpenAPI contract (ADR-013), so the router is the documented surface by construction.

`gofmt` is enforced on `backend/cmd`, `backend/internal`, `backend/migrations` and `demo`;
`go vet` runs over everything. There is no `golangci-lint`.

### Comments explain **why**, at length, and that is the house style

This repository's comments are longer than most Go code's, on purpose. The rule is not
"comment everything" — it is that a decision with a non-obvious reason carries the reason
where the next person will meet it.

```go
// Set the tenant before anything else runs. Passing orgID as a parameter
// rather than interpolating it into the statement matters even though it
// comes from trusted code: set_config is the parameterizable form, and
// SET LOCAL is not, so building the SQL by hand would be the one place in
// this package where a value reaches a statement as text.
```

That comment stops someone from "simplifying" the call. A comment that restated the code
would not.

What gets a comment: a defensive check whose absence would be silent, a workaround for a
library's behaviour, a value chosen for a reason (a TTL, a bound, a margin), and every
place where the safe reading and the obvious reading differ.

### Package doc comments carry the package's rule

Every non-trivial package opens with what it is for and what it refuses. `internal/projectgrant`:

> This package creates, reads and revokes the contract, and nothing else. A grant confers
> no access until `P4-02` lets the receiving organization assign the delegated roles and
> `P4-04` puts them in tokens and checks — which is what makes this safe to ship before
> either.

### Packages are named for the domain noun

`organization`, `project`, `application`, `role`, `grant`, `projectgrant`, `session`,
`signing`, `authn`, `authz`. Not `utils`, not `helpers`, not `common`. A package that
cannot be named for what it owns usually should not exist.

The one structural exception is `internal/grantsql`, a dependency-free leaf holding SQL
shared by `internal/authz` and `internal/grant`. It exists because those two packages
would otherwise import each other in tests. A leaf package to break a cycle is a
legitimate shape; a `shared` package that accumulates is not, and the doc comment says
which this is.

### Errors wrap with context and are matched by type

```go
return nil, fmt.Errorf("projectgrant: listing received grants: %w", err)
```

Package prefix, what was being attempted, `%w`. Sentinel errors (`ErrNotFound`,
`ErrRevoked`, `ErrUnknownRoles`) and typed errors carrying data (`UnknownRoles{Keys}`,
`NotDelegated{Keys, Delegated}`) are matched with `errors.Is`/`errors.As` at the boundary
and converted to a `management.Fault` there — never at the call site, and never by string
comparison. See [`06-ERROR-AND-FAULT-STANDARDS.md`](./06-ERROR-AND-FAULT-STANDARDS.md).

### Interfaces are declared where they are consumed, and kept small

`projectgrant.Invalidator` is one method, declared in the package that calls it:

```go
type Invalidator interface {
    InvalidateGrant(ctx context.Context, grantID string)
}
```

It exists as an interface rather than the concrete cache type so that `internal/projectgrant`
does not depend on `internal/authz` — which depends on `internal/role` and `internal/grant`,
and would make the dependency graph a ring. Same for `Observer`.

### Dependencies are added reluctantly

The `demo` module has **no external dependencies at all**, and `scripts/check.sh` fails if
`demo/go.sum` appears: a demonstration of the protocol that needs a library is a
demonstration of the library.

The service's own dependency set is small and deliberate: `chi`, `pgx`/`lib/pq`,
`go-redis`, `oapi-codegen`'s runtime, `google/uuid`. `go mod tidy` is checked, and
`govulncheck` runs over the result.

### Concurrency is rare and explicit

There is very little of it. Tests run with `-race` and shuffled. Where the service does
run concurrent work, the rule is that a goroutine started by a request must not outlive
its context.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Formatting | `gofmt` | `scripts/check.sh` |
| Vet | clean | `scripts/check.sh` |
| Parameterised SQL only | no string-concatenated queries | `AGENTS.md`, review, `gosec` |
| Errors | wrapped with `%w`, matched by type | review |
| `go.mod` | tidy | `scripts/check.sh` |
| Demo module | zero dependencies | `scripts/check.sh` |
| Unit tests | `-race`, shuffled | `scripts/check.sh` |

## Verification

- `scripts/check.sh` — gofmt, vet, tidy, race tests, gosec, govulncheck.
- `backend/internal/management/architecture_test.go` — a source-level check that no
  package outside `management` decides a permission.

## Not Yet Built / Open Questions

- **No `golangci-lint`**, so unused parameters, shadowed variables and a long tail of
  idiom issues are caught by review or not at all.
- **No import-direction test for the backend's layering** beyond the authorization one.

## Related Documents

- [`05-HANDLER-AND-STORE-TEMPLATES.md`](./05-HANDLER-AND-STORE-TEMPLATES.md)
- [`06-ERROR-AND-FAULT-STANDARDS.md`](./06-ERROR-AND-FAULT-STANDARDS.md)
- [`../ARCHITECTURE/02-BACKEND-ARCHITECTURE.md`](../ARCHITECTURE/02-BACKEND-ARCHITECTURE.md)
