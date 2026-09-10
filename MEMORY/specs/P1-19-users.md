# P1-19 — Management API: Users

**Task**: [`TASKS/PHASE-1-MVP-CORE-AUTH-SSO.md` § P1-19](../../TASKS/PHASE-1-MVP-CORE-AUTH-SSO.md)
**Spec required**: Yes — identity data
**Template**: `docs/PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`

---

## 1. Business Objective

The user lifecycle: invite, list, read, update, deactivate, plus the two flows that let a person take possession of an account — accepting an invitation and resetting a password.

This is the task that makes the product usable by somebody who is not a database administrator. Until it lands, every user in every environment was inserted by hand.

## 2. Actors

| Actor | What they may do |
|---|---|
| `ORG_ADMIN` | Invite, list, read, update, deactivate, reactivate, trigger a reset |
| `ORG_OWNER` | The same. There is nothing here that only an owner may do — see §10 |
| `INSTANCE_OWNER` | The same, in any organization, scoped to the one in the path |
| **A person holding an invite or reset token** | Set a password on the account that token names, and nothing else. Unauthenticated |

## 3. Functional Requirements

| # | Requirement |
|---|---|
| FR-1 | `POST /v1/organizations/{org_id}/users` — create, `status: "invited"`, optional `send_invite_email` |
| FR-2 | `GET .../users` — keyset pagination plus a `search` filter over email, username and display name |
| FR-3 | `GET .../users/{user_id}` |
| FR-4 | `PATCH .../users/{user_id}` — profile fields only |
| FR-5 | `POST .../users/{user_id}/deactivate` and `.../reactivate` |
| FR-6 | `POST .../users/{user_id}/password-reset` — an administrator triggering a reset for a known user |
| FR-7 | `GET`/`POST /login/forgot` — the **unauthenticated** self-service request, identical response either way |
| FR-8 | `GET`/`POST /password/set` — the hosted page that consumes an invite or reset token |
| FR-9 | Deactivation terminates sessions and refresh tokens within the same request |
| FR-10 | Per-organization email uniqueness, reported as a conflict |

## 4. Non-Functional Requirements

- The create response matches `docs/PLAN/05`'s worked example field-for-field: `id`, `email`, `status`, `created_at`. Additional fields are additive, and the four documented ones carry exactly the documented meanings.
- Tokens: 32 bytes of CSPRNG, stored as a SHA-256 hash, single-use, expiring. Invite 72 hours, reset 1 hour.
- No user-visible operation waits on email delivery (ADR-018).

## 5. Dependencies

- **`P1-01`** hashing, **`P1-02`** policy — a password set through either flow goes through both.
- **`P1-11`** sessions and **`P1-07`** refresh tokens, for deactivation.
- **`P1-12`** the login page, whose branding, CSRF and notice rendering the hosted set-password page reuses.
- **`P1-13`** rate limiting, for the two email-amplification endpoints.
- **`P1-15`** the chain. **ADR-018** email.
- **`P0-07`** `users` and `user_tokens`, both already shaped for this.

## 6. Database Changes

One additive migration, `20260910000017_email_verified_at`:

- `users.email_verified_at timestamptz` — closes **`PG-18`**. Set when an invitation is accepted through a link sent to that address, which is what verification means; cleared when the address changes, because a verification is a statement about an address and not about a user.

Nullable and unread by the previous version, so a rollback mid-deploy leaves a working schema.

`user_tokens` needs nothing: `P0-07` gave it the `purpose` discriminator, the unique hash index, the expiry index and the `used_at` column.

## 7. API Contract

### The user resource

```json
{
  "id": "…", "email": "budi@company.com", "username": "budi",
  "display_name": "Budi Santoso", "status": "invited",
  "mfa_enabled": false, "email_verified": false,
  "created_at": "…", "updated_at": "…"
}
```

`email_verified` is a boolean derived from `email_verified_at` — the column is a timestamp because "when" answers questions "whether" cannot, and the API exposes the question a caller actually asks.

**No password field exists in any direction.** Not on create, not on update, not in a response. A password is set only by the holder of a token, through the hosted page.

### Create

```
POST .../users  { "email": "…", "username": "…", "display_name": "…", "send_invite_email": true }
→ 201 { …the user…, "invite_email_sent": true }
```

