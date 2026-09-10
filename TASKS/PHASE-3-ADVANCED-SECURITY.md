# Phase 3 — Advanced Security

**Goal**: raise the bar on what "authenticated" means — multi-factor authentication, refresh token rotation with reuse detection, login anomaly detection, and self-service session management.

**Why now**: Phase 1 and 2 established identity and permissions. This phase hardens the identity half against the attacks that actually happen — stolen passwords, stolen tokens, and stolen sessions. `docs/PLAN/16` notes that Phase 3 and Phase 4 can be swapped as whole phases if an enterprise client needs SAML sooner; they must not be interleaved task by task.

**Prerequisite**: Phase 2 exit checklist fully satisfied, plus the Phase 3 threat-model review from `P2-17`.

**Roadmap reference**: `docs/PLAN/16-IMPLEMENTATION-ROADMAP.md` § Phase 3.

---

## Task Summary

| ID | Task | Surface | Size | Depends on |
|---|---|---|---|---|
| P3-01 | MFA framework and step-up architecture | backend | L | P1-11, P2-10 |
| P3-02 | TOTP enrollment | backend | M | P3-01 |
| P3-03 | TOTP verification at login | backend | M | P3-02 |
| P3-04 | Recovery codes and lost-device process | backend | M | P3-02 |
| P3-05 | WebAuthn / passkey support | backend | L | P3-01 |
| P3-06 | Refresh token rotation with reuse detection | backend | L | P1-07 |
| P3-07 | Organization-mandated MFA enforcement | backend | M | P3-03, P2-10 |
| P3-08 | Login anomaly detection | backend | L | P1-11, P1-14 |
| P3-09 | Session management API (admin and self-service) | backend | M | P1-11 |
| P3-10 | Console — User detail MFA tab | console | M | P3-02, P3-05 |
| P3-11 | Console — Sessions tab (admin and self-service) | console | M | P3-09 |
| P3-12 | Console — personal account settings | console | L | P3-09, P3-10 |
| P3-13 | Docs — MFA and session security guides | docs | M | P3-05, P3-06 |
| P3-14 | Phase 3 test suite | backend, console | L | all above |
| P3-15 | Phase 3 acceptance validation | all | M | P3-14 |

---

## P3-01 — MFA Framework and Step-Up Architecture

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-11, P2-10 |
| **Plan refs** | `docs/PLAN/05-API-CONTRACT.md` § MFA, `docs/PLAN/04-DATA-MODEL.md` § `sessions` (`auth_methods`), `docs/PLAN/08-AUTHORIZATION.md` § Least Privilege |
| **Spec required** | Yes — authentication core |
| **Surface** | backend |

**Goal** — One framework that both TOTP and WebAuthn plug into, with `amr` and `auth_methods` accurate enough that consumer applications can make real step-up decisions.

**Steps**
1. Define a factor interface covering enrollment, verification, and removal, so `P3-02` and `P3-05` are implementations rather than parallel systems.
2. Extend the login flow with a distinct MFA challenge step, holding partially-authenticated state server-side with a short expiry. A partially-authenticated session must be unusable for anything except completing the challenge.
3. Record every factor actually used in `sessions.auth_methods` (`docs/PLAN/04`), and propagate it into the token's `amr` claim (`docs/PLAN/05`).
4. Implement step-up: an action requiring a stronger factor can force re-verification even within a valid session, using `prompt=login` and an ACR/`amr` requirement. `docs/PLAN/08` § Least Privilege recommends step-up for sensitive administrative actions specifically.
5. Rate-limit MFA verification attempts per session and per user, mirroring `P1-13`'s cooldown-not-lockout approach.
6. Audit enrollment, verification success and failure, and removal — factor removal in particular is a favorite account-takeover step.
7. Handle the multi-factor case: a user may enroll both TOTP and a passkey, and losing one must not lock them out.

**Definition of Done**
- [ ] Both factor types implement one interface, verified by an architecture test.
- [ ] A partially-authenticated session cannot access any resource or obtain a token.
- [ ] `amr` accurately reflects the factors used, asserted per flow.
- [ ] Step-up forces re-verification even with a valid session.
- [ ] Verification attempts are rate-limited.
- [ ] All MFA lifecycle events are audited.

