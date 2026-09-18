# 04 - Error Handling

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-16, P1-15 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Document the one error envelope every `/v1` non-2xx response uses, the fixed set of error codes, and how a class of failure maps to an HTTP status.

## Scope

The `/v1` `Error` envelope only. The OAuth/OIDC protocol endpoints use a **different** shape (`OAuthError`, RFC 6749 §5.2) because that is what a consumer's OAuth client library parses — see `08-AUTH-API.md`. Rate-limit-specific headers are `05-RATE-LIMITING.md`; idempotency conflicts are `07-IDEMPOTENCY.md`.

## As Built

**This is not RFC 7807.** Every `/v1` error response is `openapi/openapi.yaml`'s `Error` schema — an object with a nested `error` object carrying `code`, `message`, and an optional `details` array — not `type`/`title`/`status`/`detail`/`instance`. There is no `type` URI, no `instance` pointer, and no plan document proposes moving to RFC 7807; documenting one would describe a format this API does not send.

**One mapping, one place.** `backend/internal/management/errors.go` defines a `Class` (an internal enum: `Unauthenticated`, `Forbidden`, `NotFound`, `Invalid`, `Conflict`, `RateLimited`, `Internal`, `Unavailable`, `Reauthenticate`) and a single table (`mapping`) from each class to an HTTP status and an `api.ErrorCode`. A handler raises a `Fault{Class, Message, Details, ...}`; `WriteError` renders it. There is no second code path that writes an error response by hand — this is enforced by convention (every handler returns through the generated `StrictServerInterface`) rather than a test, unlike the permission table in `02`.

**`message` is safe to show a user; `Reason` never leaves the process.** A `Fault` carries an internal `Reason` string (e.g. `"you are an ORG_ADMIN and this needs ORG_OWNER"`) that is logged but never serialized into the response — telling a caller *why* they were refused in that much detail would, for a 403, confirm to a caller probing an organization they do not administer that it exists (`docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §2). `message` is deliberately generic and constant per class/operation.

**Every response carries `Content-Type: application/json` and `Cache-Control: no-store`.** A `RateLimited` fault additionally carries `Retry-After` (seconds, rounded up — RFC 9110's `delay-seconds` is an integer, and rounding down would tell a client to retry fractionally too early and be refused again).

## Rules and Defaults

| Error code | HTTP status | Meaning | Enforced in |
|---|---|---|---|
| `UNAUTHENTICATED` | 401 | No credential, or one that cannot be used | `backend/internal/management/errors.go` |
| `PERMISSION_DENIED` | 403 | Authenticated, lacking the required role, and the target is already visible to the caller | `errors.go`, `roles.go` |
| `NOT_FOUND` | 404 | No such resource, or one not visible in the caller's scope (see `02-AUTHENTICATION-AND-AUTHORIZATION.md` on 404-vs-403) | `errors.go`, `roles.go` |
| `VALIDATION_ERROR` | 400 | Malformed request or a field fails validation; `details[]` names the field | `errors.go` |
| `CONFLICT` | 409 | An `Idempotency-Key` reused with a different body, a uniqueness violation, or a state conflict (e.g. deleting a role still referenced by a grant) | `errors.go` |
| `RATE_LIMITED` | 429 | Over the caller's request quota | `errors.go`; see `05-RATE-LIMITING.md` |
| `INTERNAL` | 500 | Anything else; message is a fixed, generic string | `errors.go` |
| `UNAVAILABLE` | 503 | No decision was reached (a dependency did not answer) — distinct from `INTERNAL`, and the caller may retry. Used by `/v1/authz/check` when the decision engine cannot be reached; never conflated with "checked and denied" | `errors.go` |
| `REAUTHENTICATION_REQUIRED` | 403 | Allowed in principle, but the caller's session is too old for an action that changes how the account is protected (MFA enrolment/removal). Distinct from `PERMISSION_DENIED` because the remedy is in the caller's hands: sign in again | `errors.go`; see `14-SESSION-API.md` |

## Interfaces

`Error` schema (`openapi/openapi.yaml`):

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Field 'email' is invalid",
    "details": [
      { "field": "email", "issue": "email format is not valid" }
    ]
  }
}
```

`details[]` (`ErrorDetail`) is present for `VALIDATION_ERROR` where naming the bad field is helpful and discloses nothing the caller did not already send; it is generally absent for every other code. Every `04`–`16` document lists the specific error cases an endpoint can return; this document is the shared vocabulary they draw from.

## Security Considerations

- `message` never contains a token, a password, a raw `/v1/authz/check` resource attribute, a stack trace, or a SQL fragment (`openapi/openapi.yaml` `Error.error.message` description; `docs/PLAN/13-OBSERVABILITY.md`; `CLAUDE.md`).
- Error codes are deliberately coarse — there is no `USER_NOT_FOUND` distinct from `PERMISSION_DENIED` — because a code per failure mode is an enumeration oracle (`docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §12).
- `UNAVAILABLE` vs. an `AuthorizationDecision{allowed:false}` on `/v1/authz/check`: treating a `503` as "allowed" would be the failure mode this distinction exists to prevent, and treating it as cacheable would make an outage look like a policy change. See `12-ROLE-AND-PERMISSION-API.md`.

## Verification

- `TestEveryRefusalUsesTheEnvelope` — `backend/internal/management/chain_integration_test.go`.
- Per-endpoint error-case tests are named in each resource document (`08`–`16`) and their corresponding `*_integration_test.go` files.

## Not Yet Built / Open Questions

None — the envelope and code set are stable and have not changed since `P0-16`.

## Related Documents

- `openapi/openapi.yaml` `components/schemas/Error`, `ErrorCode`, `ErrorDetail`, `components/responses/*`
- `docs/API/05-RATE-LIMITING.md`, `docs/API/07-IDEMPOTENCY.md`
- `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`
