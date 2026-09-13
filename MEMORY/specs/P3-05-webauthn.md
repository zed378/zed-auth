# P3-05 — WebAuthn / Passkey Support

Feature specification, per `docs/PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`.

| | |
|---|---|
| **Task** | `TASKS/PHASE-3-ADVANCED-SECURITY.md` § P3-05 |
| **Phase** | Phase 3 — Advanced Security |
| **Surface** | backend + the hosted login flow |
| **Plan refs** | `docs/PLAN/02-REQUIREMENTS.md` FR-2, `docs/PLAN/05-API-CONTRACT.md` § MFA, `docs/PLAN/07-BACKEND-ARCHITECTURE.md` § "don't reinvent cryptography" |
| **Depends on** | `P3-01` (framework), `P3-03` (the challenge step) |
| **Written** | 2026-09-13 |

---

## 1. Business objective

A second factor that **cannot be phished**. TOTP can: a convincing lookalike
page asks for the six digits and relays them within the 30-second window, and
the user has no way to tell. WebAuthn closes that by construction — the
authenticator signs over the origin it is actually talking to, so a credential
registered for `auth.example.test` produces nothing a lookalike can use.

That property is the entire point of this task. Everything below that looks like
fussiness — the origin check, the challenge binding, the RP ID — is the part
that delivers it, and skipping any of it leaves a second factor that is merely
inconvenient rather than phishing-resistant.

## 2. Actors

| Actor | Interest |
|---|---|
| A user with a passkey | Signs in by touching a key or a fingerprint reader |
| A user on an unsupported browser | Must still be able to sign in, with TOTP |
| A user with several devices | Registers each; losing one is not an account loss |
| A phisher | Must get nothing from a relayed ceremony |
| An operator | Needs a cloned authenticator to be detectable |

## 3. Functional requirements

| # | Requirement |
|---|---|
| F-1 | Registration and authentication ceremonies, using a maintained library |
| F-2 | Origin, challenge and RP ID verified on every authentication |
| F-3 | Several credentials per user, individually removable |
| F-4 | The signature counter is checked, and a regression is treated as a cloned authenticator |
| F-5 | User verification is recorded, so a true second factor is distinguishable from mere presence |
| F-6 | `amr` says `hwk` for WebAuthn and `otp` for TOTP — never interchangeable |
| F-7 | A browser without WebAuthn falls back to TOTP with an explanation |

## 4. Non-functional requirements

- The password page keeps **`default-src 'none'` and no script**. See § 20.
- A deployment with no WebAuthn credentials behaves exactly as it does today.

## 5. Dependencies

**A new dependency**, and `docs/PLAN/07` asks for one by name: *don't reinvent
cryptography*. `github.com/go-webauthn/webauthn` — CBOR decoding, COSE key
parsing, attestation formats, ES256/RS256/EdDSA verification.

This is the opposite call from `P3-02`, where TOTP was implemented in the
repository rather than vendored, and the difference is worth stating because
the reasoning there argued against new dependencies in the authentication path:

| | TOTP | WebAuthn |
|---|---|---|
| Size | ~30 lines | CBOR + COSE + attestation + three signature algorithms |
| Stability | RFC 6238, frozen since 2011 | An evolving W3C spec with live errata |
| Getting it wrong | Codes nobody's app agrees with — **visible** | A signature that verifies when it should not — **invisible** |

The last row decides it. A hand-rolled TOTP bug announces itself; a hand-rolled
COSE bug is a credential that accepts forgeries and looks perfect.

`PG-27` (no SBOM) remains open and this makes it more pressing, not less.

## 6. Data model changes

**None.** `20260912000028` already added `credential_id`, `public_key` and
`sign_count` to `user_mfa_factors`, and `docs/PLAN/04` names all three. The
`data jsonb` column carries what is neither secret nor indexed — the AAGUID,
the transports, and whether user verification was performed at registration.

There is **no secret to protect on the server side** for this factor type
(card step 5), which is a genuine advantage: a database dump yields public
keys, and a public key is public.

## 7. API contract

No new REST endpoint. Two steps inside the hosted flow:

```
GET  /login/mfa            the challenge page, now able to offer a passkey
POST /login/mfa/webauthn   the assertion
```

Registration ceremonies belong with `P3-12`'s account screen, for the reason
`P3-02`'s enrolment endpoint and `P3-04`'s regeneration do: they need "requires
recent authentication", and there is no self-service surface yet. **What lands
here is authentication**, which is what the DoD's abuse cases are about.

## 8. Frontend changes

A WebAuthn step in the hosted login flow, with **script** — see § 20.

## 9. Authorization rules

The ceremony authenticates the user the challenge names. A credential is bound
to one user at registration and the assertion is looked up by credential id
**within that user's credentials**, so a credential belonging to somebody else
resolves to nothing rather than to them.

## 10. Validation rules

| Input | Rule |
|---|---|
| `origin` | Must equal the deployment's own, exactly. Not a prefix, not a suffix |
| `challenge` | Must equal the one this login issued, consumed once |
| `rpIdHash` | Must equal SHA-256 of the configured RP ID |
| `signCount` | Must exceed the stored one, when the authenticator reports one |
| The assertion | Bounded before parsing; CBOR is an attacker-supplied encoding |

## 11. Error handling

Every failure renders the challenge page's uniform message. The operator's log
distinguishes an origin mismatch — which is a phishing attempt or a
misconfiguration, never a typo — from a signature that did not verify.

