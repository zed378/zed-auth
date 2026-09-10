# P1-05 — Application (OIDC Client) Registration and Credentials

| | |
|---|---|
| **Date** | 2026-09-09 |
| **Task** | `TASKS/PHASE-1-MVP-CORE-AUTH-SSO.md` § P1-05 |
| **Phase** | Phase 1 |
| **Surface** | backend |
| **Author** | Zed |
| **Commits / PR** | `feat/P1-05-application-registration` |
| **Status** | Completed |

---

## What Changed

`internal/oauth/client` registers OIDC clients: the type rules, the redirect URI rules, secret generation and verification, rotation with an overlap, and a store that writes each lifecycle change together with its audit event.

No HTTP endpoints. `P1-18` exposes this; the split is the task card's and it is the reason the rules below could be tested exhaustively without a router in the way.

## Why

`docs/PLAN/04` § `applications` and `docs/PLAN/09` § Protection Against Common Attacks. Two failure modes make this worth its own task rather than a CRUD screen.

A client secret that can be read back after creation is a secret that exists in a database, a backup, a support ticket and a screenshot. `docs/UI-UX/08` states the rule — "shown once at creation only, never retrievable again" — and a screen cannot keep it; only a store that never held the plaintext can.

A redirect URI that matches more than the one URL it was meant to is the failure that turns a correct implementation of everything else into an account takeover. The authorization code is delivered to whoever the match let in.

## How

**Redirect matching is one line: compare the strings.** Everything else lives at registration, which is the correct asymmetry — registration is where a human is present and a rejection can be explained, and matching is where an attacker is present and cleverness is a liability.

The rejection table in `client_test.go` is the design document for that. Each row names the attack rather than just the input: the trailing slash ("tolerating it is the precedent that makes the next tolerance look reasonable"), the dot segments, the `app.example.com.attacker.net` suffix confusion that only prefix matching would ever accept. A test table of bare strings is a table somebody deletes a row from when it becomes inconvenient.

Registration canonicalises — scheme and host lowercased, nothing else. Path case, percent-encoding and query order are left exactly as given, because normalising them is decoding, and decoding is where a matcher starts accepting `%2e%2e%2f`.

**Grant/type consistency refuses combinations rather than filtering them.** A `spa` asking for `client_credentials` gets an error explaining that the grant *is* the client authenticating as itself and a public client has nothing to authenticate with. Silently dropping the grant would leave the administrator to discover their misunderstanding when a production flow failed.

`IsConfidential` is deliberately not `!IsPublic`. `saml` is neither, and writing it as a negation would hand a SAML client a secret the moment the type existed.

**The `Secret` type is the interesting piece of the secret handling.** It wraps an unexported string, refuses to marshal, and implements `fmt.Formatter` so every verb redacts. Getting the value out requires writing `Reveal()`, which is greppable and conspicuous in review. A plain `string` return would eventually land in a log line or a struct that grew a JSON tag — an accident nobody makes deliberately and everybody makes eventually.

**Hashing is SHA-256, not Argon2id, and that is ADR-016.** A client secret is 256 bits from `crypto/rand`; brute force behind an infinitely fast hash is on the order of 10^52 years, so the hash's speed protects nothing that the entropy did not already protect. What a slow KDF *would* add is an amplification vector: secrets are verified on every `client_credentials` request, and at `P1-01`'s measured 90ms/64 MiB, fifty requests a second is 4.5 cores and 288 MiB — paid by us, while an attacker sending wrong secrets pays nothing.

The decision is conditional on the secret being generated here, so that condition is enforced rather than assumed: `Generate` is the only producer, nothing accepts a caller-supplied secret, and `TestSecretEntropy` fails if the length or alphabet weakens.

The store takes a `*postgres.Tx` throughout, so each change and its audit event commit together (ADR-012), and RLS confines every query without this package containing an `org_id` predicate anyone could forget.

## Files and Components Touched

| Path | Change |
|---|---|
| `MEMORY/specs/P1-05-application-registration.md` | New — the spec `CLAUDE.md` requires for credential handling |
| `backend/internal/oauth/client/client.go` | New — types, grant rules, redirect validation and matching |
| `backend/internal/oauth/client/secret.go` | New — `Secret`, `Generate`, `Verify`, `Credentials`, rotation |
| `backend/internal/oauth/client/store.go` | New — create, read, update, rotate, delete, authenticate |
| `backend/internal/oauth/client/*_test.go` | New — 24 tests including the integration suite |
| `backend/internal/audit/audit.go` | `application.updated` event type |
| `backend/internal/testsupport/factory.go` | `QueryRow`, for assertions that must see the raw row |
| `scripts/check-coverage.sh` | Floor for `internal/oauth/client` |
| `MEMORY/DECISIONS.md` | ADR-016 |

