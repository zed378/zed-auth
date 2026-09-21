# 12 - Logging Conventions

> Category: **Engineering Practice** (`docs/ENGINEERING/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-09, P1-24 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

How the service logs, and the one rule that is enforced by machinery rather than by
discipline: a credential never reaches a log line.

## Scope

`backend/internal/observability/logging.go`. The **audit log** is a different thing with
different rules — [`../OBSERVABILITY/`](../OBSERVABILITY/).

## As Built

### Structured logging, `log/slog`, attributes not interpolation

```go
log.Info("grant revoked", "grant_id", id, "org_id", orgID)
```

Never `fmt.Sprintf` into the message. Attributes are what the redaction layer can inspect;
an interpolated string is opaque to it.

### Redaction happens in the logger, by attribute key

`observability` installs a `ReplaceAttr` hook that runs on every attribute of every record,
including attributes nested inside groups. Any key on the deny list is replaced with
`[REDACTED]`.

> That rule is enforced by the logger itself rather than by developer discipline, because
> discipline fails silently and a leaked credential in a log is indistinguishable from a
> leaked credential anywhere else.

The deny list covers passwords in every spelling, secrets and client secrets, every token
kind and `token_hash`, `code` / `code_verifier` / `code_challenge`, `authorization`,
`cookie` / `set-cookie`, private keys, `totp_secret`, recovery codes, API keys,
assertions, `saml_response`, and — from `docs/PLAN/13` — the raw `attributes`,
`resource_attributes` and `subject_attributes` sent to `/v1/authz/check`.

Matching is case-insensitive and also matches the **last segment** of a namespaced key, so
`http.request.authorization` is caught as readily as `authorization`.

### `[REDACTED]` rather than an empty value

A fixed string, so a reader can tell "this field was present and withheld" from "this
field was absent". The difference matters when reconstructing an incident.

### What is deliberately *not* redacted, and why

`session_id` was on the list until `P1-11`. The plan originally had the session cookie
carry the row's id, which made the id a bearer credential and redacting it correct.
`PG-14` separated the two: the cookie now carries a token whose hash is stored, and `id`
is an internal identifier the sessions screen displays and the audit log must name.

> Redacting it now would defeat the point of that separation — an audit entry saying a
> session was revoked without saying **which** cannot answer the question it exists for.

The credential is `token_hash`, and that stays redacted. This is the general shape of the
rule: redact the thing that grants access, name the thing that identifies a record.

### A positional argument would bypass all of it

The hook inspects keys. A raw body or header passed positionally has no key, so the gate
greps for it:

```
(slog|log)\.[A-Za-z]+\([^)]*(r\.Header|r\.Body|req\.Header|req\.Body|\.RawQuery)
```

and fails the build if request material reaches a log call that way.

### `IsSensitiveKey` is exported on purpose

So the tests and the CI lint rule share **one** definition of what is sensitive. Two
copies of that list would diverge, and the copy that matters would be the stale one.

### Levels

`Debug` for detail nobody needs in production, `Info` for lifecycle events, `Warn` for
something an operator should look at eventually, `Error` for a request that failed for a
reason the service is responsible for. A client error (a 400, a 403) is not an `Error`:
logging those at error level makes the level meaningless, since a probing client can
generate them at will.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Logger | `log/slog`, structured | `backend/internal/observability/logging.go` |
| Redaction | by attribute key, in the logger | `redactAttr` |
| Redacted marker | `[REDACTED]` | `Redacted` |
| Sensitive-key definition | one, exported | `IsSensitiveKey` |
| Raw request material in a log call | refused | `scripts/check.sh` |
| Tokens, passwords, raw authz attributes | never logged | `AGENTS.md` rule 6, `docs/PLAN/13` |
| A client error | not `Error` level | convention |

## Verification

- `backend/internal/observability/logging_test.go` — every deny-listed key, namespaced
  keys, and attributes nested in groups.
- `scripts/check.sh` — no raw request material in a log call; no credential-shaped tokens
  committed.

## Not Yet Built / Open Questions

- **Tracing is wired but emits no spans** (`backend/internal/observability/tracing.go`).
- **No log-sampling policy.** Volume has not been a problem yet.
- **The deny list is a list.** It catches what it names; a new field with a novel name is
  caught by review, or by the gate only if it arrives through a request object.

## Related Documents

- [`13-SECURITY-CODING-RULES.md`](./13-SECURITY-CODING-RULES.md)
- [`../OBSERVABILITY/`](../OBSERVABILITY/)
