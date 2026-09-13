# P3-10 — User Detail MFA Tab, and the Factor API It Needs

Feature specification, per `docs/PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`,
with the `docs/UI-UX/19` chain for the screen in § 8.

| | |
|---|---|
| **Task** | `TASKS/PHASE-3-ADVANCED-SECURITY.md` § P3-10 |
| **Phase** | Phase 3 — Advanced Security |
| **Surface** | backend (Management API + one hosted page) and console |
| **Plan refs** | `docs/UI-UX/08` (MFA tab), `docs/UI-UX/19`, `docs/UI-UX/13`, `docs/UI-UX/14`, `docs/PLAN/05`, `docs/PLAN/08` |
| **Depends on** | `P3-02` (TOTP), `P3-04` (recovery codes, admin reset), `P3-05` (passkeys), `P3-07` (mandate) |
| **Written** | 2026-09-13 |

---

## 0. Two gaps this task has to close before it can start

**`PG-42` — nothing assigns the factor-management API.** `P3-02`, `P3-04` and
`P3-05` each built a mechanism and deferred "the endpoint" to `P3-12`'s screen.
`P3-10` comes first, is marked console-only, and its own steps (enrol, remove,
regenerate) cannot happen without that API. The console must not gain a
capability the API lacks (FR-14), so the API is built here, and the three
requirements those tasks carried forward are met here rather than in `P3-12`:
enrolment requires recent authentication, enrolment is audited, recovery codes
are issued at the first enrolment.

**`PG-43` — a passkey cannot be registered from the console's page.** WebAuthn
binds a credential to the relying party, and `P3-05` made the relying party the
issuer's origin (on purpose: that is the phishing defence). The console is a
separate static deployment on a separate origin (`docs/PLAN/06`), so a ceremony
started in the console page is refused by the browser. Registration therefore
happens on a **hosted page on the issuer's origin**, like the login page that
later asks for the passkey, and the console sends the user there and back.

## 1. Business objective

A user can see and manage their own second factors; an administrator can see a
member's factors, and can reset them through `P3-04`'s audited path, but can
never add or remove one on the member's behalf (`docs/UI-UX/08`: admin
read-only, self-service full control).

## 2. Actors

| Actor | Interest |
|---|---|
| A user (self) | Add TOTP, add a passkey, remove a factor, regenerate recovery codes |
| `ORG_ADMIN` | See a member's factors; reset them when a device is lost |
| An attacker holding a live session or access token | Must not be able to add a factor — that is how a stolen session becomes a permanent, MFA-protected takeover |

## 3. Functional requirements

| # | Requirement |
|---|---|
| F-1 | `GET /v1/me/mfa` — the caller's active factors, recovery codes remaining, whether the organization requires MFA, and which factor types this deployment offers |
| F-2 | `POST /v1/me/mfa/totp` — begin a TOTP enrolment: returns the secret, the provisioning URI and the QR module grid, once |
| F-3 | `POST /v1/me/mfa/totp/{factor_id}/confirm` — prove a code; activates the factor; returns recovery codes **once** when the user had none |
| F-4 | `DELETE /v1/me/mfa/factors/{factor_id}` — remove one of the caller's active factors |
| F-5 | `POST /v1/me/mfa/recovery-codes` — replace the caller's recovery codes; returns them once |
| F-6 | `GET /v1/organizations/{org_id}/users/{user_id}/mfa` — a member's factors, read-only, for `ORG_ADMIN` |
| F-7 | `GET/POST /account/passkeys` — the hosted passkey registration page |
| F-8 | The console's MFA tab: read-only for another user, full management for oneself |

## 4. Non-functional requirements

- **Recent authentication** for F-2, F-4, F-5 and F-7: the session the request
  runs under authenticated within **10 minutes**. Past that, the API answers
  `403 REAUTHENTICATION_REQUIRED` and the console sends the user through the
  ordinary login with `prompt=login`, which re-checks the password **and the
  existing factor** — stronger than a password field, and it keeps passwords off
  the Management API.
- F-3 is not gated by recency (the factor is already pending, begun under a
  recent authentication) but **is** bounded by `P3-03`'s per-user attempt counter,
  because it is a six-digit code check.
- No secret, code or recovery code is ever logged or audited.

## 5. Dependencies

`mfa.TOTP`, `mfa.Store`, `mfa.RecoveryStore`, `mfa.WebAuthnVerifier`,
`mfa.RedisAttempts`, `authn.LoginPolicy` (the mandate), `session` (authentication
time from `sid`), `management` (caller, policy, audit), the hosted page
infrastructure in `login`. New Go dependency: `rsc.io/qr` (BSD-3-Clause, no
transitive dependencies) to compute the QR grid server-side.