**No schema change.** `P0-07` had already written `applications` with both rotation columns and the CHECK that a public client holds no secret. Rotation being a feature of the data model rather than something bolted on saved the whole migration.

## Decisions Made

| Decision | Rationale | ADR |
|---|---|---|
| SHA-256 for client secrets, not Argon2id | 256-bit entropy settles brute force; a slow KDF on a hot verification path is self-inflicted amplification | **ADR-016** |
| Unsalted | There is no precomputation to defend against over 2^256, and a salt would imply the entropy is not being relied on | ADR-016 |
| `Secret` is a type, not a string | A string reaches a log eventually; a type that redacts under every verb cannot | — |
| Canonicalise at registration, compare exactly at authorization | Normalising at comparison time is how prefix and traversal bugs get in | — |
| Client type is immutable after creation | Changing it would strand a secret on a now-public client, or leave a confidential one without one. Delete and re-register is two audit events rather than one silent reclassification | — |
| Private IP ranges allowed as redirect targets, link-local refused | An intranet redirect is a legitimate self-hosted configuration; `169.254.169.254` is a cloud metadata service and nothing a client owns lives there | — |
| Redirect URI values are recorded in audit payloads | They are not secret, and an entry saying "redirect_uris changed" cannot answer the question it exists for | — |
| Array columns read back as JSON from Postgres | `database/sql` returns `text[]` as a raw array literal; the alternatives were hand-parsing escapes for user-supplied values, or a second SQL driver for its array codec | — |

## Deviations from the Plan

**One correction to the task card, not a deviation from the plan.** The card cites `docs/SECURITY/02` §7 (SSRF) for the internal-address abuse case. That mapping is not right: a `redirect_uri` is never fetched by this server, it is handed to the user's browser, so there is no server-side request to forge. What an internal redirect target actually risks is an authorization code delivered to a host the *user's* machine can reach and its owner cannot observe. Link-local is refused for that reason; the control is the same, the reasoning is not, and a control kept for a wrong reason is one that gets removed when someone notices the reason is wrong.

## Tests Added

| Layer | What it covers |
|---|---|
| Unit | Redirect matching: 15 rejection cases each naming its attack, plus the control asserting the exact registered URI matches and that two registered URIs both match |
| Unit | Post-logout matching is separate from redirect matching in both directions |
| Unit | Redirect validation: 17 rejections (relative, fragment, bare `#`, wildcard, http on a public host, no host, link-local v4 and v6, custom scheme by type) and 9 acceptances including every loopback form |
| Unit | Canonical form is stable — validate, canonicalise, validate again |
| Unit | Grant/type consistency across all five types; forbidden grants refused for every type |
| Unit | `saml` is neither public nor confidential |
| Unit | Secret entropy, non-determinism over 100 draws, redaction under six format verbs and inside a struct, marshalling refused, and the control that `Reveal` returns something real |
| Unit | Verification: correct, wrong, empty, hash-as-secret, prefix, suffix |
| Unit | Rotation: both secrets during the overlap, only the new one after; rotation during an open window restarts it and drops the two-generations-old secret; `RotateNow` leaves no window; expired is indistinguishable from unrelated |
| Integration | The whole stored row inspected as text for the plaintext, plus every audit payload — proven non-vacuous by making the store write the plaintext and watching it fail |
| Integration | Public clients get no secret, and the database CHECK independently refuses one |
| Integration | Rotation through the store across the expiry boundary |
| Integration | Cross-tenant read is not-found, with a control proving the row is readable in its own scope |
| Integration | All four lifecycle events written; a widened redirect URI appears in the audit payload with its value |
| Integration | Registration stores the canonical form and the pre-canonical form no longer matches |

## Abuse Cases Covered

| Abuse case | Source | Test |
|---|---|---|
| Open redirect via a shared prefix | `docs/SECURITY/02` §1 | `TestRedirectURIMatchingIsExact` |
| Suffix confusion (`app.example.com.attacker.net`) | `docs/SECURITY/02` §1 | Same |
| Public client using `client_credentials` | Task card | `TestPublicClientsCannotUseClientCredentials` |
| Public client created with a secret | Task card | `TestPublicClientsGetNoSecret`, `TestTheDatabaseRefusesAPublicClientSecret` |
| Secret read back after creation | `docs/UI-UX/08`, `docs/SECURITY/02` §16 | `TestOnlyTheHashIsStored` |
| Secret leaked through a log or audit payload | `docs/SECURITY/02` §16, §19 | `TestSecretDoesNotPrintItself`, `TestOnlyTheHashIsStored` |
| Timing oracle on secret comparison | `docs/SECURITY/02` §1 | Constant-time comparison; expired-vs-unrelated indistinguishability tested |
| Expired previous secret accepted | — | `TestExpiredPreviousSecretIsJustWrong` |
| Code delivered to a link-local address | §11 of the spec | `TestValidateRedirectURIRejects` |
| Cross-tenant application access | `docs/SECURITY/02` §2 | `TestApplicationsAreNotReadableAcrossTenants` |
| CPU/memory exhaustion via wrong secrets | `docs/SECURITY/02` §10 | ADR-016 is the control |

