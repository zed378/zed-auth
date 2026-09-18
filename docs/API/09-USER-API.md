# 09 - User API

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-19 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Document organization-scoped user administration: listing, inviting, reading, updating, deactivating and reactivating a user, and administrator-triggered password reset.

## Scope

`/v1/organizations/{org_id}/users*`, excluding a member's MFA and sessions (`14-SESSION-API.md`) and a user's project role grants (`12-ROLE-AND-PERMISSION-API.md`). All routes require `ORG_ADMIN` over the organization in the path (`02-AUTHENTICATION-AND-AUTHORIZATION.md`).

## As Built

**No password field exists anywhere on this resource, in either direction.** Not on create, not on update, not in any response (`User`, `UserCreate`, `UserUpdate` all omit it). A new user is created `status: "invited"` with no password; the person who owns the account sets one themselves through a link sent to the address being invited — which is also what proves the address is theirs. There is no administrator-settable password anywhere in this API.

**Invitation delivery outcome is reported, not assumed.** `POST /v1/organizations/{org_id}/users` returns `invite_email_sent`, which reports what actually happened rather than what was asked for: `201` with `invite_email_sent: false` means the account was created but the mail server could not deliver the link, so it must be conveyed another way — the account is **not** rolled back because delivery failed (ADR-018: outbound email is plain SMTP and nothing waits on it).

**`status` is not part of `UserUpdate`.** Deactivation and reactivation are their own operations, deliberately, because "who deactivated this account and when" is a question asked during an incident and answering it should not require diffing two profile updates. A body naming `status`, `email_verified`, `id`, or `org_id` on `PATCH .../users/{user_id}` is **refused** (`400`), not silently ignored — silently dropping a field would tell the caller a change happened when it did not.

**Deactivation is synchronous and complete before the response returns.** `POST .../deactivate` revokes every live session, tombstones the session cache, and revokes every refresh token — all three, before the `204` is returned. A deactivated user who can still act because one of the three was skipped is not deactivated.

**Reactivation does not restore sessions.** `POST .../reactivate` returns the account to `active`; it does not undo what deactivation ended. The user signs in again — correct, because the reason the account was deactivated is not known to have stopped being true.

**Deletion does not exist for a user.** The card this resource implements asks for deactivation instead of deletion specifically so that every audit entry naming a user stays resolvable; there is no `DELETE` on this resource.

**Administrator-triggered password reset never returns the link.** `POST .../password-reset` mails a single-use, short-lived reset link to the user's own address and answers `202` with only `email_sent: boolean`. The administrator triggering it cannot see the link — the only party who should hold it is the one who can read that mailbox. This is distinct from the self-service `/login/forgot` hosted page, which is unauthenticated and answers identically whether or not the address exists.

**`search` on the list endpoint is a substring match**, against email, username, and display name — not a prefix match, because an administrator usually remembers the middle of a name rather than its start.

## Rules and Defaults

| Field / rule | Value | Enforced in |
|---|---|---|
| `email` (create) | Required, `format: email`, max 320 chars; unique within the organization, case-insensitive, stored lowercased | `openapi/openapi.yaml` `UserCreate`; `backend/internal/user/store.go` |
| `email` (update) | `format: email`, max 320 chars; changing it clears `email_verified` | `openapi/openapi.yaml` `UserUpdate` |
| `username` | Optional, max 100 chars, nullable | `openapi/openapi.yaml` |
| `display_name` | Optional, max 200 chars, nullable | `openapi/openapi.yaml` |
| `send_invite_email` (create) | Optional, default `true`; `false` creates the account and its invitation token without mailing anything (bulk import) | `openapi/openapi.yaml` `UserCreate` |
| `status` values | `invited`, `active`, `locked`, `deactivated` — not settable via `PATCH` | `openapi/openapi.yaml` `UserStatus` |
| `search` query param | Substring match on email/username/display name, max 200 chars | `openapi/openapi.yaml` `listUsers` |
| Page size | Default 20, max 100 | `06-PAGINATION.md` |
| Required role | `ORG_ADMIN` over the organization in the path, every route | `backend/internal/management/policy.go` |

## Interfaces

| Method & path | `operationId` | Notes |
|---|---|---|
| `GET /v1/organizations/{org_id}/users` | `listUsers` | `search`, `page_size`, `page_token` |
| `POST /v1/organizations/{org_id}/users` | `createUser` | `201` with `UserCreated` (`User` + `invite_email_sent`) |
| `GET /v1/organizations/{org_id}/users/{user_id}` | `getUser` | |
| `PATCH /v1/organizations/{org_id}/users/{user_id}` | `updateUser` | Profile fields only; `status`/`email_verified`/`id`/`org_id` refused |
| `POST /v1/organizations/{org_id}/users/{user_id}/deactivate` | `deactivateUser` | `204`; ends every session and refresh token first |
| `POST /v1/organizations/{org_id}/users/{user_id}/reactivate` | `reactivateUser` | `204`; does not restore sessions |
| `POST /v1/organizations/{org_id}/users/{user_id}/password-reset` | `resetUserPassword` | `202` with `ResetRequested{email_sent}`; link never returned |

Full request/response schemas: `public-site/docs/api-reference/list-users.api.mdx`, `create-user.api.mdx`, `get-user.api.mdx`, `update-user.api.mdx`, `deactivate-user.api.mdx`, `reactivate-user.api.mdx`, `reset-user-password.api.mdx`.

Error cases follow `04-ERROR-HANDLING.md`; notable ones: `409 CONFLICT` on `createUser` for a duplicate email within the organization; `404` on any route for a user not visible in the caller's organization (see `02-AUTHENTICATION-AND-AUTHORIZATION.md` on 404-vs-403); `409` on `reactivateUser` for a user not currently deactivated.

Idempotency: `Idempotency-Key` is accepted on every `POST` here (`07-IDEMPOTENCY.md`).

Audit events (`backend/internal/audit/audit.go`): `user.created`, `user.invited`, `user.updated`, `user.deactivated`, `user.reactivated`, `user.password.reset_requested` (written only when a user was found — never for a nonexistent one, which would otherwise put every probed address into the audit log).

## Security Considerations

- No password field anywhere on this resource is the primary control against an account whose first credential was chosen by someone other than its owner (`docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`).
- `email_sent` rather than the reset link itself keeps an `ORG_ADMIN` from being able to take over a member's account by triggering a reset and reading the link.
- Deactivation's three-part synchronous teardown (sessions, session cache, refresh tokens) is what makes "deactivated" mean "cannot act", not "cannot start a new session."

## Verification

- `backend/internal/user/user_integration_test.go`.
- `backend/internal/user/mfareset_integration_test.go` (adjacent resource; cross-referenced from `14-SESSION-API.md`).

## Not Yet Built / Open Questions

None specific to this resource.

## Related Documents

- `docs/API/12-ROLE-AND-PERMISSION-API.md` (a user's project role grants)
- `docs/API/14-SESSION-API.md` (a member's MFA and sessions, administrator view/reset)
- `MEMORY/DECISIONS.md` ADR-018 (outbound email)