## 6. Data model changes

**None.** `user_mfa_factors`, `user_recovery_codes`, `sessions` already hold
everything. The passkey ceremony's state between begin and finish lives in Redis
under a hashed handle, as the login challenge's does.

## 7. API contract

```
GET    /v1/me/mfa                                         self
POST   /v1/me/mfa/totp                                    self, recent auth
POST   /v1/me/mfa/totp/{factor_id}/confirm                self, attempt-bounded
DELETE /v1/me/mfa/factors/{factor_id}                     self, recent auth
POST   /v1/me/mfa/recovery-codes                          self, recent auth
GET    /v1/organizations/{org_id}/users/{user_id}/mfa     ORG_ADMIN
GET    /account/passkeys?client_id=…&return_to=…          hosted, session cookie, recent auth
POST   /account/passkeys                                  hosted: begin | finish
```

The admin view returns **factor types, labels and dates only** — no secret, no
credential id, no public key. `POST /v1/organizations/{org_id}/users/{user_id}/mfa-reset`
(`P3-04`) remains the only administrator write.

`POST /v1/me/mfa/totp` takes a **required** body (`{}` for the default name):
the generated server decodes a body on every call regardless of the contract's
`required: false`, and a contract promising an optional body the server refuses
would be a contract that lies. Found by the integration test.

New error code `REAUTHENTICATION_REQUIRED` (403). Distinct from
`PERMISSION_DENIED` because the caller can act on it — sign in again — and a
console that treated it as "you may not" would tell a user they cannot manage
their own account.

## 8. Frontend — the `docs/UI-UX/19` chain

| Step | Answer |
|---|---|
| Design | A tab on User detail. For another user: a list of factor badges (type, label, added, last used) and the recovery-code count, plus "Reset MFA" where the caller may. For oneself: the same list with Remove per factor, "Add authenticator app", "Add passkey", "Regenerate recovery codes" |
| Component | `Table`, `Badge`, `Button`, `Modal` (enrolment), `ConfirmDialog` (remove, regenerate, reset); one new component, `QrCode`, rendering the module grid as SVG `rect`s |
| State | loading · error · no factors · factors · enrolment step 1 (scan) · step 2 (code) · codes shown once · reauthentication needed · MFA not configured on this deployment |
| Interaction | Add authenticator → modal with QR + manual key → code field → confirm → recovery codes (if issued) with Copy and Download, and a "I have saved these" acknowledgement before the modal can close. Remove → confirm dialog naming the factor; `color-danger` because it is destructive. Add passkey → if authentication is stale, re-authenticate; then navigate to the hosted page |
| API Dependency | § 7 |
| Loading | Skeleton rows; buttons show their pending state |
| Error | Wrong code → inline under the field ("That code didn't match. Codes change every 30 seconds — try the current one."). Expired enrolment → the modal restarts. `REAUTHENTICATION_REQUIRED` → "For your security, sign in again to change your sign-in methods." with a Sign in again button. Last factor under a mandate → the Remove action is disabled with the reason visible, and the API refuses too (409) |
| Empty | "No second factor yet" with the add actions (self) or "This user has no second factor" (admin) — genuinely empty, never filtered |
| Permission | Self management only on one's own user id, and the API derives the user from the token regardless. Admin view needs `ORG_ADMIN`; Reset MFA needs what `P3-04` requires. A member viewing a colleague is refused by the API and the tab says so |
| Responsive | Desktop-first like the rest of User detail; the enrolment modal fits a 360 px width because `P3-12` reuses it on the mobile personal settings screen |
| Accessibility | The QR code is `role="img"` with a label, and the **manual key is always shown beside it as text** — the text alternative is the same secret in a form a screen reader and a keyboard can use. Recovery codes are a list, copyable as text. Focus moves into the modal and returns to the trigger |
| Test | Component tests for the tab's states and the codes-once acknowledgement; E2E for TOTP enrolment (the test computes the code from the secret) and passkey registration (a Chrome virtual authenticator), each ending in a real login that uses the new factor |

## 9. Authorization rules

| Operation | Rule |
|---|---|
| Self routes | User and organization from the token. A factor id is resolved together with the caller's user id; another user's factor is `404` |
| Recent authentication | The token's `sid` names a session; its `created_at` is the authentication time; it must be within 10 minutes. A token with no session (`client_credentials`) cannot manage factors at all |
| Removing the last active factor | Refused with `409` when the organization requires MFA (`P3-07`). A user may still remove their last factor in an organization that does not |
| Admin read | `ORG_ADMIN`, `ScopeOrganization` |
| Hosted passkey page | The SSO session cookie, recent authentication, CSRF; `return_to` only to an origin in the named application's `allowed_origins` (ADR-020) — never an arbitrary URL |

