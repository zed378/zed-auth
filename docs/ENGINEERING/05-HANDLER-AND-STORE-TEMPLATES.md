# 05 - Handler and Store Templates

> Category: **Engineering Practice** (`docs/ENGINEERING/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-15…P1-19, P2-05, P4-01…P4-06 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

Give the shape every Management API endpoint takes, so a new one is written by following a
pattern rather than by inventing one. The worked example throughout is `P4-06`, the most
recent endpoint added.

## Scope

`backend/internal/<domain>/`, `backend/internal/management/`.

## As Built

### The layers a request passes through

```
generated router (from openapi.yaml)
  → management.Chain     auth → rate limit → idempotency → audit guard
    → Handler method     decode, scope, call the store, render
      → Store method     SQL inside a tenant transaction
        → PostgreSQL     RLS, triggers, constraints
```

Every layer below the handler is reachable only through it, and every rule that matters is
enforced at **more than one** of them. A subset check lives in the store *and* in a
trigger, because a future writer that bypasses the store still meets the trigger.

### 1. The contract first

Add the path, the `operationId`, the schemas and the responses to `openapi/openapi.yaml`;
add the path to `scripts/openapi-shipped-paths.py`; run `make openapi-generate`. The
generated interface now has a method the server does not implement, and the build fails
until it does. That is the intended order — the contract is not documentation of the
handler, it is its declaration.

### 2. The policy row

```go
// The receiving side's own list of what was delegated to it (P4-06).
// ORG_ADMIN over the organization in the path: this is a view of the
// organization's own affairs, not of one grant, so it is not grant-scoped.
"GET /v1/organizations/{org_id}/project-grants": {Role: OrgAdmin, Scope: ScopeOrganization},
```

One row in `backend/internal/management/policy.go`. There is no second place a permission
is decided, and `architecture_test.go` reads the source of every package to prove it.

### 3. The handler method

```go
func (h *Handler) ListReceivedProjectGrants(
    ctx context.Context, request api.ListReceivedProjectGrantsRequestObject,
) (api.ListReceivedProjectGrantsResponseObject, error) {
    size := management.PageSize(intParam(request.Params.PageSize))
    cursor, err := management.DecodeCursor(stringParam(request.Params.PageToken))
    if err != nil {
        return nil, err
    }

    var rows []Received
    if err := h.inScope(ctx, func(tx *postgres.Tx, orgID string) error {
        var err error
        rows, err = h.Grants.ListReceived(ctx, tx, orgID, cursor, size)
        return err
    }); err != nil {
        return nil, faultFrom(err)
    }

    page, err := management.Paginate(rows, size, func(r Received) management.Cursor {
        return management.Cursor{After: r.CreatedAt, ID: r.ID}
    })
    ...
}
```

The shape, in order:

1. **Decode the request** with the shared helpers (`PageSize`, `DecodeCursor`). No handler
   parses a page token itself.
2. **`inScope`** opens the tenant transaction and hands the handler the organization id.
   It refuses outright if the request reached here with no scope, because that is a bug in
   the chain rather than a client error.
3. **Call the store.** The handler holds no SQL.
4. **`faultFrom`** converts the package's typed errors into a `management.Fault` at this
   one boundary.
5. **Paginate and render.** Rendering is a pure function from the store type to the
   generated API type.

A write adds `h.audit(...)` inside the same transaction and, where a cache is affected,
`h.Cache.InvalidateGrant(...)` **after** the commit.

### 4. The store method

```go
func (s *Store) ListReceived(
    ctx context.Context, tx *postgres.Tx, grantedOrgID string, after management.Cursor, size int,
) ([]Received, error) {
```

Rules the stores follow:

- **Takes a `*postgres.Tx`, never the pool.** The transaction carries the tenant; a store
  that opened its own connection would run outside RLS.
- **Filters explicitly on the tenant column even though RLS is on.** RLS bounds what is
  *visible*; the query decides what is *relevant*. For `project_grants` both parties can
  see a row, so a receiving-side list filters on `granted_org_id` — visibility is not
  authority, and the mutation that widens this filter turns a test red.
- **`size+1`** rows fetched, so `Paginate` can tell whether another page exists without a
  count.
- **Errors wrapped** with the package prefix and what was attempted.
- **Secondary lookups are batched**, never per row. `ListReceived` resolves names and
  holder counts with one query each over the page's ids.

### 5. Errors become faults in one place

```go
func faultFrom(err error) error {
    var fault management.Fault
    if errors.As(err, &fault) { return err }
    var unknown UnknownRoles
    if errors.As(err, &unknown) { ... }
    if errors.Is(err, ErrNotFound) { ... }
    return err
}
```

Anything unrecognised falls through as an internal error, which the middleware renders
without detail. See [`06-ERROR-AND-FAULT-STANDARDS.md`](./06-ERROR-AND-FAULT-STANDARDS.md).

### 6. The interface entry

A handler is reachable only after its method is added to the surface interface in
`backend/internal/httpserver/server.go` and the concrete type is wired in
`backend/cmd/authservice/main.go`. The compiler enforces the first; the second is where
optional collaborators (cache, metrics observer) are attached.

### Optional collaborators are interfaces declared by the consumer

```go
type Invalidator interface{ InvalidateGrant(ctx context.Context, grantID string) }
type Observer interface{ ProjectGrantChanged(action string) }
```

Both are nil-safe: nil means the cache TTL is the only mechanism, which is slower to take
effect and never wrong. Declaring them here rather than importing `internal/authz` keeps
the dependency graph acyclic.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Contract before code | required | generated interface, `scripts/check.sh` |
| One policy row per route | required | `backend/internal/management/policy.go` |
| Permission decided outside `management` | refused | `backend/internal/management/architecture_test.go` |
| Store takes a `*postgres.Tx` | required | signature |
| Tenant filter in the query as well as RLS | required | review, mutation testing |
| Page fetch | `size + 1` | `management.Paginate` |
| Audit inside the transaction | required | `management.Audit` |
| Cache invalidation after commit | required | `backend/internal/projectgrant/handler.go` |

## Verification

- `backend/internal/management/architecture_test.go` — nothing outside `management`
  decides a permission.
- `backend/internal/management/page_test.go`, `page_integration_test.go` — pagination.
- Each domain package's `*_integration_test.go` — the endpoint through the real chain
  against real Postgres.

## Related Documents

- [`06-ERROR-AND-FAULT-STANDARDS.md`](./06-ERROR-AND-FAULT-STANDARDS.md)
- [`07-DATABASE-ACCESS-STANDARDS.md`](./07-DATABASE-ACCESS-STANDARDS.md)
- [`../API/`](../API/)