**Abuse cases to test**
- Skipping the MFA step by manipulating the partially-authenticated state (`docs/SECURITY/02` §3, §11).
- Brute-forcing a six-digit TOTP within its validity window.
- Removing another user's factor (`docs/SECURITY/02` §2).
- Downgrade: forcing a weaker factor when a stronger one is enrolled.

---

## P3-02 — TOTP Enrollment

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P3-01 |
| **Plan refs** | `docs/PLAN/02-REQUIREMENTS.md` FR-2, `docs/PLAN/05-API-CONTRACT.md` § MFA, `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` (MFA tab) |
| **Spec required** | Yes — credential handling |
| **Surface** | backend |

**Goal** — Standard RFC 6238 TOTP enrollment where the shared secret is treated as the credential it is.

**Steps**
1. Generate a cryptographically random secret of adequate length, stored in `user_mfa_factors.secret_encrypted` encrypted at rest (`docs/PLAN/04`, `docs/PLAN/09` § Transport & Storage).
2. Return the provisioning URI and a QR code once, during enrollment only.
3. Require verification of a generated code before the factor is activated — enrolling without proof is how users lock themselves out.
4. Require the user's current password (or a recent authentication) to begin enrollment, so a hijacked session cannot silently add a factor.
5. Set a clock-skew tolerance of one step in each direction; wider windows meaningfully weaken the factor.
6. Prevent replay: a code already used must not be accepted again within its window.
7. Never log or return the secret after enrollment.
8. Audit enrollment start and completion.

**Definition of Done**
- [ ] The secret is encrypted at rest and never returned after enrollment.
- [ ] The factor activates only after a successful verification.
- [ ] Enrollment requires recent authentication.
- [ ] A used code cannot be replayed within its window, verified by test.
- [ ] Clock skew tolerance is exactly one step each way.

---

## P3-03 — TOTP Verification at Login

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P3-02 |
| **Plan refs** | `docs/PLAN/05-API-CONTRACT.md` § MFA, `docs/PLAN/11-TESTING.md` § E2E, `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 3 |
| **Spec required** | Yes — authentication |
| **Surface** | backend |

**Goal** — The challenge step in the login flow: correct password plus wrong code is rejected, which `docs/PLAN/11` names explicitly as an E2E test case.

**Steps**
1. After successful password verification, if a factor is enrolled, transition to the challenge step rather than issuing a session.
2. Rate-limit code attempts aggressively per challenge — a six-digit code has a small keyspace, and unlimited attempts defeat the factor entirely.
3. Expire the challenge after a short window, requiring a restart from the password step.
4. Keep messaging uniform: whether the password or the code was wrong must not be distinguishable to an attacker probing the flow.
5. Record `["password", "totp"]` in `auth_methods` on success (`docs/PLAN/04`'s own example).
6. Support the "remember this device" option only if it is designed properly — a signed, revocable device token with a bounded lifetime, visible and revocable from the sessions screen. If that cannot be delivered in this phase, omit it rather than shipping a weak version.
7. Audit MFA verification success and failure separately from password success and failure.

**Definition of Done**
- [ ] Correct password with a wrong code is rejected — the literal `docs/PLAN/11` E2E case.
- [ ] Code attempts are rate-limited per challenge and per user.
- [ ] Challenges expire.
- [ ] `auth_methods` and `amr` both reflect the factors used.
- [ ] Password-versus-code failure is not distinguishable.
- [ ] Any "remember this device" token is revocable and visible to the user.

---

## P3-04 — Recovery Codes and Lost-Device Process

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P3-02 |
| **Plan refs** | `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 3 ("including recovery from a lost device — documented process, even if manual at first") |
| **Spec required** | Yes — account recovery is an attack path |
| **Surface** | backend |

**Goal** — A recovery path, because a factor with no recovery path produces either permanently locked-out users or an ad-hoc support process that becomes the weakest link in the whole system.

