# P1-18 — Management API: Applications

**Date**: 2026-09-10
**Branch**: `feat/P1-18-applications`
**Spec**: [`MEMORY/specs/P1-18-applications.md`](../specs/P1-18-applications.md)

---

## What this is

Six operations on registered OIDC clients, under the project that owns them:

```
GET    /v1/organizations/{org_id}/projects/{project_id}/applications
POST   .../applications
GET    .../applications/{application_id}
PATCH  .../applications/{application_id}
DELETE .../applications/{application_id}
POST   .../applications/{application_id}/rotate-secret
```

No migration, and almost no new logic. `P1-05` already owns secret generation, hashing, redirect URI canonicalisation, the grant-type rules and the audit events. What this task adds is the HTTP surface and **two boundaries the store could not know about on its own**.

## The two boundaries

### The project is not a tenant, and RLS does not know it

The organization is enforced the way `P1-17` enforces it: `WithTenant` scopes the transaction, `org_id = current_org_id()` confines every query, and no handler compares an id.

**`applications.project_id` is not a tenant column, so none of that touches it.** Two projects in one organization are one tenant to the database — every row of both satisfies the policy. The project boundary is an ordinary predicate: `WHERE id = $1 AND project_id = $2`, in one `GetInProject` that every other operation passes through first.

The test that proves it uses the same caller, the same role and the same organization, and asks for an application through the *other* project's path. Get, patch, delete and rotate all answer `404`, each paired with the same operation through the right project succeeding — otherwise a service that refused everything would pass.

Worth saying plainly, because it is the assumption that silently deletes a boundary: **RLS is the tenant boundary, not a general-purpose filter.**

### The audit guard could not see any of this

`P1-05`'s store writes its own events straight at `audit.Writer`. `P1-15`'s guard watches a per-request trail that only `management.Audit` marks — so every successful application mutation would have been reported as unaudited, and `auth_management_unaudited_mutations_total`, a metric that is supposed to be permanently zero, would have been noise from the first request.

**A false alarm on every normal request is worse than a missing one.** It is the alert that gets muted, and then the real one is invisible too.

`NewStore` now takes a `Recorder` interface — `*audit.Writer` satisfies it, so nothing that already passed one changed — and this package hands it an adapter routing through `management.Audit`. Two stores over one table, deliberately: the token endpoint's writes straight at the writer, which is right there because there is no HTTP guard around a token exchange.

The test asserts the observer counted zero, and then **makes the same guard fire** against a handler that writes nothing — because an observer that never fires proves nothing, and a disconnected one would pass the first assertion forever.

## The secret

The one thing this task is really about. It leaves the process exactly twice: the `201` of create and the `200` of rotate. Both are an explicit `Reveal()` call; every other path holds a `client.Secret`, whose `String`, `GoString`, `Format` and `MarshalJSON` all return `[REDACTED]`.

The test does not assert "the read response has no `client_secret` field" — that passes against a service returning it under another key, or inside the name. It asserts that **the secret's characters do not appear anywhere in any subsequent body**, and separately that the stored hash and a half-length prefix do not either. Each probe is paired with `has_secret: true`, so the absence is not simply an application that lost its credential.

Rotation's overlap is tested at both ends: the previous secret still verifies inside the window, and does not verify one second after it — through `client.Credentials.Verify` rather than a comparison the test invents. And `overlap_hours=0` is tested on its own, because a handler treating `0` as "unset" and substituting the 24-hour default is the obvious bug and would leave a leaked credential live for a day.

## `type` is refused, not ignored

An update naming `type` is a `400` whose `details` name the field. Not silently dropped, which is what `json.Unmarshal` does on its own — the caller would get a `200` and a body still saying `web`, having been told a reclassification happened.

That check reads the **raw** body, which is why `P1-16` added `RawBody`: `additionalProperties: false` is documentation, not enforcement. `id`, `project_id`, `org_id`, `client_secret` and `has_secret` are refused the same way and for the same reason.

The test asserts both halves — the refusal, and that nothing changed, including the `name` that was bundled with it and the secret. Asserting only the second would pass with no refusal at all.

## Found while building it

**A caller's typo was a 500.** The first version told a validation error from an internal one by matching on the error text, and every wildcard redirect URI, every removed grant type and every malformed URI arrived as `500 An unexpected error occurred`. `P1-05`'s validators are plain `fmt.Errorf` with no common prefix, so the string check matched none of them.

A string check standing in for a type, and it did not work. `client.ErrInvalid` is a sentinel now, wrapped around all twenty validator messages, and `faultFrom` matches it with `errors.Is`. **A 500 tells the caller nothing they can act on and pages somebody at night for a typo** — the reason to fix it properly rather than extend the list of prefixes.

**`oapi-codegen` decodes an optional request body unconditionally.** The contract said `requestBody: {required: false}` for rotation; the generated wrapper calls `json.NewDecoder(r.Body).Decode(&body)` regardless, so `POST .../rotate-secret` with no body answered `400 can't decode JSON body`.

Requiring `{}` would be a contract describing a papercut, on the one operation people perform under stress. `overlap_hours` is a query parameter instead — it is a small integer, not a credential, so an access log holding it is fine. The reasoning is in the parameter's own description, not only here.

**The third required handler, and the import cycle it caused.** `httpserver.New` refuses a `/v1` chain missing any half of the Management API, so every endpoint package's harness must name every other one. This package first satisfied its project check by calling into `internal/project` — and `P1-17`'s in-package test then needed a handler from here, which is a cycle.

