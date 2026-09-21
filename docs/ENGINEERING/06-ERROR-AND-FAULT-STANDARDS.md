# 06 - Error and Fault Standards

> Category: **Engineering Practice** (`docs/ENGINEERING/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-15, P2-06, P3-10, P4-01…P4-06 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

One error envelope, one mapping from class to status, and a rule about what a caller is
told versus what is logged.

## Scope

`backend/internal/management/errors.go`, and every handler's `faultFrom`.

## As Built

### `Fault` carries the class, the message, and what is *not* said

```go
type Fault struct {
    Class      Class
    Message    string          // for the caller
    Details    []api.ErrorDetail // field-level, mapped back to a form field
    RetryAfter time.Duration
    Reason     string          // for the log, NEVER for the response
}
```

`Reason` is the important field. "You are an `ORG_ADMIN` and this needs `ORG_OWNER`" is
useful to an operator and, told to a caller probing an organization they do not
administer, confirms it exists.

### The class-to-status mapping is a table, not a switch

```go
var mapping = map[Class]struct{ status int; code api.ErrorCode }{
    Unauthenticated: {401, api.UNAUTHENTICATED},
    Forbidden:       {403, api.PERMISSIONDENIED},
    NotFound:        {404, api.NOTFOUND},
    Invalid:         {400, api.VALIDATIONERROR},
    Conflict:        {409, api.CONFLICT},
    RateLimited:     {429, api.RATELIMITED},
    Internal:        {500, api.INTERNAL},
    Unavailable:     {503, api.UNAVAILABLE},
    Reauthenticate:  {403, api.REAUTHENTICATIONREQUIRED},
}
```

A table, so adding a class without deciding its status is a gap you can see rather than a
silent fall-through to 500. One mapping rather than a status literal at each call site,
because the value of a consistent envelope is that a client writes one error path — and a
handler that picks its own status eventually picks a different one for the same condition.

### Four classes exist because the distinction matters to a caller

| Class | Distinct from | Why the difference is real |
|---|---|---|
| `NotFound` | `Forbidden` | Another organization's resource answers **404**, the same as one that does not exist. A 403 would confirm the id names something real |
| `Unavailable` | `Internal` | "A dependency did not answer" is not "a bug happened here". For `/v1/authz/check` it is the difference between "we checked and the answer is no" and "no decision was reached" — the first is cacheable and makes an outage look like a policy change on every dashboard watching the allow/deny ratio |
| `Reauthenticate` | `Forbidden` | Both are 403. The caller can fix one by signing in again, and a client must be able to tell which |
| `Conflict` | `Invalid` | A reused `Idempotency-Key` with a different body, or a uniqueness violation — the request was well-formed and the world disagreed |

### An unrecognised error becomes `Internal` with a fixed message

```go
fault := Fault{Class: Internal, Message: "An unexpected error occurred."}
if !errors.As(err, &fault) { ... }
```

An unexpected error's text is written for a developer and routinely names a table, a
column or a query. None of that belongs in a response.

### Conversion happens at one boundary per package

Each domain package has a `faultFrom` that recognises its own typed errors and returns
everything else untouched. Handlers call it once, on the way out. Nothing converts an
error halfway down, and nothing compares error strings.

`projectgrant` recognises `UnknownRoles` (which carries the offending keys, so the detail
can name them), `NotDelegated`, `ErrRevoked` and `ErrNotFound`.

### One sentence for several causes, when telling them apart would leak

`P4-01` answers an unknown organization, a suspended one, and the granting organization
itself with the **same** message. The console repeats it rather than guessing which
occurred — guessing is how a console tells a user something the server did not say.

### Details map to form fields

```go
Details: []api.ErrorDetail{{Field: "role_keys", Issue: issue}}
```

`docs/UI-UX/15-FORM-UX.md` needs `details[0].issue` to land next to the field that caused
it, so a validation failure is rendered where it can be fixed.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Envelope | `docs/PLAN/05`'s, written in one place | `backend/internal/management/errors.go` |
| Class → status | table | same |
| `Reason` | logged, never returned | same |
| Unrecognised error | `Internal`, fixed message | `WriteError` |
| Another tenant's resource | 404, not 403 | `NotFound` class, abuse case A-3 |
| Conversion point | one `faultFrom` per package | each domain package |
| Error string comparison | never | review |

## Verification

- `backend/internal/management/middleware_test.go` — the envelope and the mapping.
- Every domain package's integration tests assert the **status and the message**, not just
  that an error occurred.
- `backend/tests/security/` — that a cross-tenant request answers 404.

## Not Yet Built / Open Questions

- **No error catalogue for integrators.** The codes are in the OpenAPI spec and rendered
  into the public API reference; there is no prose page listing them with recovery advice.

## Related Documents

- [`05-HANDLER-AND-STORE-TEMPLATES.md`](./05-HANDLER-AND-STORE-TEMPLATES.md)
- [`12-LOGGING-CONVENTIONS.md`](./12-LOGGING-CONVENTIONS.md)
- [`../API/`](../API/)
