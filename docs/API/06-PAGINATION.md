# 06 - Pagination & Sorting

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Define cursor-based and offset-based pagination formats for list endpoints.

## Category Mandate

Ensures efficient database querying and consistent collection responses.

## Key Topics To Specify

- Opaque base64 encoded cursor parameters (`page_token`).
- Page size limit parameters (`page_size`, default 50, max 250).
- Response structure containing `next_page_token` and `total_count`.

## Reference Architecture & Specification

```json
{
  "items": [...],
  "next_page_token": "eyJpZCI6IjEyMyIsImNyZWF0ZWRfYXQiOjE3ODk1NzQ0MDB9",
  "total_count": 1250
}
```

## Acceptance Criteria

- [x] Cursor pagination format specified.
- [x] Maximum page size enforced.

## Open Questions

None.

## Related Documents

- `docs/API/01-API-STANDARDS.md`