`invite_email_sent` reports what happened rather than what was asked for. A `201` with `false` means the user exists and the administrator must convey the link another way — better than rolling back a user because a mail server was busy (ADR-018).

### Deactivate and reactivate

Verb sub-resources, as `docs/PLAN/05` uses for `:deactivate`. `204` either way. Deactivation is not deletion: `docs/PLAN/05` and the card both prefer it, because a deleted user makes every audit entry naming them unresolvable.

### The unauthenticated flows

```
GET  /login/forgot?request=…   → a form asking for an address
POST /login/forgot             → the same confirmation notice, always
GET  /password/set?token=…     → the hosted form, or a notice
POST /password/set             → sets the password, consumes the token
```

**These are hosted HTML pages, not JSON endpoints, and they are not in the OpenAPI spec.** They belong with `P1-12`'s login page: same branding, same CSRF, same notice rendering, same absence of JavaScript. Putting them under `/v1` would place unauthenticated endpoints inside the authenticated surface, which is how one ends up accidentally exempted from something.

`FR-14`'s API-first rule is not weakened by that. The capability an administrator has — triggering a reset for a user — **is** in the REST API. The capability a person has over their own forgotten password is not a console action at all, so there is no console-only shortcut here to mirror.

## 8. Frontend Changes

The hosted set-password page joins `P1-12`'s login page in `internal/login` — same branding, same CSRF, same notice rendering. `P1-23` builds the console's user screens against this API later.

## 9. Backend Changes

- `internal/mail` — SMTP, per ADR-018.
- `internal/user` — store, handlers, token issuance and consumption.
- `internal/login` — the set-password page and the forgot form.
- `internal/management/policy.go` — seven route entries.
- `internal/config` — `AUTH_SMTP_URL`, `AUTH_MAIL_FROM`.

## 10. Authorization Rules

Every `/v1` user route requires `ORG_ADMIN` over the organization in the path.

**Nothing here is raised to `ORG_OWNER`, and that is a deliberate departure from `P1-17` and `P1-18`.** The reason those raised delete is that it is irreversible; deactivation is not — it is the reversible operation the card asks for *instead* of deletion. Raising it would make the safe action harder than the unsafe one it replaced, and administrators would route around it.

**A user cannot escalate through this API.** `status`, `email_verified`, `id` and `org_id` are refused in an update body the way `P1-18` refuses `type` — from the raw body, because `additionalProperties: false` does not enforce. Roles are not on this surface at all: `manager_roles` is `P2-*`'s, and there is no field here that touches it.

## 11. Tenant scoping

`P1-17`'s rule: the transaction is scoped to the organization in the path, RLS confines every query, and no store method takes an `org_id`.

The unauthenticated flows have no path organization — but they do not need a cross-tenant lookup either, and it is worth saying why, because reaching for a `SECURITY DEFINER` function here would have been the obvious wrong move.

**`/login/forgot` is reached from inside a pending authorization request.** That request names the OIDC client, the client names its organization, and `P1-12`'s login handler already scopes its own password check that way — `WithTenant(pending.App.OrgID)`. The forgot flow is the same flow one step sideways, so it inherits the same tenant for free.

A form reached with no pending request has no tenant, and answers the same confirmation notice as everything else rather than an error. That is not a workaround: an error there would be a signal, and the whole point of this endpoint is that it emits none.

`/password/set` carries a token, and the token row carries `org_id`, so it is scoped by what the token says rather than by what the caller claims.

## 12. Validation

- **Email** — structurally validated, lowercased for storage and comparison. Uniqueness is per organization, case-insensitively.
- **Password** — only ever through `P1-02`'s policy evaluator, which reads the organization's own settings. The API never accepts a hash and never returns one.
- **Search** — matched against email, username and display name with a bounded `ILIKE`; the term is a parameter, never interpolated.

## 13. Abuse Cases

Cross-referenced with `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`.

