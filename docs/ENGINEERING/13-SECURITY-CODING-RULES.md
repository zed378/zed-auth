# 13 - Security Coding Rules

> Category: **Engineering Practice** (`docs/ENGINEERING/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-13, P1-01…P1-13, P2-01…P2-07, P3-01…P3-08, P4-01…P4-06 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

The rules that exist because breaking one is an incident rather than a bug. Each is stated
with the failure it prevents and where it is enforced.

The threat model is [`../SECURITY/`](../SECURITY/); this is the coding-level distillation.

## Scope

`backend/`, and the console where a rule crosses the boundary.

## As Built

### 1. Authorization is enforced server-side, every request, independently of any UI

A hidden button is not a control. A route guard is not a control. Both exist, both are
user experience, and both say so in a comment at the point somebody would be tempted to
rely on them.

*Prevents:* an administrator who edits a claim, or calls the API directly, getting what the
console refused to show them.
*Enforced by:* `backend/internal/management/policy.go` plus
`backend/internal/management/architecture_test.go`, which reads the source of every other
package and fails if any of them decides a permission.

### 2. A delegated role assignment is re-validated against the grant on **every** request

Not only at creation. The grant may have narrowed or been revoked since.

*Prevents:* a receiving organization assigning a role the grant no longer delegates.
*Enforced by:* the store reads the grant `FOR SHARE` in the same transaction, **and** a
trigger repeats every rule for any writer. `P4-02` found its first trigger fired on
`project_grant_id` only, so a delegated row could have been widened by the direct `PATCH`;
it now fires on every column the rule reads.

### 3. Visibility is not authority

A row you can see is not a row you may act through. Every query filters on the tenant
column that expresses *authority*, even where RLS already bounds what is visible.

*Prevents:* the receiving side of a Project Grant revoking it; a receiving-side list
showing the grants that organization *made*.
*Enforced by:* explicit filters, and mutation tests that widen them and watch a test go
red (`P4-01`, `P4-06`).

### 4. Another tenant's resource answers 404, never 403

*Prevents:* an attacker confirming that an id names something real.
*Enforced by:* the `NotFound` class, used for "not visible in the caller's scope" as well
as "does not exist". `backend/tests/security/` asserts it.

### 5. Never log a credential

Tokens, passwords, secrets, codes, verifiers, cookies, private keys, recovery codes, and
the raw attributes sent to `/v1/authz/check`.

*Enforced by:* redaction inside the logger by attribute key, plus a gate that greps for
request material passed positionally. See
[`12-LOGGING-CONVENTIONS.md`](./12-LOGGING-CONVENTIONS.md).

### 6. Parameterised SQL only

Including where the value is trusted. `set_config('app.current_org_id', $1, true)` rather
than `SET LOCAL`, precisely so there is no place in the package where a value reaches a
statement as text.

*Enforced by:* `gosec`, review, `AGENTS.md`.

### 7. The service never holds DDL rights

`auth_app` for the service, `auth_owner` for migrations. A role that can disable row-level
security is not constrained by it.

*Enforced by:* `scripts/check.sh` asserts the service container receives no owner
credentials.

### 8. A secret is shown exactly once, because that is what the service does

The plaintext client secret exists in the `201` response and nowhere else; no endpoint
returns it again. The console's modal is built on that being **true rather than a
convention**, and says the consequence rather than the rule.

*Enforced by:* the schema stores a hash; there is no read path.

### 9. Tokens are verified where they are consumed, and not where it would prove nothing

The service verifies every token it receives. The console does **not** verify the token it
was just handed by the service it is about to call — that would prove nothing it does not
already assume — and nothing it reads from the token is a security decision.

### 10. A cache failure is never an authorization failure

Redis unavailable means a miss, and the decision is made from PostgreSQL.

*Prevents:* a cache outage becoming an access-denial outage.

### 11. Never log, cache, or return a decision computed against inputs that can change per request

The authorization cache holds the **inputs** to a decision, never a decision. Phase 4b is
the reason: once policies read `resource.attributes`, a cached decision would be served for
a resource it was never computed against.

### 12. Every security-sensitive feature ships with an abuse-case test

Numbered in the spec, referenced in the test.

*Enforced by:* `AGENTS.md` rule 7, and the card's Definition of Done.

### 13. A `SECURITY DEFINER` function's own `WHERE` clause is the whole control

RLS does not apply to it. So: fixed `search_path`, `REVOKE ALL … FROM PUBLIC`, an explicit
`GRANT EXECUTE` to `auth_app`, a predicate bounded to the caller's own tenant, and a test
that calls it as somebody else and gets nothing.

`P4-06`'s `received_grant_context` is the pattern.

### 14. Secrets never reach a remote branch

*Enforced by:* `scripts/hooks/pre-commit` locally, gitleaks in CI. If you bypass the hook,
rotate the value — deleting the commit does not un-leak it.

## Rules and Defaults

Every rule above, with its enforcement, is listed in place. The ones a tool cannot check —
1 (partially), 3, 9, 11 — are on the review checklist.

## Verification

- `backend/tests/security/` — isolation, multi-org, full flow, RLS plans, coverage map.
- `backend/internal/management/architecture_test.go` — the authorization negative.
- `scripts/check.sh` — RLS presence, audit append-only, credential scanning, log safety,
  owner-credential separation, `gosec`, `govulncheck`.
- `docs/SECURITY/05-VERIFICATION-AND-REDTEAM-PLAN.md` — the verification plan these serve.

## Not Yet Built / Open Questions

- **`manager_roles` has no tenant-scoped RLS policy at all** — not a weaker one, none. It
  is read *during* permission resolution, before a tenant context can be said to exist, and
  a correct policy would key on the current user, which needs an `app.current_user_id`
  session setting this codebase does not implement. Tracked as `DV-02`; the backlog says it
  closes with `P2-05`, which is separately marked done. The discrepancy and the migration's
  own note are in `docs/DATABASE/02-ROW-LEVEL-SECURITY-POLICIES.md`.
- **No owner notification on manager-role assignment**, so an elevation is visible in the
  audit log and nowhere else at the time it happens.
- **Webhook `secret_hash` cannot support HMAC signing** as specified — a hash cannot sign.
  A plan change is needed before `P4-12`.

## Related Documents

- [`../SECURITY/`](../SECURITY/)
- [`12-LOGGING-CONVENTIONS.md`](./12-LOGGING-CONVENTIONS.md)
- [`14-CODE-REVIEW-CHECKLIST.md`](./14-CODE-REVIEW-CHECKLIST.md)