**Steps**
1. Generate single-use recovery codes at enrollment, displayed exactly once, stored hashed with the same rigor as passwords.
2. Each code is usable once; consuming one invalidates it. Warn the user as the remaining count runs low.
3. Allow regeneration, which invalidates all previous codes and requires re-authentication.
4. Define the administrator-assisted reset path: which manager role may reset another user's MFA, what identity verification is required out-of-band, and how it is audited. `docs/PLAN/17` accepts a manual process — it does not accept an undocumented one.
5. Rate-limit recovery code attempts as strictly as password attempts.
6. Audit every recovery use and every admin-assisted reset with elevated visibility; these are exactly the events an incident review will look for (`docs/SECURITY/04-INCIDENT-RESPONSE-PLAYBOOKS.md`).

**Definition of Done**
- [ ] Recovery codes are shown once, stored hashed, and single-use.
- [ ] Regeneration invalidates all prior codes.
- [ ] The admin-assisted reset process is documented, permission-gated, and audited.
- [ ] Recovery attempts are rate-limited.
- [ ] An end-to-end lost-device recovery has been walked through and recorded.

**Abuse cases to test**
- Social-engineering the admin-assisted path — mitigated procedurally; the documented process must name the verification requirement.
- Brute-forcing recovery codes.
- Recovery code reuse.

---

## P3-05 — WebAuthn / Passkey Support

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P3-01 |
| **Plan refs** | `docs/PLAN/02-REQUIREMENTS.md` FR-2, `docs/PLAN/05-API-CONTRACT.md` § MFA, § Passwordless |
| **Spec required** | Yes — authentication |
| **Surface** | backend |

**Goal** — Phishing-resistant authentication via WebAuthn, as a second factor and — per `docs/PLAN/05` — potentially as the sole factor later.

