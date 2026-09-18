# 02 - Privacy-Preserving Logging

> Category: **Observability** (`docs/OBSERVABILITY/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-09 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Specify the rule that no token, password, or raw `/v1/authz/check` resource attribute reaches a log line or an audit event, how it is enforced in code rather than left to developer discipline, and how it is checked in CI.

## Scope

Covers the redaction applied to operational (`slog`) logs and, by shared code, to audit event payloads (`backend/internal/audit/audit.go`'s `redactPayload`, described in `01-AUDIT-LOGGING-SPECIFICATION.md`). Does **not** cover span attributes on distributed traces — see `04-DISTRIBUTED-TRACING.md` for why that boundary is currently a documentation-only rule rather than an enforced one.

## As Built

**Redaction by key, at the handler level, not by discipline.** `backend/internal/observability/logging.go` defines `sensitiveKeys`, a fixed set of attribute keys (`password`, `password_hash`, `new_password`, `current_password`, `secret`, `client_secret`, `client_secret_hash`, `token`, `access_token`, `refresh_token`, `id_token`, `id_token_hint`, `token_hash`, `code`, `code_verifier`, `code_challenge`, `authorization`, `cookie`, `set-cookie`, `private_key`, `privatekey`, `totp_secret`, `recovery_code`, `recovery_codes`, `api_key`, `apikey`, `credential`, `credentials`, `assertion`, `saml_response`, `attributes`, `resource_attributes`, `subject_attributes`). `IsSensitiveKey` matches case-insensitively and also matches the last segment of a namespaced key (`.`, `/`, `:`, `-` separated), so `http.request.authorization` is caught as readily as `authorization`.

`redactAttr` is installed as `slog.HandlerOptions.ReplaceAttr` in `NewLogger`, so it runs on **every** attribute of **every** record, including attributes nested inside `slog` groups — this is a property of the handler, not of each call site remembering to redact. A matched key's value is replaced with the fixed string `"[REDACTED]"` rather than emptied, so a reader can tell "this field was present and withheld" from "this field was absent" — the distinction matters when reconstructing an incident.

**`session_id` is deliberately not on the list.** Before `P1-11` the session cookie carried the row's own id, making the id itself a bearer credential. `PG-14` separated the two: the cookie now carries an opaque token whose hash is stored (`token_hash`, which stays redacted), and `id` is an internal identifier the sessions screen displays and the audit log must be able to name. Redacting it now would defeat the point of that separation.

**Correlation without a tracing backend.** Every log record can carry a `request_id` (always, via `httpserver.RequestID` middleware → `observability.WithRequestID`) and a `trace_id` (only when tracing is enabled and a span is active — which today it is not; see `04-DISTRIBUTED-TRACING.md`). This is done by a `contextHandler` wrapping the base `slog.Handler`, so no call site has to remember to attach either.

**CI enforcement, not just code review.** `scripts/check.sh` § Security greps every tracked `.go` file under `backend/cmd` and `backend/internal` for a log call whose arguments include `r.Header`, `r.Body`, `req.Header`, `req.Body`, or `.RawQuery` passed positionally — the one class of leak the key-based redaction cannot catch, because a raw `http.Header` or body passed as a single log value never goes through per-attribute redaction. A match fails the gate.

**Log levels.** `NewLogger` parses `debug`/`info`/`warn`/`error` (anything else falls back to `info`). `docs/PLAN/13-OBSERVABILITY.md` § Logging asks that an expected authentication failure (wrong password) be logged at `WARN`, not `ERROR`. In the code as it stands, authentication outcomes are recorded through the **audit log** (`user.login.failed`) and **metrics** (`auth_login_attempts_total{outcome="failure"}`) rather than through an operational `slog` line — `backend/internal/login` contains no `log.Warn`/`log.Error` calls for a wrong password. `log.Error` calls elsewhere in the tree are reserved for system-side failures (a closed database, a failed admin listener, a secret that could not be resolved) — see `backend/cmd/authservice/main.go`. The level-separation rule is therefore honored by the absence of an operational log line for a routine auth failure rather than by a `WARN`-level line for one.

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Redacted-value placeholder | `"[REDACTED]"` (never emptied) | `backend/internal/observability/logging.go` `Redacted` |
| Key matching | Case-insensitive; last segment of `.`/`/`/`:`/`-` separated keys | `backend/internal/observability/logging.go` `IsSensitiveKey` |
| Redaction scope | Every `slog` attribute, including nested groups | `redactAttr` installed as `slog.HandlerOptions.ReplaceAttr` |
| `session_id` | Not redacted, deliberately | `backend/internal/observability/logging.go` (comment on `sensitiveKeys`) |
| Raw request material in log calls | Refused | `scripts/check.sh` § Security (grep gate) |
| Default log format | JSON | `backend/internal/observability/logging.go` `NewLogger` |

## Interfaces

- `observability.IsSensitiveKey(key string) bool` — the single shared definition used by the logger, the audit writer, and the CI lint rule.
- `observability.WithRequestID` / `RequestIDFromContext`, `WithTraceID` / `TraceIDFromContext` — correlation ID plumbing.
- `scripts/check.sh` § Security — the grep-based gate; run via `make check` / `scripts/check.sh`.

## Security Considerations

- **Positional logging of raw request material** bypasses per-attribute redaction entirely; this is why the CI gate exists as a second, independent mechanism rather than relying on the redaction hook alone (`docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` Information Disclosure).
- **A credential logged before this mechanism existed** would already be in a log aggregator's retention window; the mechanism prevents new leaks, it does not retroactively scrub anything.
- **The redaction list is a fixed, hand-maintained set of keys.** A new secret-shaped field introduced under an unlisted key name would not be redacted until the list is updated — this is a coverage gap inherent to a denylist approach, not a defect in the implementation of the approach chosen.

## Verification

- `backend/internal/observability/logging_test.go` — `TestNewLogger_EmitsValidJSON`, `TestNewLogger_RedactsSensitiveKeys`, `TestIsSensitiveKey_MatchesNamespacedKeys`, `TestNewLogger_RedactsInsideGroups`, `TestNewLogger_AttachesCorrelationIDsFromContext`, `TestNewLogger_NoCorrelationIDsWhenAbsent`, `TestNewLogger_LevelFiltering`, `TestNewLogger_UnknownLevelFallsBackToInfo`.
- `backend/internal/audit/audit_integration_test.go` — `TestPayloadIsRedactedBeforeStorage` (the audit half of the same rule).
- `backend/internal/management/audit_test.go` — `TestNoRequestContentReachesAnEventByDefault`.
- `backend/tests/security/isolation_test.go` §13 (Credential stuffing / token leakage) references `login.TestNoPasswordReachesTheAuditLog` and the CI gate together as the verification for this category (`MEMORY/records/2026-09-11-P1-28-threat-model-review.md`).

## Not Yet Built / Open Questions

- Span attributes are not covered by this mechanism at all — see `04-DISTRIBUTED-TRACING.md`. If tracing is ever wired to actually emit spans, the same key-based redaction does not automatically apply to `attribute.KeyValue` pairs passed to `StartSpan`.
- No automated check confirms the redaction key list stays in sync with every field name used across the codebase; it is maintained by inspection.

## Related Documents

- `docs/PLAN/13-OBSERVABILITY.md` § Logging
- `docs/PLAN/09-SECURITY.md` § Passwords
- `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §16 (Secret exposure), §13 (Credential stuffing / token leakage)
- [`01-AUDIT-LOGGING-SPECIFICATION.md`](./01-AUDIT-LOGGING-SPECIFICATION.md), [`04-DISTRIBUTED-TRACING.md`](./04-DISTRIBUTED-TRACING.md)