The dependency bought nothing: the question is "is there a row", which RLS already scopes to the caller's tenant, not "give me a Project". It is a bare `SELECT EXISTS` now. **The friction is still growing** — `P1-19` makes four — and the answer when it does is a shared constructor for `Deps`, not a weaker guard. The guard is right: `apiRoutes` embeds these interfaces, and a nil one is a panic on the first request to an already-registered route.

**A mutation survived, and it was right to.** Removing `canonicalise` from `Update` left every redirect-validation test passing — because `Application.Validate` already rejects a bad URI, so what the mutation actually removed was the **canonicalisation**, and nothing was checking that at all.

That is not a cosmetic gap. Redirect matching at authorize time is exact string comparison against the stored form, deliberately (`P1-05`). A client updated with `HTTPS://App.Example.TEST/cb` would store a value that the browser's `https://app.example.test/cb` never equals, and the login would fail with an error naming neither. There is a test now, and it compares an update's stored form against a create's rather than against a literal — so the two paths cannot drift apart without it failing.

**The coverage floor caught the other half of the same omission.** `internal/oauth/client` fell to 77% because `GetInProject` and `List` were exercised only from the HTTP package. A floor on a security-critical package is not a percentage game: the two functions holding the project boundary had no test where they live.

## The permission table

| Route | Role |
|---|---|
| `GET`, `POST` `.../applications` | `ORG_ADMIN` |
| `GET`, `PATCH` `.../applications/{id}` | `ORG_ADMIN` |
| `POST .../applications/{id}/rotate-secret` | `ORG_ADMIN` |
| `DELETE .../applications/{id}` | `ORG_OWNER` |

**Rotation deliberately stays at `ORG_ADMIN`.** It is the response to a suspected leak, and a control that requires waking the organization owner is a control that gets skipped at 3am. It is loud in the audit log instead.

Delete is the raised one: every user signing in through that client stops being able to, at once and without warning to the consumer application.

## Audit

`P1-05`'s four events, unchanged: `application.created`, `application.updated`, `application.secret_rotated`, `application.deleted`.

**The update event records redirect URIs before and after, by value.** They are not secret, and a widened redirect URI is the highest-value change anybody can make to a registration — it is the open-redirect and token-theft path. This is the only place it becomes visible afterwards. A test asserts the widened URI is in the payload, and that no payload in the organization contains the secret.

## Deployed and verified on staging

`a37ea70` rolled out on `10.1.200.13`. Backup first (113KB, restore-verified), then the application — no migration, and `schema_migrations` still reads `20260910000016`.

32 checks, driven through the public TLS endpoint with a real PKCE login. Everything the smoke test creates is removed afterwards, by name rather than by emptying the fixture organization.

| Check | Result |
|---|---|
| `POST` a confidential client | `201` with `client_secret` |
| The read, the list and an update | none carries the secret, its hash, or a prefix; all report `has_secret: true` |
| A `spa` client | created with no secret, and **none stored** — the response omitting it would look identical if one had been |
| Rotating a public client | `409` |
| Rotation with `overlap_hours=24` | a different secret, `previous_secret_expires_at` a day out, and the previous hash still live in the row |
| `overlap_hours=0` | the previous hash is gone at once |
| `overlap_hours=10000` | `400` |
| `GET`, `PATCH`, `DELETE`, rotate through the **other project of the same tenant** | `404` each, with the control: the same application through its own project reads `200` |
| The other project's list | does not contain it |
| `PATCH {"type":"spa"}` | `400` naming the field, and the stored type still `web` |
| A wildcard redirect URI | `400` on **both** create and update — not the `500` the first implementation gave |
| `implicit` | `400` |
| `ORG_ADMIN` rotating | `200` — the 3am path stays open |
| `ORG_ADMIN` deleting | `403` `PERMISSION_DENIED` |
| `ORG_OWNER` deleting | `204` |
| Audit | all four events; the update event carries `redirect_uris_before`/`_after`; no payload contains the secret |

No failures, and nothing to fix afterwards — the two things that would have shown up here had already been found locally: the `500` on a caller's typo, and the missing canonicalisation on update.

## Verification

- **Integration, 26 tests** against real PostgreSQL and Redis, through the generated router and the real `/v1` chain: the secret shown once and never again (three probes, each with its control), no hash and no prefix in any read, a public client created with no secret and none stored, rotation's overlap at both ends, a zero overlap, a public client refused rotation, redirect validation identical on create and update across four bad URIs plus a good one on both paths, the removed grants, `type` and the identity fields refused, an omitted field left alone, both boundaries with their controls, the permission split, `PROJECT_OWNER` granting nothing, the four audit events, the guard seeing every mutation, pagination, and a replayed create making one application.
- **Store-level** (`internal/oauth/client`, real PostgreSQL): `GetInProject` refusing another project in the same organization with its control, `List` scoped and paging by `(created_at, id)` with `size+1` and a cursor that skips exactly what came before it, a list unable to reach another tenant's project — paired with the owning tenant seeing the row, so the empty page is RLS rather than a query matching nothing — the sentinel on four validator errors with a control that `ErrNotFound` does *not* wrap it, and an update canonicalising exactly as a create does.
- **Unit**: `P1-05`'s validator tables, unchanged and still passing after the sentinel wrapping.
- **Five mutations, all killed**: the project predicate dropped, canonicalisation removed from update, a zero overlap falling back to the default, the immutable-field check removed, and the audit adapter writing straight at the recorder. Each asserts the named test actually ran.

## What this task did not build

- **A console screen.** `P1-22` owns the Applications tab; `docs/UI-UX/08` already specifies the show-once dialog.
- **Anything that reads a secret back.** There is nothing to read it from.
- **A staleness signal for an unrotated secret.** `applications` records no "when was this issued", and deriving it from `updated_at` would be wrong — any edit moves that. Noted as out of scope rather than approximated.