**Steps**
1. Implement registration and authentication ceremonies with a well-maintained library. Do not implement the CBOR and attestation parsing by hand (`docs/PLAN/07`: don't reinvent cryptography).
2. Configure the Relying Party ID correctly for the deployment's domain structure. This interacts directly with `P2-09`'s tenant resolution — a subdomain-per-org strategy has real consequences for credential scoping, and getting it wrong means credentials that work on one subdomain and not another.
3. Verify challenge, origin, and signature counter on every authentication. Skipping the origin check discards the phishing resistance that is the entire point.
4. Support multiple registered credentials per user, so losing one device is not an account loss.
5. Store credential IDs and public keys; there is no secret to protect on the server side, which is a genuine advantage worth stating in the docs.
6. Handle user verification flags to distinguish a true second factor from mere presence.
7. Record `["password", "webauthn"]` or `["webauthn"]` in `auth_methods` accordingly.
8. Degrade gracefully on unsupported browsers by falling back to TOTP with a clear explanation.

**Definition of Done**
- [ ] Registration and authentication work across at least two browser and platform combinations.
- [ ] Origin and challenge verification are enforced, verified by a test using a mismatched origin.
- [ ] Multiple credentials per user are supported and individually removable.
- [ ] The signature counter is checked, with cloned-authenticator detection where the authenticator supports it.
- [ ] `amr` distinguishes WebAuthn from TOTP.
- [ ] Unsupported browsers fall back cleanly.

**Abuse cases to test**
- Phishing via a lookalike origin (must fail on the origin check).
- Credential replay across origins.
- Registering a credential to another user's account (`docs/SECURITY/02` §2).

---

## P3-06 — Refresh Token Rotation with Reuse Detection

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-07 |
| **Plan refs** | `docs/PLAN/09-SECURITY.md` § Tokens & Keys, `docs/PLAN/11-TESTING.md` § Security Testing, `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 3, `docs/PLAN/10-THREAT-MODEL.md` |
| **Spec required** | Yes — token security core |
| **Surface** | backend |

**Goal** — A rotated refresh token cannot be reused, verified by automated test — the exact wording of `docs/PLAN/17`'s Phase 3 criterion.

**Steps**
1. On every refresh, issue a new refresh token and invalidate the old one atomically.
2. Track the token family via `refresh_tokens.family_id` and mark rotation with `replaced_by` (`docs/PLAN/04`) — presenting a token that already has a `replaced_by` is the reuse signal.
3. On detecting reuse of an already-rotated token, **revoke the entire family immediately**. Reuse means either the token was stolen or a client is misbehaving; in both cases the safe action is the same.
4. Alert on reuse detection (`docs/PLAN/13` § Alerting) and audit it — this is a strong theft signal, not routine noise.
5. Handle the legitimate race: a client that retries a refresh after a network timeout may present the same token twice. Distinguish this from theft with a short grace window keyed to the immediately-preceding token, and document the reasoning. Getting this wrong logs real users out constantly, which trains teams to disable the protection.
6. Enforce both an absolute family lifetime and an idle timeout.
7. Store only hashes (`docs/PLAN/04`), and ensure revocation is immediate rather than TTL-bound.
8. Bind refresh tokens to the client, and consider binding to the session where the flow allows.

**Definition of Done**
- [ ] A rotated refresh token cannot be reused — automated test, per `docs/PLAN/17`.
- [ ] Reuse revokes the entire family and raises an alert.
- [ ] The retry grace window is documented and tested for both the legitimate and the malicious case.
- [ ] Only hashes are stored.
- [ ] Family absolute and idle lifetimes are enforced.

**Abuse cases to test**
- Token replay after rotation (`docs/PLAN/10` § High-Priority Abuse Scenarios).
- Stolen refresh token used in parallel with the legitimate client — must be detected and the family killed.
- Refresh token used by a different client.
- Indefinite session extension by continuous refreshing beyond the absolute lifetime.

---

## P3-07 — Organization-Mandated MFA Enforcement

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P3-03, P2-10 |
| **Plan refs** | `docs/PLAN/02-REQUIREMENTS.md` FR-6, `docs/PLAN/08-AUTHORIZATION.md` Part B, `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 2 |
| **Spec required** | Yes — policy enforcement |
| **Surface** | backend |

**Goal** — Deliver the enforcement half of `mfa_required`, which `P2-10` deliberately stored without enforcing because MFA did not yet exist.

**Steps**
1. When `mfa_required` is true, a user without an enrolled factor is routed into a forced enrollment flow at login rather than being denied outright — denial without a path forward creates a support queue, not security.
2. Define the grace behavior for existing users at the moment the policy is enabled: an immediate hard requirement locks out everyone at once. A bounded grace period with clear warnings is usually right; whichever is chosen, it must be deliberate and documented.
3. Ensure the forced-enrollment state cannot be bypassed by navigating elsewhere or by using a token obtained before the policy changed.
4. Exempt nothing silently. If service accounts or break-glass administrators need an exemption, make it explicit, individually audited, and visible in the console.
5. Warn the administrator enabling the policy about how many users have no factor enrolled.
6. Audit policy activation and every forced enrollment.

**Definition of Done**
- [ ] Enabling `mfa_required` forces enrollment at next login, with no bypass path.
- [ ] Grace behavior is documented and implemented deliberately.
- [ ] Any exemption is explicit, audited, and visible.
- [ ] The admin sees the impact count before enabling.
- [ ] `docs/PLAN/17`'s Phase 2 criterion about MFA-required being enforced rather than merely stored is now fully satisfied.

---

## P3-08 — Login Anomaly Detection

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-11, P1-14 |
| **Plan refs** | `docs/PLAN/09-SECURITY.md` § Audit & Anomaly Detection, `docs/PLAN/13-OBSERVABILITY.md` § Alerting, `docs/SECURITY/03-DETECTION-AND-MONITORING.md` |
| **Spec required** | Yes — detection control |
| **Surface** | backend |

**Goal** — Notice and notify on logins that look unlike the user's normal pattern — new device, new location, impossible travel — without generating so much noise that the notifications get filtered.

**Steps**
1. Build a lightweight device fingerprint from stable signals already collected in `sessions` (`docs/PLAN/04`: `ip`, `user_agent`). Avoid invasive fingerprinting — this is an identity provider, and the privacy posture matters.
2. Coarse geolocation from IP for new-location detection. Coarse is deliberate: city-level is enough to notify, and finer resolution adds privacy risk without adding signal.
3. Impossible-travel detection: two successful logins from locations that cannot both be true given the elapsed time.
4. Notify the user on a genuinely new device or location, with a clear path to report "this wasn't me" that revokes sessions and forces a password change.
5. Tune the threshold honestly. A notification on every login trains users to ignore them, which is worse than no notification at all.
6. Feed the signals into `docs/PLAN/13`'s alerting and `docs/SECURITY/03`'s monitoring.
7. Decide whether anomalies trigger step-up or only notification. Step-up on a false positive is disruptive; notification alone is passive. Record the decision and its reasoning.

**Definition of Done**
- [ ] New-device and new-location logins are detected and notified.
- [ ] Impossible travel is detected and tested with synthetic data.
- [ ] The "this wasn't me" path revokes sessions and forces a credential change.
- [ ] The false-positive rate is measured against real staging traffic before enabling notifications broadly.
- [ ] The step-up-versus-notify decision is recorded in `MEMORY/DECISIONS.md`.
- [ ] Signals appear in monitoring per `docs/SECURITY/03`.

---

## P3-09 — Session Management API

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-11 |
| **Plan refs** | `docs/PLAN/02-REQUIREMENTS.md` FR-5, `docs/PLAN/05-API-CONTRACT.md` § Endpoint Structure, `docs/UI-UX/04-USER-FLOWS.md` Flow 4, `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 3 |
| **Spec required** | Yes — session control |
| **Surface** | backend |

**Goal** — Users can view and revoke their own sessions (FR-5), and administrators can do the same for users in their organization, with revocation taking effect immediately.

**Steps**
1. Implement `GET /v1/organizations/{org_id}/users/{user_id}/sessions` and `DELETE .../sessions/{session_id}`, plus a self-service path for the authenticated user's own sessions.
2. Return useful, non-sensitive detail: device and browser summary, coarse location, creation time, last activity, and a clear marker for the current session.
3. Make revocation immediate — not TTL-bound. `docs/PLAN/17` requires that a revoked session is immediately unusable, and a cache TTL is not "immediately."
4. Revoking a session also revokes the refresh tokens issued through it.
5. Authorize correctly: a user sees only their own sessions; `ORG_ADMIN` and `ORG_OWNER` see any user's in their organization (`docs/UI-UX/08`).
6. Support "revoke all other sessions" as a single action, which is what a user who suspects compromise actually wants.
7. Audit every revocation with actor and target.

**Definition of Done**
- [ ] A user sees only their own sessions; an admin sees their organization's.
- [ ] Revocation is effective on the very next request, verified by test.
- [ ] Refresh tokens from a revoked session are invalidated.
- [ ] "Revoke all other sessions" preserves the current one.
- [ ] Revocations are audited.

**Abuse cases to test**
- Revoking another user's session without authorization (`docs/SECURITY/02` §2).
- A revoked session still usable through a cached path.
- Session detail leaking precise location or full user-agent fingerprints to an admin beyond what is justified.

---

## P3-10 — Console: User Detail MFA Tab

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P3-02, P3-05 |
| **Plan refs** | `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` (MFA tab), `docs/UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md` |
| **Spec required** | No — implementation chain mandatory |
| **Surface** | console |

**Goal** — Admins see MFA status read-only; users manage their own factors fully — the split `docs/UI-UX/08` specifies.

**Steps**
1. Run the full `docs/UI-UX/19` chain.
2. Admin view: enrolled factor types and enrollment dates as badges, strictly read-only, plus the audited reset action from `P3-04` where permitted.
3. Self-service view: enroll TOTP with a QR code, register a passkey, remove a factor, and regenerate recovery codes.
4. Recovery codes display once, with copy and download affordances and unmistakable warning copy.
5. Removing the last factor while `mfa_required` is on must be blocked with a clear explanation, not a silent failure.
6. Enrollment errors — wrong code, expired challenge, unsupported browser — each get their own message per `docs/UI-UX/14`.

**Definition of Done**
- [ ] Admins cannot enroll or remove factors for another user except via the audited reset path.
- [ ] Enrollment flows for both factor types complete successfully in an E2E test.
- [ ] Recovery codes are shown once with clear warnings.
- [ ] Removing the last factor under a mandatory-MFA policy is blocked with an explanation.
- [ ] Accessibility requirements are met, including for the QR code (which needs a text alternative).

---

## P3-11 — Console: Sessions Tab

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P3-09 |
| **Plan refs** | `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` (Sessions tab), `docs/UI-UX/04-USER-FLOWS.md` Flow 4, `docs/UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md` § Worked Example |
| **Spec required** | No — implementation chain mandatory |
| **Surface** | console |

**Goal** — Implement `docs/UI-UX/04` Flow 4 exactly, following the worked example in `docs/UI-UX/19` — which specifies this component down to its screen-reader label.

**Steps**
1. Follow `docs/UI-UX/19`'s worked example for the revoke button precisely: secondary-styled button per row, single click with no modal (it is low-risk per `docs/UI-UX/09`'s feedback-timing table), optimistic removal with rollback on error, spinner at constant width, inline error next to the row.
2. Screen-reader label must be "Revoke session on [device/browser]", not just "Revoke" — `docs/UI-UX/19` calls this out specifically because multiple sessions are listed.
3. Table showing device, location, created, last active, with the current session clearly marked and not accidentally revocable without warning.
4. "Revoke all other sessions" as a separate, prominent action.
5. This screen is in scope for full mobile optimization via personal account settings (`docs/UI-UX/16-MOBILE-UX.md`), so the button must remain a single tap target at mobile width.
6. Empty state: "no other active sessions" reads differently from "no sessions," since the current one always exists.

**Definition of Done**
- [ ] The implementation matches `docs/UI-UX/19`'s worked example on every one of its twelve rows.
- [ ] The screen-reader label disambiguates between sessions.
- [ ] Optimistic update with rollback is covered by a component test.
- [ ] Flow 4 is covered end-to-end by an E2E test.
- [ ] The revoke target is usable at mobile width.

---

## P3-12 — Console: Personal Account Settings

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P3-09, P3-10 |
| **Plan refs** | `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` (Personal account settings), `docs/UI-UX/16-MOBILE-UX.md`, `docs/UI-UX/12-RESPONSIVE-BEHAVIOR.md` |
| **Spec required** | No — implementation chain mandatory |
| **Surface** | console |

**Goal** — The self-service surface for end users — and the **only** console screen requiring full mobile optimization, because end users, unlike admins, will reasonably open it on a phone (`docs/UI-UX/08` § Responsive Scope).

**Steps**
1. Run the full `docs/UI-UX/19` chain for every component on the screen.
2. Change password, with the current password required and `P1-02`'s policy shown before submission rather than only on rejection.
3. Manage own MFA factors, reusing `P3-10`'s self-service components.
4. Manage own sessions, reusing `P3-11`'s components.
5. Show linked social logins as a placeholder state until Phase 4 delivers them — labelled as unavailable, never implied as working.
6. Full mobile optimization per `docs/UI-UX/16`: touch targets, no horizontal scroll, and forms that work with a mobile keyboard.
7. Reachable from anywhere in the console, since a user may arrive at it from any context.

**Definition of Done**
- [ ] Every action works on a real mobile viewport, verified against `docs/UI-UX/16`.
- [ ] Password change enforces the org policy and shows requirements up front.
- [ ] MFA and session management are fully self-service here.
- [ ] Social login is shown as unavailable rather than broken.
- [ ] Accessibility requirements are met at mobile width as well as desktop.

---

## P3-13 — Docs: MFA and Session Security Guides

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P3-05, P3-06 |
| **Plan refs** | `docs/PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`, `docs/UI-UX/21-CONTENT-AND-COPY-STRATEGY.md` |
| **Spec required** | No |
| **Surface** | docs |

**Steps**
1. End-user guide: enrolling MFA, using recovery codes, what to do when a device is lost.
2. Integrator guide: reading `amr` to make step-up decisions, with a concrete worked example.
3. Integrator guide: handling refresh token rotation correctly, including the reuse-detection behavior and the retry grace window — a client that retries naively will otherwise trigger family revocation and be blamed on the service.
4. Admin guide: enabling mandatory MFA, including the grace behavior and impact assessment.
5. Regenerate the API reference; publish the changelog entry.
6. Audit for unshipped claims — SAML and social login remain Phase 4.

**Definition of Done**
- [ ] The refresh-rotation guide accurately describes reuse detection and the grace window.
- [ ] The `amr` guide matches the values actually emitted.
- [ ] The lost-device process matches what `P3-04` implemented.
- [ ] No page claims a Phase 4 capability.

---

## P3-14 — Phase 3 Test Suite

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | all Phase 3 implementation tasks |
| **Plan refs** | `docs/PLAN/11-TESTING.md`, `docs/PLAN/10-THREAT-MODEL.md`, `docs/SECURITY/05-VERIFICATION-AND-REDTEAM-PLAN.md` |
| **Spec required** | No |
| **Surface** | backend, console |

**Steps**
1. **Unit**: TOTP generation and verification including skew boundaries; recovery code hashing and consumption; refresh token family logic.
2. **Integration**: full MFA enrollment and login flows for both factor types; rotation and reuse detection against a real store; session revocation propagation.
3. **E2E**: `docs/PLAN/11`'s named case — correct password with wrong TOTP is rejected; session revocation from the console; the full lost-device recovery path.
4. **Security**: every abuse case listed on every Phase 3 task, plus `docs/PLAN/11` § Security Testing's "used (rotated) refresh token cannot be reused."
5. Add a timing test confirming MFA verification does not leak validity through response time.
6. Verify anomaly detection against synthetic impossible-travel and new-device data.

**Definition of Done**
- [ ] Every Phase 3 abuse case has a passing test.
- [ ] The rotated-refresh-token test explicitly satisfies `docs/PLAN/17`'s Phase 3 criterion.
- [ ] Both factor types are covered end-to-end.
- [ ] The suite is green in CI.

---

## P3-15 — Phase 3 Acceptance Validation

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P3-14 |
| **Plan refs** | `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 3, `docs/PLAN/09-SECURITY.md` |
| **Spec required** | No |
| **Surface** | all |

**Steps**
1. Verify each `docs/PLAN/17` Phase 3 criterion with recorded evidence:
   - TOTP enrollment and verification work end-to-end, including documented lost-device recovery.
   - A rotated refresh token cannot be reused, proven by an automated test.
   - A user can view and revoke their own sessions, and a revoked session is immediately unusable.
2. Re-run the load test; MFA adds a step to the login path and its latency cost should be measured, not assumed.
3. Run the Phase 4 threat-model review — SAML and social login both add substantial new attack surface (`docs/SECURITY/02` §7 SSRF, and XML parsing risks).
4. Write the phase summary in `MEMORY/`.
5. Update `PROGRESS.md`; tag; publish the changelog.

**Definition of Done**
- [ ] All three `docs/PLAN/17` Phase 3 criteria verified with evidence.
- [ ] Login-path latency with MFA is measured against `docs/PLAN/12`.
- [ ] The Phase 4 threat-model review is complete.
- [ ] A phase summary exists in `MEMORY/`.

---

## Phase 3 Exit Checklist

From `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 3:

- [ ] TOTP enrollment and verification work end-to-end, including recovery from a lost device via a documented process.
- [ ] A rotated refresh token cannot be reused, verified by an automated test.
- [ ] A user can view and revoke their own active sessions, and a revoked session is immediately unusable.

Plus, from this phase's own scope:

- [ ] WebAuthn/passkey registration and authentication work across at least two platforms.
- [ ] Organization-mandated MFA is enforced, closing the gap `P2-10` deliberately left open.
- [ ] Login anomaly detection notifies on new device and new location with a measured false-positive rate.
- [ ] Personal account settings are fully usable on mobile.