| # | Abuse case | Control | Test |
|---|---|---|---|
| A-1 | **Enumeration through the reset response** (§12) | `/password/forgot` returns `202` with a fixed body for every input, and does the same work either way | Byte-identical bodies and headers for a real and an unknown address, plus a timing comparison with a stated tolerance |
| A-2 | **Enumeration through create conflicts** (§12) | A duplicate is a `409` — and this one is **deliberately not hidden**, because the caller is an authenticated administrator who can already list every user in the organization | Asserted, with the reasoning recorded |
| A-3 | **A reset token for user A setting user B's password** (§11) | The token names the user; the form carries no user id at all | The stored `user_id` is the only source; a request naming another is impossible because there is nowhere to name one |
| A-4 | **Invite-email flooding of a third party** (§10) | Rate limit on invite and on forgot, per organization and per address | The limiter refuses after the bound, and the refusal is the same shape as the success |
| A-5 | **Privilege escalation by self-update** (§3) | `status`, `email_verified`, `id`, `org_id` refused from the raw body | Each refused with a `400` naming the field, and the stored value unchanged |
| A-6 | **A token surviving its use** | `used_at` set in the same transaction as the password write, and consumption is `UPDATE … WHERE used_at IS NULL RETURNING` — so two concurrent uses cannot both win | A concurrent test, because a sequential one passes against a check-then-act |
| A-7 | **A reset link in a log** | The token is a query parameter on the hosted page; `P0-09`'s redaction covers `token`, and the page never logs its own URL | Asserted against captured log output |
| A-8 | **A deactivated user still acting** | Sessions revoked, cache tombstoned, refresh tokens revoked, all in the request | End-to-end: a live session's cookie and a refresh token both stop working immediately after |

## 14. Logging / Audit

`user.created`, `user.updated`, `user.deactivated`, `user.reactivated`, `user.invited`, `user.invite_accepted`, `user.password_reset_requested`, `user.password_changed`.

`password_reset_requested` is written **only when a user was actually found**. Writing one for an unknown address would put every probed address into the audit log, which is the enumeration list the endpoint exists to withhold, stored durably.

No event carries a token, a password, or a hash.

## 15. Security Controls

- Tokens hashed with SHA-256 at rest. 256 bits of entropy settles brute force; a slow KDF here would be self-inflicted amplification, which is ADR-016's reasoning arriving in a second place.
- Single-use enforced by the database, not by a read-then-write.
- Deactivation revokes sessions **and** refresh tokens **and** tombstones the session cache. Any one of the three left out leaves the user working.
- The set-password page reuses `P1-12`'s CSRF, and its token comes from the query string while the CSRF comes from a cookie — so a cross-site post cannot supply both.
- Rate limits on the two amplification endpoints.

## 16. Testing Strategy

- **Unit**: token generation and hashing, email normalisation, the search predicate's parameterisation.
- **Integration**: every route and its permission; the four documented create fields; conflict on duplicate; the update refusals; deactivation killing a real session and a real refresh token; invite acceptance moving `invited` → `active` and setting `email_verified_at`; reset consuming a token exactly once under concurrency; the forgot endpoint's identical responses; rate limits; every audit event.
- **Security**: the abuse-case table above, in `tests/security` where it already has neighbours.
- **Mutation**: at minimum — make consumption a read-then-write, drop refresh-token revocation from deactivate, and make the forgot response differ.

## 17. Acceptance Criteria

The card's five DoD items, plus the global DoD.

## 18. Implementation Sequence

1. Migration, then `internal/mail` (ADR-018).
2. OpenAPI, which generates the interface.
3. `internal/user`: store, tokens, handlers.
4. The hosted page in `internal/login`.
5. Policy, wiring, config.
6. Tests, then the generated client and API reference.

## 19. Rollback Strategy

The migration is additive and nullable, so the previous version runs against the new schema unchanged. Rolling the application back leaves `email_verified_at` populated and unread, which is harmless.

## 20. Technical Risks

| Risk | Mitigation |
|---|---|
| The forgot endpoint leaks by timing | It does the same work for both cases, including a hash comparison against a fixed dummy; asserted with a tolerance, and the assertion fails when the work is short-circuited |
| Deactivation misses one of the three revocations | One handler does all three, and the test checks all three through their real interfaces rather than by reading rows |
| A token is consumed twice under load | Consumption is a single conditional `UPDATE`, tested concurrently |
| The set-password page becomes a second login page | It sets a password and redirects to `/login`; it issues no session of its own |
