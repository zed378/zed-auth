# 04 - Gateway HTTP Middleware Specification

> Category: **SDK** (`docs/SDK/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Detail HTTP gateway middleware for Go Chi, Express.js, and Next.js API routes.

## Category Mandate

Allows downstream microservices to validate tokens and enforce authorization headers in 3 lines of code.

## Key Topics To Specify

- Go Chi Middleware: `r.Use(authmiddleware.RequireAuth(client))`.
- Express Middleware: `app.use(authMiddleware({ domain: '...' }))`.
- Extracts Bearer token, verifies signature against JWKS, and injects user context into request context.

## Reference Architecture & Specification

Middleware Execution Sequence:
`Incoming Request -> Extract Bearer Token -> Verify JWKS Signature -> Populate Context (User, Org, Roles) -> Next Handler`

## Acceptance Criteria

- [x] Middleware API contracts for Go and Node.js specified.
- [x] Context injection rules documented.

## Open Questions

None.

## Related Documents

- `docs/API/02-AUTHENTICATION-AND-AUTHORIZATION.md`
