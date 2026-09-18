# 05 - WebAuthn and Passkeys

> Category: **Identity Protocol** (`docs/IDENTITY-PROTOCOL/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: P3-05, P3-10 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describes how WebAuthn/passkeys are implemented today: **as a second factor and hosted registration flow, not as a passwordless primary sign-in method.** A reader should not assume "passkey support" means a user can sign in with only a passkey — that is not built.

## Scope

Covers `backend/internal/mfa/webauthn.go` (the ceremony implementation) and `backend/internal/login/passkeys.go` (the hosted registration page). Where WebAuthn plugs into the MFA challenge and mandate is `06-MULTI-FACTOR-AUTHENTICATION.md`.

## As Built

WebAuthn is implemented via the `go-webauthn/webauthn` library, deliberately — unlike TOTP (`06-MULTI-FACTOR-AUTHENTICATION.md`), which was hand-written. The reasoning stated in `backend/internal/mfa/webauthn.go`: WebAuthn is CBOR, COSE key parsing, five attestation formats, and three signature algorithms against an evolving specification, and a hand-rolled bug here "produces a signature check that accepts forgeries and looks perfect" rather than one that visibly disagrees with every authenticator app, as a hand-rolled TOTP bug would (`docs/PLAN/07-BACKEND-ARCHITECTURE.md`'s "don't reinvent cryptography").

### Relying Party configuration

The RP ID and origin are derived **once, at startup, from the deployment's own configured issuer URL** (`mfa.NewWebAuthn`) — never from a request header. This is the entire phishing defense: a service that took its expected origin from an `Origin` header sent by the caller would accept whatever a lookalike page sent. Because this service resolves tenants by path and session token rather than by subdomain (`TASKS/BACKLOG.md` `PG-33`), one RP ID covers every organization in the deployment — a subdomain-per-organization design would instead give each organization credentials that do not work on any other subdomain.

### Two ceremonies

- **Registration** (`BeginRegistration`/`FinishRegistration`) writes nothing until the ceremony completes — there is no half-finished "pending" WebAuthn factor the way TOTP has one, because a completed registration ceremony *is* the proof the credential works; nothing further needs confirming. A credential is activated (`Status: active`) immediately on `FinishRegistration`.
- **Authentication** (`BeginLogin`/`FinishLogin`) is invoked from the MFA framework during a login challenge, never standalone. `FinishLogin` checks the presented credential against **only this user's own stored credentials** (loaded by user id before validation), so a credential resolving to nobody in that set is refused rather than matched against another user's row.

**Signature counter check (clone detection)**: `counterAcceptable` accepts a stored-and-reported-zero pair (most platform authenticators — phone biometrics, Touch ID — never increment their counter, and refusing those would exclude most users), but otherwise requires the presented counter to strictly increase. A counter that goes backward or repeats is `ErrClonedAuthenticator`, logged at `WARN`, and refused — this is the defense against two authenticators presenting one private key.

**User verification** is requested as `Preferred`, not `Required`, at registration — an authenticator that can verify the user (PIN, fingerprint, face) should, but one that cannot is still accepted as a factor. What is not done is pretending afterward that verification happened: the `UserVerified` flag is recorded from what the authenticator actually reported.

### Hosted passkey registration page (`/account/passkeys`, P3-10, `PG-43`)

WebAuthn credentials bind to the relying party (this service's own origin), and the console is a separately-deployed frontend on its own origin (`docs/PLAN/06-FRONTEND-ARCHITECTURE.md`) — a ceremony run from the console's page would be refused by the browser. So registration is a hosted page served by this service itself, which the console redirects users to and back from.

Access requires an **SSO session that authenticated recently** — within `mfa.RecentAuthentication` (10 minutes), the same bound that gates removing a factor or regenerating recovery codes via the Management API. This closes the specific risk that a stolen but still-live session could add an attacker's own passkey and make a takeover permanent and (nominally) MFA-protected.

The page carries an inline script under the hosted-page CSP rules that also govern the login page (`PG-40`: hash-pinned, no fetch, no `connect-src`, one browser API call, one form submission).

### Passkey sign-in at login

`WebAuthnStep` (`backend/internal/login/mfa.go`) verifies an assertion against **the session this specific login issued** (the `WebAuthnSession` stored inside the server-side MFA challenge, never reconstructed from anything the browser holds) — a relayed assertion captured by a lookalike page carries that page's origin and fails this check regardless of anything else.

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| RP ID / origin source | Configured issuer URL only, never a request header | `mfa.NewWebAuthn` |
| User verification requested | `Preferred`, recorded honestly either way | `BeginRegistration` |
| Signature counter | Zero/zero accepted; otherwise must strictly increase | `counterAcceptable` |
| Recent-authentication window for registration and removal | 10 minutes | `mfa.RecentAuthentication` |
| Assertion/attestation size bound | 64 KiB, truncated before reaching the CBOR parser | `maxAssertionBytes`, `maxCredentialBytes` |
| Registration ceremony TTL (hosted page) | 5 minutes | `login.passkeyRegistrationTTL` |

## Interfaces

| Route | Kind | Notes |
|---|---|---|
| `GET`/`POST` `/account/passkeys` | Hosted page (not a REST API operation) | Requires a recent SSO session; `return_to` validated against a registered application origin |
| `POST /login/mfa/webauthn` | Hosted page | Answers an in-progress login's MFA challenge |

There is **no Management API endpoint to begin or confirm a WebAuthn enrollment** — `GET /v1/me/mfa` reports whether the deployment can serve passkeys (`available_types` includes `webauthn` when `PasskeysAvailable` is set), but registration itself only happens through the hosted page above, for the origin-binding reason stated in As Built.

## Security Considerations

- **Phishing resistance is the entire point.** The origin binding (never from a request) is what TOTP cannot provide and what makes WebAuthn the strongest factor this service offers.
- **Cloned authenticator**: signature-counter regression, logged distinctly from an ordinary wrong answer, though the browser sees the same uniform failure message as any other MFA refusal — see `06-MULTI-FACTOR-AUTHENTICATION.md`.
- **Session-to-ceremony binding** prevents a passkey being registered onto an account other than the one that started the ceremony (`docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`).

## Verification

| Test | File |
|---|---|
| `TestSigningInWithARealPasskeyOpensAnHwkSession` | `backend/internal/login/passkeysignin_integration_test.go` |
| `TestAnAssertionFromAnUnregisteredKeyIsRefused` | same |
| `TestAPasskeyRegistersOnlyToTheSignedInUser` | same |
| WebAuthn ceremony unit/integration coverage | `backend/internal/mfa/webauthn_integration_test.go` |

Per `MEMORY/records/2026-09-15-P3-14-test-suite.md`: end-to-end passkey coverage is **Chromium only**, since Playwright's virtual authenticator is the only one it can drive — cross-browser passkey behavior is explicitly not exercised.

## Not Yet Built / Open Questions

- **Passwordless / discoverable-credential ("usernameless") sign-in is not implemented.** Every WebAuthn ceremony today is issued against a known user id resolved from a password already entered; there is no allow-list-free, discoverable-credential login path.
- Cross-browser passkey end-to-end coverage beyond Chromium.

## Related Documents

- `docs/IDENTITY-PROTOCOL/06-MULTI-FACTOR-AUTHENTICATION.md`
- `docs/SESSION-MANAGEMENT/00-SESSION-ARCHITECTURE.md`
- `MEMORY/specs/P3-05-webauthn.md`, `P3-10-mfa-tab.md`
- `TASKS/BACKLOG.md` PG-33 (tenant resolution by path, not subdomain), PG-40 (hosted-page script rules), PG-43 (hosted passkey page)
