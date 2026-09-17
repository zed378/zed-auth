# 01 - Backend Unit & Integration Testing

> Category: **TESTING** (`docs/TESTING/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Detail Go testing conventions, table-driven tests, and PostgreSQL integration testing with `testcontainers-go`.

## Category Mandate

Ensures backend services and database repositories are thoroughly validated against real PostgreSQL instances.

## Key Topics To Specify

- Go standard `testing` package with `stretchr/testify` assertions.
- Table-driven unit test layout (`tt := []struct{...}`).
- Integration tests spinning up real PostgreSQL & Redis containers via `testcontainers-go`.

## Reference Architecture & Specification

Table-Driven Test Example:
```go
func TestAuthenticateUser(t *testing.T) {
    tests := []struct {
        name    string
        email   string
        pass    string
        wantErr bool
    }{
        {"Valid Credentials", "admin@org.com", "pass123", false},
        {"Invalid Password", "admin@org.com", "wrong", true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) { ... })
    }
}
```

## Acceptance Criteria

- [x] Table-driven testing pattern specified.
- [x] Real DB integration container pattern documented.

## Open Questions

None.

## Related Documents

- `docs/ARCHITECTURE/02-BACKEND-ARCHITECTURE.md`
