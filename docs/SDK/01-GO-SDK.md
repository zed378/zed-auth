# 01 - Go Client SDK Specification

> Category: **SDK** (`docs/SDK/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Detail Go client library (`pkg/client`) interface for backend service integration.

## Category Mandate

Provides a simple, high-performance Go client for validating tokens and querying permissions.

## Key Topics To Specify

- Client initialization (`client.NewClient(opts)`).
- Method `VerifyToken(ctx, tokenString)`.
- Method `CheckPermission(ctx, req)`.

## Reference Architecture & Specification

Go Usage Example:
```go
client := auth.NewClient(auth.Config{BaseURL: "https://auth.example.com"})
claims, err := client.VerifyToken(ctx, bearerToken)
```

## Acceptance Criteria

- [x] Go SDK package API specified.
- [x] Token verification method documented.

## Open Questions

None.

## Related Documents

- `docs/SDK/00-SDK-ARCHITECTURE.md`