## 10. Validation

| Input | Rule |
|---|---|
| `label` | Optional, trimmed, ≤ 64 characters; defaults by type |
| `code` | 6 digits |
| `return_to` | Absolute `https` URL (or loopback `http` locally) whose origin is in the application's `allowed_origins` |

## 11. Error handling

| Case | Answer |
|---|---|
| MFA not configured on the deployment | `GET /v1/me/mfa` says so (`available_types: []`); writes answer `409` |
| Wrong code | `400 VALIDATION_ERROR` on `code`, counted against the per-user bound |
| Bound exhausted | `429` |
| Confirming a factor that is not pending, not the caller's, or gone | `404` |

## 12. Edge cases

| Case | Behaviour |
|---|---|
| Begin, never confirm | The pending factor is harmless (it answers no challenge) and is replaced by the next Begin; a daily sweep is not needed for correctness |
| Two TOTP enrolments | `P3-02` allows one active TOTP; a second Begin while one is active is `409` |
| First factor added | Recovery codes issued and shown once |
| Factor added when codes already exist | No new codes; the count is shown |
| Regenerate with no active factor | `409` — recovery codes without a factor recover nothing |

## 13. Abuse cases

| # | Scenario | Control | Test |
|---|---|---|---|
| A-1 | A stolen session adds the thief's authenticator | Recent authentication; the thief would have to pass the login again, including the victim's existing factor | A token whose session authenticated 11 minutes ago is refused |
| A-2 | Brute-force the confirm code | Per-user attempt bound shared with login | Exhausting it refuses a correct code |
| A-3 | Confirm another user's pending factor | Factor resolved with the caller's user id | `404`, still pending |
| A-4 | Remove another user's factor | Same | `404`, still active |
| A-5 | An admin adds or removes a member's factor | No such admin route exists; self routes use the token's user | Architecture test over the policy table; admin token on self route acts on the admin only |
| A-6 | Read a member's secret or credential material through the admin view | Not selected, not in the schema | Response body inspected |
| A-7 | Remove the last factor under a mandate | `409` | Tested |
| A-8 | The hosted page as an open redirect | `return_to` origin must be the application's registered origin | An unregistered origin is refused |
| A-9 | Cross-site POST to the hosted page | CSRF token + `SameSite=Lax` session cookie | Tested |

## 14. Logging and audit

Through `management.Audit` for the API, the audit writer for the hosted page:

| Event | Payload |
|---|---|
| `user.mfa.enrolment_started` | `factor_id`, `factor_type` |
| `user.mfa.enrolled` | `factor_id`, `factor_type`, `recovery_codes_issued` |
| `user.mfa.removed` | `factor_id`, `factor_type`, `remaining` |
| `user.mfa.codes_generated` | `count`, `initiator` (`enrolment` or `self-service`) |

`user.mfa.enrolled` and `user.mfa.codes_generated` already existed (`P3-04`,
`P3-07`) and are reused rather than given P3-10-specific twins, so an incident
review searches one name whichever path wrote it.

## 15–17. Security controls, testing, acceptance

Summarised in §§ 9, 13 and § 8's Test row. Acceptance is the card's Definition of
Done.

## 18. Implementation sequence

1. Error code, recency check, contract, generated code.
2. The self and admin API handlers; integration tests.
3. The hosted passkey page; integration test with the software authenticator `P3-05` built.
4. The console tab and components; component tests.
5. The e2e stack gains an MFA seal key; E2E for both factor types.
6. Mutation run.

## 19. Rollback

No migration. A rolled-back build loses the routes; factors enrolled through them
remain valid, because the login path that verifies them predates this task.

## 20. Risks

- **The recency window** is a judgement: 10 minutes is long enough to finish an
  enrolment begun right after signing in, short enough that a session left open
  at lunch cannot add a factor.
- **Forced enrolment at login (`P3-07`) issues no recovery codes.** Found while
  reading it for this task. Handing codes over there needs an interstitial in the
  hosted login between confirming the code and resuming the authorization, and
  `P3-12`'s card already carries "recovery codes are generated at enrolment" for
  exactly that flow. Not fixed here. What this task does instead: the MFA tab
  shows a user with a factor and **zero** codes a prominent "You have no recovery
  codes" state with the generate action, so the gap is visible to the person it
  affects rather than discovered when their phone is lost.