A **counter regression is different**: it is not a wrong answer but a signal
that two authenticators are presenting one credential. It refuses and says so in
the log at a level an operator will see.

## 12. Edge cases

| Case | Behaviour |
|---|---|
| The authenticator reports no counter (always 0) | Accepted. Many platform authenticators do not count; refusing would exclude most phones |
| A user has both TOTP and a passkey | Both offered; either completes the challenge |
| The browser has no `navigator.credentials` | The passkey form is not rendered and TOTP is, with an explanation |
| A credential removed mid-challenge | Refused, like any factor the challenge named and the store no longer has |
| Two tabs, two ceremonies | Each challenge holds its own WebAuthn challenge; answering one does not help the other |

## 13. Abuse cases

| # | Scenario | Control |
|---|---|---|
| A-1 | Phishing via a lookalike origin | The origin check. **Tested with a mismatched origin**, which is the DoD's own requirement |
| A-2 | Replay an assertion across origins | Same control, plus the challenge is consumed |
| A-3 | Replay an assertion to the same origin | The challenge is single-use and bound to this login |
| A-4 | Register a credential to another user's account (`docs/SECURITY/02` §2) | Registration binds to the authenticated user server-side; the request names no user |
| A-5 | A cloned authenticator | The signature counter |
| A-6 | Downgrade a passkey session to look like a TOTP one, or the reverse | `Type.AMR()` maps them to different values and `StepUp` is a subset test |

## 14. Logging and audit

The existing `user.mfa.success` / `user.mfa.failed`, with `factor_type` now
able to say `webauthn`. Never the assertion, never the public key, never the
credential id — an identifier that follows a user across logins does not belong
in a table with 24-month retention.

A counter regression logs at warn with the factor id and both counters, because
an operator investigating a cloned authenticator needs exactly those.

## 15. Security controls

- Origin, RP ID and challenge verified by the library, with this service
  supplying the expected values from configuration rather than from the request.
- The challenge is server-side, in the existing per-login Redis state, and
  consumed on use.
- The assertion is size-bounded before it reaches a CBOR parser.
- The script on the WebAuthn step is **hash-pinned**, not nonce-allowed (§ 20).

## 16. Testing strategy

| Level | What |
|---|---|
| Unit | The ceremony's decisions, with a software authenticator: good assertion, wrong origin, wrong challenge, replayed challenge, regressed counter |
| Integration | Against real Postgres and Redis: registration then authentication, several credentials, individual removal |
| Security | A-1 through A-6, each failing when its control is reverted |
| Mutation | Every control above |

## 17. Acceptance criteria

The card's Definition of Done, **except item 1** — see § 21.

## 18. Implementation sequence

1. The library, and a configuration surface for the RP ID and origin.
2. The credential store.
3. The registration ceremony (used by tests and by `P3-12` later).
4. The authentication ceremony and its framework entry point.
5. The login step, its script, and its CSP.
6. Tests, then the mutation run.

## 19. Rollback strategy

No migration. A build without the verifier registered offers no passkey and
challenges with TOTP, so a rollback degrades rather than locking anybody out —
the same property `P3-03` relies on.

## 20. Technical risks

**The hosted login page has no JavaScript, and WebAuthn cannot work without
it.** This is the task's central conflict and it is a real one.

`P1-12` did not merely happen to omit script; it is load-bearing:

> There is no JavaScript. Not "it degrades gracefully" — none at all, which is
> what makes `default-src 'none'` an achievable policy rather than an
> aspirational one, and what leaves no DOM sink for an injected value to reach.

`P3-03` already saw this coming and deferred it here in as many words: *"WebAuthn
needs script, and this page has none. It will need its own step."*

**Resolution**: a separate step, and the password page is not touched.

- `/login` keeps `default-src 'none'`, no `script-src`, no script. The page
  where a password is typed is unchanged.
- `/login/mfa` keeps its current policy when it offers only TOTP.
- The WebAuthn step gets `script-src '<sha256 of the inline script>'` — a
  **hash**, not a nonce, for the reason the stylesheet is hash-pinned: a nonce
  changes per response and authorises whatever the server put it on, while a
  hash authorises one exact block of code and nothing else. If the script
  changes by a byte, it stops executing.
- The script does one thing: call `navigator.credentials.get()` and post the
  result. No framework, no network calls of its own, no DOM sink for an
  injected value — `connect-src` stays `'none'`.

The residual is honest: this page has script and the others do not, so it is the
one page in the estate where a content-injection bug could reach a script
context. That is the price of phishing resistance, it is contained to one step,
and it is recorded as `PG-40` rather than left for somebody to discover by
reading the CSP.

**The library is a new dependency in the authentication path**, which `P3-02`
argued against. § 5 says why this case is the other way round.

## 21. What this task does not deliver

- **"Registration and authentication work across at least two browser and
  platform combinations"** (DoD item 1). **Not achievable in this environment
  and not claimed.** Playwright can drive a virtual authenticator through the
  Chrome DevTools Protocol, which covers Chromium; Firefox and WebKit expose no
  equivalent, and a platform authenticator (Touch ID, Windows Hello) needs real
  hardware. What ships is the ceremony verified against a software authenticator
  plus a Chromium virtual one. **The item stays open on the card**, with what
  would close it written down: two real devices, or a hosted test lab.
- **Self-service registration** of a passkey. `P3-12`, with the account screen.
- **WebAuthn as the sole factor** (passwordless). `docs/PLAN/05` calls it a
  later phase and the roadmap agrees.
