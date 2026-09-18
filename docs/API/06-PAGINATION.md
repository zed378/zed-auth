# 06 - Pagination

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-15, P1-16, P2-02 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Document the cursor pagination every `/v1` list endpoint uses: request parameters, page size bounds, the response envelope, and why there is no `total_count`.

## Scope

Cursor-based pagination as implemented in `backend/internal/management/page.go`. Every list endpoint in `08`–`16` uses this mechanism; none uses offset-based pagination or returns a total count.

## As Built

**Cursor-based only, never offset-based.** An `OFFSET` re-reads and re-skips rows that shifted under concurrent writes, silently duplicating or omitting entries under the exact conditions a user list or an audit log has constantly (`openapi/openapi.yaml` `PageInfo` description). There is no `offset` or `page` query parameter anywhere in this API.

**The cursor carries only a sort position, never a filter.** `Cursor{After time.Time, ID string}` (time-ordered lists) or `Cursor{Key string}` (name-ordered lists — roles only, see below) is base64url-encoded JSON. Every filter (`search`, `event_type`, `actor_id`, `from`/`to`, etc.) comes from the request itself and is re-applied on every page; row-level security applies to the underlying query regardless of what the cursor says. This is deliberate: a cursor that carried its own filter would be a filter the *client* controls, which is the shape of a bug where a crafted token pages past a permission boundary (`page.go`).

**The token is opaque by contract, not by encryption.** Nothing about a page token is a secret; encrypting it would incorrectly suggest it protects something. The thing that protects a page is row-level security on the query, not the token's construction.

**Two sort orders exist**, and each list endpoint's document states which:
- **Time-ordered, newest-or-oldest first** by `(created_at, id)` — the tiebreak on `id` exists because `created_at` alone is not unique, and without it a page boundary is non-deterministic under concurrent inserts. Most lists (organizations, projects, applications, users, sessions, grants) use this, oldest-first; the audit log (`15-AUDIT-LOG-API.md`) is the one list ordered **newest first**, because an audit log is read from the end.
- **Name-ordered**, by `key` — roles only (`12-ROLE-AND-PERMISSION-API.md`). A role list is read as a reference table; somebody looking for `billing-admin` should not have to know when it was created.

**Page size is clamped, never refused.** A requested `page_size` outside `[1, 100]` (or non-numeric, or absent) is silently clamped to the nearest bound or the default of `20` (`page.go` `PageSize`, `DefaultPageSize`, `MaxPageSize`) — failing a list request over a number is unhelpful when the correct behaviour is unambiguous.

**An unreadable page token is a `400`, never a silent restart.** `DecodeCursor` returns `VALIDATION_ERROR` for a token that is not valid base64url, not valid JSON, carries both a time-ordered and a name-ordered position at once, or carries neither. Silently starting over from the first page would look like data loss to a caller mid-way through a list and would be much harder to diagnose than a named parameter error.

**There is no `total_count`.** The response carries `next_page_token`, present only when there is a next page; its absence is the sole valid end-of-collection signal. An empty `items`/resource array on a page is not itself an end-of-collection signal — a page can legitimately return zero items and still have a next page (`Paginate`, `PageInfo` description).

## Rules and Defaults

| Setting | Value | Enforced in |
|---|---|---|
| `page_size` parameter | Optional; default `20`; clamped to `[1, 100]` | `backend/internal/management/page.go` |
| `page_token` parameter | Optional; opaque, base64url; max length `512` at the OpenAPI parameter level | `openapi/openapi.yaml` `components/parameters/PageToken`; `page.go` `DecodeCursor` |
| Sort key | `(created_at, id)` for most lists; `key` for roles | `page.go` `Cursor`, `KeyCursor` |
| Sort direction | Oldest-first, except the audit log (newest-first) | Each resource document |
| Invalid page token | `400 VALIDATION_ERROR` | `page.go` `DecodeCursor` |
| End-of-collection signal | Absence of `next_page_token`; never an empty array | `page.go` `Paginate`; `PageInfo` |
| `total_count` | Does not exist | — |

## Interfaces

Common request parameters (`openapi/openapi.yaml` `components/parameters/PageSize`, `PageToken`):

| Parameter | In | Type | Notes |
|---|---|---|---|
| `page_size` | query | integer, 1–100, default 20 | Server may return fewer than requested |
| `page_token` | query | string, ≤512 chars | The previous response's `next_page_token`; opaque |

Common response envelope fragment (`PageInfo`), embedded in every list response (`OrganizationList`, `UserList`, `ProjectList`, `RoleList`, `ApplicationList`, `GrantList`/`DelegatedGrantList`, `ProjectGrantList`, `SessionList`, `EventList`, `AdministeredOrganizationList`):

```json
{
  "next_page_token": "eyJhIjoiMjAyNi0wOS0xN1QxNjowMDowMFoiLCJpIjoiOGYzZS4uLiJ9"
}
```

`next_page_token` is omitted entirely (not `null`, not empty string) on the last page.

## Security Considerations

- A cursor never encodes a filter or a tenant scope; every request re-applies its own filters and runs under the caller's own row-level-security scope, so a forged or replayed cursor cannot be used to page into another tenant's data (`page.go`).
- Clamping rather than refusing `page_size` avoids a class of validation-error responses that would otherwise leak nothing more than a number was too large, while still bounding one page's cost server-side.

## Verification

- `backend/internal/management/page_test.go` — pure `Cursor`/`DecodeCursor`/`PageSize`/`Paginate` behaviour.
- `backend/internal/management/page_integration_test.go` — pagination against real Postgres, including the concurrent-insert determinism case.

## Not Yet Built / Open Questions

None — this mechanism is uniform across every list endpoint that exists today.

## Related Documents

- `openapi/openapi.yaml` `components/schemas/PageInfo`, `components/parameters/PageSize`, `PageToken`
- `docs/API/15-AUDIT-LOG-API.md` (the one newest-first list)
- `docs/API/12-ROLE-AND-PERMISSION-API.md` (the one name-ordered list)