## Definition of Done Verification

- [x] Tests at the appropriate pyramid layer
- [x] Every abuse case has an automated test
- [x] No API surface changed — `P1-18` adds the endpoints
- [x] Sensitive actions write audit events; four lifecycle events, none carrying secret material
- [x] Nothing sensitive is logged
- [x] `scripts/check.sh` green (40/40)
- [x] `TASKS/PROGRESS.md` and the phase file updated

Task-specific DoD:

- [x] **A client secret is retrievable exactly once.** `Create` and `RotateSecret` are the only producers of a `Secret`; no read path can construct one, because only the hash was stored.
- [x] **Only the hash exists in the database, verified by direct inspection.** The whole row is read as text and searched — not just the secret column, so a plaintext leaking into any other column would also be caught. Proven non-vacuous.
- [x] **A redirect URI differing by a trailing slash, a query parameter, or a fragment is rejected.** All three, plus twelve more, with a control proving the matcher does not simply reject everything.
- [x] **A public client cannot be created with a secret.** Refused in code and independently by the database.
- [x] **Application lifecycle events appear in the audit log.** All four.

## What Did Not Work

**`fmt.Stringer` is not enough to redact a type.** `TestSecretDoesNotPrintItself` checks six verbs, and `%d` leaked the plaintext: `fmt` consults `String` only for `%v`, `%s`, `%q`, `%x` and `%X`, and falls back to printing struct fields for anything else — emitting the secret inside a `%!d(string=...)` marker. Nobody writes `%d` on a secret deliberately; somebody writes it on a struct that contains one, or drifts a printf format away from its arguments. Fixed by implementing `fmt.Formatter`, which takes precedence for every verb. The test found this because it enumerated verbs instead of checking the one that was obviously handled.

**Hostnames are case-insensitive and the loopback check was not.** `http://LOCALHOST:5173/cb` was refused as cleartext-on-a-public-host. Caught by the test asserting the canonical form is stable — validate, canonicalise, validate again — which is a property worth testing precisely because this asymmetry is invisible when you only ever try the lowercase form.

**A nil `[]string` binds as SQL NULL.** Both array columns are `NOT NULL DEFAULT '{}'`, so the common case — a client with no post-logout URIs — was a constraint violation. `canonicalise` now always returns a non-nil slice.

**`database/sql` cannot scan `text[]`.** pgx binds `[]string` happily on the way in and hands back the raw Postgres array literal on the way out. The options were parsing that literal by hand, getting quoting and backslash escapes right for values that come from user input, or taking `lib/pq` as a direct dependency purely for its array codec. Neither appealed; the columns are now selected through `to_jsonb`, so Postgres does the escaping it already knows how to do and `encoding/json` does the reading.

## Follow-Ups and Open Questions

- `P1-06` wires `MatchesRedirectURI` to the authorization endpoint and enforces PKCE. `P1-07` wires `Authenticate` to the token endpoint. Until then this package has no caller.
- `P1-18` adds the management endpoints, and must honour the disclosure asymmetry: the authorization endpoint discloses nothing about registered URIs, the authenticated management API names them.
- `P2-05` supplies the manager-role check that decides *who* may register an application. This package deliberately enforces no authorization of its own.
- Client type is immutable. If a "convert to confidential" operation is ever wanted, it needs its own audit event and a deliberate decision about the secret.

## What to Watch

**Anything that accepts a caller-supplied client secret.** ADR-016 depends on the secret being generated here, and the failure would be silent: no test breaks, nothing logs, and secrets are simply weakly protected from that day forward. `Generate` being the sole producer is the guard, and it is a convention a sufficiently determined feature request could route around.

**Any change that makes redirect matching more forgiving.** Each individual relaxation is defensible — a trailing slash, a case-insensitive host, resolving `..` — and the rejection table exists to make the argument visible. If a row is ever deleted, the reason it named should be answered rather than skipped.

**`application.updated` payload size.** It records redirect URIs before and after. A client with many URIs updated frequently writes large audit rows into a partitioned, append-only table. Not a problem at current scale; worth remembering when `P5-*` looks at retention.
