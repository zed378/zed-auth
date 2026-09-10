# Phase 4 — Enterprise Interoperability

**Goal**: make the service usable by organizations that don't look like the first one — legacy applications that only speak SAML, users who want to sign in with an existing identity, external partners who need scoped access to one project, and systems that need to react to events.

**Why now**: Project Grants (the cross-organization delegation that `docs/PLAN/08` Part C describes as the reason this project needs more than plain RBAC) depend on Phase 2's role model being correct. SAML and social login are additive protocol surfaces that would have complicated Phase 1's core flow if built earlier.

**Prerequisite**: Phase 3 exit checklist satisfied, plus the Phase 4 threat-model review from `P3-15`. If Phase 3 and Phase 4 were swapped per `docs/PLAN/16`'s note, substitute the Phase 2 exit and run the Phase 4 threat model at that point instead.

**Roadmap reference**: `docs/PLAN/16-IMPLEMENTATION-ROADMAP.md` § Phase 4. **Delegation source of truth**: `docs/PLAN/08-AUTHORIZATION.md` Part C.

---

## Task Summary

| ID | Task | Surface | Size | Depends on |
|---|---|---|---|---|
| P4-01 | Project Grants — data and lifecycle | backend | L | P2-03, P2-05 |
| P4-02 | Delegated user grants with subset validation | backend | L | P4-01 |
| P4-03 | `PROJECT_GRANT_OWNER` role enforcement | backend | M | P4-01, P2-05 |
| P4-04 | Delegated role claims and revocation propagation | backend | L | P4-02, P2-04 |
| P4-05 | Console — Project Grants tab | console | L | P4-01 |
| P4-06 | Console — Granted Projects list | console | L | P4-02 |
| P4-07 | SAML 2.0 Identity Provider — core | backend | L | P1-11 |
| P4-08 | SAML SP-initiated and IdP-initiated flows | backend | L | P4-07 |
| P4-09 | SAML application type and metadata management | backend, console | M | P4-07, P1-18 |
| P4-10 | Social login federation | backend | L | P1-11, P1-19 |
| P4-11 | Account linking and identity reconciliation | backend | L | P4-10 |
| P4-12 | Webhooks for important events | backend | L | P0-12 |
| P4-13 | SCIM provisioning (optional) | backend | L | P1-19 |
| P4-14 | Docs — delegation, SAML, and social login guides | docs | M | P4-06, P4-09, P4-10 |
| P4-15 | Phase 4 test suite | backend, console | L | all above |
| P4-16 | Phase 4 acceptance validation | all | M | P4-15 |

---

## P4-01 — Project Grants: Data and Lifecycle

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P2-03, P2-05 |
| **Plan refs** | `docs/PLAN/08-AUTHORIZATION.md` Part C, `docs/PLAN/04-DATA-MODEL.md` § `project_grants`, `docs/PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md` § Worked Example |
| **Spec required** | Yes — and the worked example in `docs/PLAN/19` is literally this feature |
| **Surface** | backend |

**Goal** — The delegation mechanism from `docs/PLAN/08` Part C: an owning organization lends a project to another organization with a restricted subset of roles.

**Note** — `docs/PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md` uses Project Grant creation as its worked example. Read that worked example before writing the spec; much of the analysis is already done.

**Steps**
1. Implement `project_grants` per `docs/PLAN/04`: `project_id`, `granting_org_id`, `granted_org_id`, `granted_role_keys[]`, `status` in {`active`, `revoked`}.
2. Implement the API from `docs/PLAN/08` Part C:
   - `POST /v1/organizations/{granting_org_id}/projects/{project_id}/grants`
   - `GET /v1/organizations/{granting_org_id}/projects/{project_id}/grants`
   - `DELETE /v1/organizations/{granting_org_id}/projects/{project_id}/grants/{grant_id}`
3. Validate at creation that every key in `granted_role_keys` actually exists in that project.
4. Restrict creation to `PROJECT_OWNER` or above within the **granting** organization.
5. Prevent a self-grant (granting to the owning organization), which would create a confusing second path to the same access.
6. Handle revocation as the high-consequence operation it is: revoking a grant must immediately remove access for every user who held roles through it (`docs/PLAN/17` Phase 4 criterion). Design the propagation explicitly — this is `P4-04`'s work, but the lifecycle design must account for it.
7. Prefer status transition over hard deletion, so the audit trail of a past delegation survives.
8. Require typed confirmation for revocation in the console (`docs/UI-UX/08`, `docs/UI-UX/07`) and audit both creation and revocation with elevated visibility (`docs/PLAN/08` § Least Privilege).
9. Handle the edge case where a role is deleted from the project after being granted: either block the deletion or cascade it out of `granted_role_keys`, but never leave a grant referencing a role that no longer exists.

**Definition of Done**
- [ ] A grant can only reference roles that exist in the project.
- [ ] Only `PROJECT_OWNER` or above in the granting organization can create or revoke.
- [ ] Self-grants are rejected.
- [ ] Revocation is a status transition and preserves history.
- [ ] Deleting a granted role is handled deliberately, never leaving a dangling reference.
- [ ] Creation and revocation are audited with full detail.

**Abuse cases to test**
- Creating a grant on a project the caller does not own (`docs/SECURITY/02` §3).
- Granting a role that does not exist, or one from a different project.
- Modifying `granted_role_keys` after creation to widen the delegation.
- Revocation not propagating to already-issued access.

---

## P4-02 — Delegated User Grants with Subset Validation

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P4-01 |
| **Plan refs** | `docs/PLAN/08-AUTHORIZATION.md` Part C, `CLAUDE.md` § Non-Negotiable Constraints, `AGENTS.md` hard rule 3, `docs/PLAN/09-SECURITY.md` § Delegation abuse |
| **Spec required** | Yes — the single most security-critical check in the system |
| **Surface** | backend |

**Goal** — The receiving organization assigns roles to its own users, restricted to the delegated subset. `CLAUDE.md`, `AGENTS.md`, `docs/PLAN/08`, and `docs/PLAN/09` all state the same rule independently, which is a strong signal about how it should be treated.

> **The rule, stated exactly as the plan states it**: Project Grant role assignment must be validated server-side as a subset of `granted_role_keys` **on every single request**, not just at grant-creation time.

**Steps**
1. Implement `POST /v1/organizations/{granted_org_id}/project-grants/{grant_id}/user-grants` per `docs/PLAN/08` Part C.
2. Validate on **every** request that the requested `role_keys` are a subset of the grant's current `granted_role_keys`. Not at creation only. Not from a cached copy that could be stale relative to a narrowed grant.
3. Verify the grant is `active` on every request — a revoked grant must reject immediately.
4. Verify the target user belongs to the receiving organization. Assigning a delegated role to a user outside it would be a cross-tenant breach.
5. Populate `user_grants.project_grant_id` (`docs/PLAN/04`), which is what distinguishes a delegated grant from a direct one everywhere downstream.
6. Invert the guard test from `P2-03`, which currently asserts that a non-null `project_grant_id` is rejected — this is the designed slot being filled.
7. Return an unambiguous, actionable error when a non-granted role is requested: `docs/PLAN/17`'s Phase 4 criterion requires the rejection to come with a clear error.
8. Audit every delegated assignment with both organizations, the grant, the user, and the exact roles.

**Definition of Done**
- [ ] Subset validation runs on every request, verified by a test that narrows `granted_role_keys` after a grant exists and confirms the previously-valid assignment is now rejected.
- [ ] A revoked grant rejects immediately.
- [ ] A user outside the receiving organization cannot receive a delegated role.
- [ ] `project_grant_id` is populated and distinguishes delegated from direct grants.
- [ ] The rejection error is clear and actionable.
- [ ] Every delegated assignment is audited.

**Abuse cases to test**
- The receiving organization assigning a role outside `granted_role_keys` — `docs/PLAN/11` § Security Testing names this explicitly.
- Assignment through a revoked grant.
- Assignment to a user in a third organization.
- Race between narrowing a grant and assigning a role under the old set.
- Privilege escalation by editing `project_grant_id` on a direct grant to borrow a delegation's scope.

---

## P4-03 — `PROJECT_GRANT_OWNER` Role Enforcement

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P4-01, P2-05 |
| **Plan refs** | `docs/PLAN/08-AUTHORIZATION.md` Part C § Manager Role Hierarchy, `docs/PLAN/04-DATA-MODEL.md` § `manager_roles` |
| **Spec required** | Yes — administrative authorization |
| **Surface** | backend |

**Goal** — Activate the fifth manager role, reserved since `P2-05`. `docs/PLAN/08` is precise: it "only applies to roles actually delegated via `project_grants`."

**Steps**
1. Scope `PROJECT_GRANT_OWNER` by `scope_id` pointing at a specific `project_grant`, not at a project.
2. Grant exactly one capability: assigning and revoking the delegated roles, for users within the receiving organization.
3. Explicitly deny everything else: this role cannot modify the project, its roles, its applications, or the grant itself. It is the narrowest role in the hierarchy by design.
4. Extend `P2-05`'s combinatorial hierarchy test with this role, since the whole point of an exhaustive test is that it stays exhaustive.
5. Decide who assigns it in the receiving organization — normally `ORG_OWNER` or `ORG_ADMIN` there — and audit the assignment.

**Definition of Done**
- [ ] The role is scoped to a specific grant, not a project.
- [ ] It cannot reach any capability beyond assigning delegated roles.
- [ ] The hierarchy test covers it exhaustively.
- [ ] Assignment is audited.

**Abuse cases to test**
- A `PROJECT_GRANT_OWNER` acting on the granting organization's project (`docs/SECURITY/02` §3).
- Using the role to widen its own grant.
- Using it to act on a different grant in the same organization.

---

## P4-04 — Delegated Role Claims and Revocation Propagation

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P4-02, P2-04 |
| **Plan refs** | `docs/PLAN/08-AUTHORIZATION.md` Part C § Token Claim Format, § Full Permission Check Flow, `docs/PLAN/12-PERFORMANCE.md` |
| **Spec required** | Yes — token and decision path |
| **Surface** | backend |

**Goal** — Delegated roles appear in tokens with the correct organizational context, and a revoked grant stops working before the token expires.

**Steps**
1. Emit delegated roles in the claim format from `P2-04`, where the nested `org_id` now carries its designed meaning — disambiguating a role reachable from two organizational contexts, which is exactly why `docs/PLAN/08` required it from the start.
2. Implement `docs/PLAN/08` Part C's full permission check flow, in order: verify signature and expiry; read the role claim; in a client-organization context, verify the claim's `org_id` matches the request's tenant context and, if the role came via a project grant, verify the grant is still active; then match against the required permission.
3. Use short-TTL caching for grant validity, not a database hit per request (`docs/PLAN/08`, `docs/PLAN/12`). Reuse `P2-07`'s cache with proactive invalidation on revocation.
4. Make revocation propagate within the documented window and publish that number — an integrator who believes revocation is instant when it is not will build an incorrect security model.
5. Extend `/v1/authz/check` to evaluate delegated grants, so `docs/PLAN/08`'s guidance to prefer the real-time endpoint for sensitive actions is actually actionable.
6. Instrument the Project Grant creation and revocation rate (`docs/PLAN/13` names unusual spikes as a possible misuse indicator).

**Definition of Done**
- [ ] Delegated roles appear with the correct `org_id` in the claim.
- [ ] The permission check follows `docs/PLAN/08` Part C's documented sequence, verified step by step.
- [ ] Revoking a grant removes access within the documented window, and immediately via `/v1/authz/check`.
- [ ] The revocation window is documented publicly.
- [ ] Grant rate metrics are emitted per `docs/PLAN/13`.
- [ ] `docs/PLAN/12`'s authz latency targets still hold with delegation in the path.

**Abuse cases to test**
- A role claim from org A honored while acting in org B's context — `docs/PLAN/08` Part C step 3a exists precisely for this.
- Access persisting after revocation beyond the documented window.
- A forged `org_id` inside a role claim (defeated by signature, but test it).

---

## P4-05 — Console: Project Grants Tab

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P4-01 |
| **Plan refs** | `docs/UI-UX/18-DETAILED-PAGE-SPECIFICATIONS.md` § Project Grants Tab, `docs/UI-UX/04-USER-FLOWS.md` Flow 2, `docs/UI-UX/08-PAGE-SPECIFICATIONS.md`, `docs/UI-UX/07-COMPONENT-SPECIFICATION.md` |
| **Spec required** | No — but `docs/UI-UX/18` has a detailed spec for this screen |
| **Surface** | console |

**Goal** — The granting side of delegation, implementing `docs/UI-UX/04` Flow 2 exactly, per `docs/UI-UX/08`'s note.

**Steps**
1. Run the full `docs/UI-UX/19` chain; `docs/UI-UX/18` already specifies this page in detail — follow it rather than re-deriving.
2. Table of grants: receiving organization, granted roles, status, created date.
3. Creation modal: select the receiving organization, then select a subset of the project's roles. The interface must make the subset relationship visually obvious — an admin should see what they are *not* granting as clearly as what they are.
4. Use `color-warning` for the state `docs/UI-UX/05` names by example: "this Project Grant has no roles selected yet."
5. Revocation uses the **typed-confirmation** dialog variant (`docs/UI-UX/08` specifies this exact variant), with copy stating plainly that every user holding roles through this grant loses access.
6. Show the count of users currently holding roles through the grant before revoking — the blast radius should be visible at the moment of decision.
7. Use `color-danger` only for the revoke action.

**Definition of Done**
- [ ] Flow 2 from `docs/UI-UX/04` is implemented exactly and covered by an E2E test.
- [ ] The implementation matches `docs/UI-UX/18`'s detailed spec for this page.
- [ ] Revocation requires typed confirmation and shows the affected user count.
- [ ] The no-roles-selected state uses `color-warning`, not `color-danger`.
- [ ] Accessibility and responsive requirements are met.

---

## P4-06 — Console: Granted Projects List

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P4-02 |
| **Plan refs** | `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` (Granted Projects list), `docs/UI-UX/04-USER-FLOWS.md` Flow 3, `docs/UI-UX/01-USER-PERSONAS.md` (Vendor Admin) |
| **Spec required** | No — implementation chain mandatory |
| **Surface** | console |

**Goal** — The receiving side: a vendor admin assigns delegated roles to their own staff, implementing Flow 3 — with the hard constraint from `docs/UI-UX/08` that this screen **never shows non-granted roles**.

**Steps**
1. Run the full `docs/UI-UX/19` chain.
2. List projects delegated *to* this organization, with the granting organization named and the available roles shown.
3. The role-select form is restricted at the UI level to `granted_role_keys` — and the server validates independently on every request (`P4-02`). The UI restriction is convenience; the server check is the control.
4. Show the role-source badge marking these as delegated (`docs/UI-UX/08` § Cross-Screen Requirements makes the badge mandatory wherever roles appear). `P2-12` built the badge; this is where its second value finally renders.
5. Handle revocation from the receiving side's perspective: when the granting organization revokes, this list must reflect it clearly rather than failing opaquely on the next action.
6. Empty state explains what a granted project is, since a vendor admin may encounter the concept here for the first time.

**Definition of Done**
- [ ] A non-granted role is never rendered, verified by test.
- [ ] The role-source badge shows "delegated" correctly.
- [ ] Flow 3 is covered by an E2E test.
- [ ] A revoked grant is reflected clearly rather than as an opaque error.
- [ ] The empty state is explanatory rather than blank.

---

## P4-07 — SAML 2.0 Identity Provider: Core

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-11 |
| **Plan refs** | `docs/PLAN/03-ARCHITECTURE.md` § SAML Identity Provider, `docs/PLAN/05-API-CONTRACT.md` § Standards Used, `docs/PLAN/11-TESTING.md` § Security Testing (fuzzing SAML assertions) |
| **Spec required** | Yes — new protocol surface |
| **Surface** | backend |

**Goal** — Act as a SAML 2.0 IdP for enterprise and legacy applications that cannot speak OIDC.

**Steps**
1. Use a well-maintained SAML library. SAML's security history is dominated by XML parsing and signature-wrapping bugs; hand-rolling this is a documented path to compromise.
2. Configure the XML parser defensively: disable external entity resolution (XXE), disable DTD processing, and bound document size and entity expansion (`docs/SECURITY/02` §7 SSRF, §9 File Upload Abuse patterns apply to XML ingestion too).
3. Sign assertions with dedicated SAML signing keys, managed through `P1-03`'s key infrastructure but kept distinct from the OIDC signing keys.
4. Validate signatures on incoming requests, guarding specifically against signature wrapping — verify that the signed element is the one actually being used, not merely that a valid signature exists somewhere in the document.
5. Reuse the same session model as OIDC (`P1-11`), so SSO works across protocol boundaries — a user logged in via OIDC should not be re-prompted by a SAML app.
6. Map user attributes into assertions, honoring the same scope discipline as `/oauth/userinfo`: release only what the service provider is configured to receive.
7. Enforce assertion lifetime and audience restriction strictly.
8. Implement replay protection with assertion ID tracking.

**Definition of Done**
- [ ] XXE and DTD processing are disabled, verified by a test feeding a malicious document.
- [ ] Signature wrapping is defeated, verified by a test with a wrapped assertion.
- [ ] SAML signing keys are distinct from OIDC keys.
- [ ] A session established via OIDC satisfies a SAML request without re-authentication.
- [ ] Assertion replay is rejected.
- [ ] Fuzz tests run against the assertion parser (`docs/PLAN/11`).

**Abuse cases to test**
- XXE via a crafted assertion.
- Signature wrapping.
- Assertion replay.
- Audience confusion: an assertion for SP A accepted by SP B.
- Billion-laughs entity expansion.

---

## P4-08 — SAML SP-Initiated and IdP-Initiated Flows

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P4-07 |
| **Plan refs** | `docs/PLAN/03-ARCHITECTURE.md` § SAML Identity Provider, `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 4 |
| **Spec required** | Yes |
| **Surface** | backend |

**Goal** — Both flows `docs/PLAN/03` names, with a SAML-only legacy application completing a full SP-initiated login — `docs/PLAN/17`'s Phase 4 criterion.

**Steps**
1. SP-initiated: accept an `AuthnRequest`, authenticate or reuse the session, and POST the assertion back to the registered Assertion Consumer Service URL.
2. IdP-initiated: allow initiating from the IdP side, while noting it carries inherent CSRF-like risk since there is no request to correlate against. Document the risk and consider making it opt-in per application.
3. Validate `RelayState` and bound its size; it is attacker-influenced input that gets echoed.
4. Validate the ACS URL against the application's registered value with the same exact-match discipline as OIDC redirect URIs — this is the same open-redirect class of bug in a different protocol.
5. Support both HTTP-Redirect and HTTP-POST bindings.
6. Implement SAML Single Logout, or document explicitly that it is unsupported. An undocumented gap in logout is a security surprise.
7. Audit SAML authentications alongside OIDC ones, in the same `events` taxonomy.

**Definition of Done**
- [ ] A SAML-only application completes a full SP-initiated login — `docs/PLAN/17` Phase 4 criterion.
- [ ] IdP-initiated flow works with its risk documented and opt-in per application.
- [ ] ACS URL matching is exact.
- [ ] `RelayState` is validated and size-bounded.
- [ ] Single Logout is implemented or its absence is documented.
- [ ] SAML logins appear in the audit log identically to OIDC logins.

---

## P4-09 — SAML Application Type and Metadata Management

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P4-07, P1-18 |
| **Plan refs** | `docs/PLAN/04-DATA-MODEL.md` § `applications` (`type: saml`), `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` (Applications tab) |
| **Spec required** | No |
| **Surface** | backend, console |

**Steps**
1. Activate the `saml` application type already present in `docs/PLAN/04`'s enum.
2. Support both metadata XML upload and manual configuration of entity ID, ACS URL, and certificate.
3. Validate uploaded metadata defensively — it is untrusted XML from an external party, subject to every risk in `P4-07`.
4. Publish the IdP metadata endpoint so service providers can configure themselves.
5. Add the SAML application type to the console's Applications tab (`docs/UI-UX/08` § Phase 4 note), with SAML-specific fields replacing the OIDC ones.
6. Handle certificate expiry: warn before an SP certificate expires rather than failing silently at authentication time.

**Definition of Done**
- [ ] SAML applications are creatable via both API and console (FR-14).
- [ ] Uploaded metadata is parsed with the same hardened parser configuration as assertions.
- [ ] IdP metadata is published and consumable by a standard SP.
- [ ] Certificate expiry produces a warning before it produces an outage.

---

## P4-10 — Social Login Federation

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-11, P1-19 |
| **Plan refs** | `docs/PLAN/02-REQUIREMENTS.md` FR-3, `docs/PLAN/05-API-CONTRACT.md` § Social login, `docs/PLAN/08-AUTHORIZATION.md` Part B (`allowed_login_methods`) |
| **Spec required** | Yes — authentication |
| **Surface** | backend |

**Goal** — Act as an OIDC Relying Party toward Google, Microsoft, and GitHub, while keeping roles centrally managed — `docs/PLAN/05` is explicit that a federated user still gets a `users` record.

**Steps**
1. Implement the RP side of Authorization Code + PKCE against each provider.
2. Store provider client secrets in the secret manager (`P0-14`), never in configuration files.
3. Validate the provider's ID token fully: signature against their JWKS, issuer, audience, expiry, and nonce.
4. Handle unverified email correctly. Trusting an unverified email from a provider allows account takeover by registering that address at the provider — this is a well-known attack, and the mitigation is to require `email_verified` or to fall back to explicit linking.
5. Create a local `users` record for every federated user and link it through `user_identities` (`docs/PLAN/04`), so grants and roles work identically regardless of login method (`docs/PLAN/05`).
6. Enforce `allowed_login_methods` from organization policy (`docs/PLAN/08` Part B) — a provider not on the list is refused even though it is implemented.
7. Record the provider in `auth_methods` and `amr`.
8. Handle provider outages gracefully: a user with only a social login and no password needs a documented path when the provider is down.

**Definition of Done**
- [ ] All three providers work end-to-end.
- [ ] An unverified email from a provider does not automatically link to an existing account.
- [ ] Every federated user has a local `users` record with normal grant handling.
- [ ] Organization policy can disallow a provider.
- [ ] Provider secrets live only in the secret manager.
- [ ] `amr` records the provider used.

**Abuse cases to test**
- Account takeover via an unverified email claim from a provider.
- Provider ID token replay.
- A provider account whose email later changes to one matching another user.
- Bypassing `allowed_login_methods` by calling the callback directly.

---

## P4-11 — Account Linking and Identity Reconciliation

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P4-10 |
| **Plan refs** | `docs/PLAN/05-API-CONTRACT.md` § Social login, `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` (Personal account settings — linked social logins) |
| **Spec required** | Yes — identity binding is an attack surface |
| **Surface** | backend |

**Goal** — Let one person use several login methods for one account, without letting anyone attach their own provider identity to someone else's account.

**Steps**
1. Require an **authenticated session** to link a new provider. Linking by matching email alone at login time is the account-takeover path from `P4-10`.
2. Support unlinking, with a guard preventing removal of the last remaining login method.
3. Handle the collision case explicitly: a social login whose email matches an existing local account must prompt for authentication rather than linking silently.
4. Show linked identities in personal account settings (`docs/UI-UX/08`), replacing `P3-12`'s placeholder.
5. Audit every link and unlink — these are account-takeover-adjacent events.
6. Handle provider identifier changes: match on the provider's stable subject identifier, never on email, which is mutable at most providers.

**Definition of Done**
- [ ] Linking requires an authenticated session.
- [ ] The last login method cannot be removed.
- [ ] An email collision prompts for authentication instead of linking automatically.
- [ ] Matching uses the provider's stable subject, not email.
- [ ] Link and unlink events are audited.
- [ ] Linked identities render in personal account settings.

---

## P4-12 — Webhooks for Important Events

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P0-12 |
| **Plan refs** | `docs/PLAN/05-API-CONTRACT.md` § Rate Limiting, Idempotency, Webhooks, `docs/PLAN/16-IMPLEMENTATION-ROADMAP.md` § Phase 4 |
| **Spec required** | Yes — outbound requests are an SSRF surface |
| **Surface** | backend |

**Goal** — Deliver the events `docs/PLAN/05` names — `user.created`, `user.deleted`, `role.assigned`, `role.revoked`, `login.success`, `login.failed` — to consumer systems, reliably and without turning the service into an SSRF gadget.

**Steps**
1. Endpoint registration per organization via `webhook_endpoints`, with delivery attempts recorded in `webhook_deliveries` (`docs/PLAN/04`).
2. **SSRF defense is the central concern** (`docs/SECURITY/02` §7): validate destination URLs against an allow-list policy, block private and link-local address ranges, and re-resolve DNS at request time to defeat rebinding. An outbound HTTP client that fetches attacker-supplied URLs from inside the network perimeter is exactly the primitive an attacker wants.
3. Sign every payload with a per-endpoint secret so receivers can verify authenticity, and include a timestamp to let them reject replays.
4. Retry with exponential backoff and a bounded attempt count; disable an endpoint after sustained failure and notify the organization.
5. Ensure delivery cannot block the originating request — queue asynchronously.
6. Never include tokens, passwords, or raw authz attributes in payloads (`docs/PLAN/13`).
7. Provide a delivery log so an admin can debug their own integration without opening a support ticket.
8. Rate-limit per endpoint to prevent a webhook storm from amplifying an incident.

**Definition of Done**
- [ ] Private, loopback, and link-local destinations are rejected, verified by test including a DNS-rebinding attempt.
- [ ] Payloads are signed and timestamped.
- [ ] Retries are bounded, and a failing endpoint is disabled with notification.
- [ ] Delivery never blocks the originating request.
- [ ] No sensitive value appears in any payload.
- [ ] The delivery log is visible to the organization's admins.

**Abuse cases to test**
- SSRF to internal services via a registered webhook URL (`docs/SECURITY/02` §7).
- DNS rebinding between validation and request.
- Webhook payload used to exfiltrate data across tenants.
- Amplification: registering a webhook that generates further events.

---

## P4-13 — SCIM Provisioning (Optional)

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-19 |
| **Plan refs** | `docs/PLAN/05-API-CONTRACT.md` § Standards Used (SCIM: Phase 4, optional), `docs/PLAN/01-PRODUCT-SCOPE.md` § Out of Scope ("Deferred to Phase 4 (optional)") |
| **Spec required** | Yes |
| **Surface** | backend |

**Goal** — Automated user provisioning from an external IdP — **only if a real integration needs it**. `docs/PLAN/01` is explicit: build it once an external IdP integration actually requires it, not speculatively.

**Gate** — Do not start this task without a named integration requiring it. Record the decision either way in `MEMORY/DECISIONS.md`; if deferred, move it to `BACKLOG.md` rather than leaving it as a permanently-open task.

**Steps**
1. Implement SCIM 2.0 `/Users` and `/Groups` endpoints.
2. Support create, read, update, patch, delete, and filtered list.
3. Authenticate via a dedicated bearer token per provisioning integration, scoped to provisioning only — not a general-purpose admin token.
4. Map SCIM attributes onto the `users` model, deciding explicitly what happens to attributes with no local equivalent.
5. Handle deprovisioning as deactivation rather than deletion, preserving audit history (`P1-19.3`'s reasoning).
6. Rate-limit and audit; a bulk sync can generate very high volume, and an unbounded one can act as a denial of service against the database.

**Definition of Done**
- [ ] A named integration justified building this, recorded in MEMORY.
- [ ] The endpoints pass a standard SCIM conformance check.
- [ ] The provisioning token cannot perform non-provisioning operations.
- [ ] Deprovisioning deactivates rather than deletes.
- [ ] Bulk operations are rate-limited and audited.

---

## P4-14 — Docs: Delegation, SAML, and Social Login Guides

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P4-06, P4-09, P4-10 |
| **Plan refs** | `docs/PLAN/20-PUBLIC-SITE-ARCHITECTURE.md` § Site Structure (`/docs/guides` — "Create a Project Grant" is a named example), `docs/UI-UX/21-CONTENT-AND-COPY-STRATEGY.md` |
| **Spec required** | No |
| **Surface** | docs |

**Steps**
1. Concepts page for delegation, using `docs/PLAN/08` Part C's concrete Procurement Portal scenario, which is already written at the right level of explanation.
2. Guide: "Create a Project Grant" — named explicitly in `docs/PLAN/20`'s site structure.
3. Guide: "Assign delegated roles as a receiving organization."
4. Guide: "Set up SAML for a legacy application," including IdP metadata and certificate rotation.
5. Guide: "Enable social login," including the verified-email requirement and why it exists.
6. Guide: "Consume webhooks," including signature verification and retry semantics.
7. Regenerate the API reference; publish the changelog.
8. Audit for unshipped claims — ABAC remains Phase 4b and may never ship at all.

**Definition of Done**
- [ ] All six guides exist and have been followed end-to-end by someone who did not write them.
- [ ] The API reference covers every new endpoint.
- [ ] No page claims ABAC availability.

---

## P4-15 — Phase 4 Test Suite

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | all Phase 4 implementation tasks |
| **Plan refs** | `docs/PLAN/11-TESTING.md`, `docs/SECURITY/02` §3, §7, `docs/SECURITY/05-VERIFICATION-AND-REDTEAM-PLAN.md` |
| **Spec required** | No |
| **Surface** | backend, console |

**Steps**
1. **Unit**: subset validation logic in isolation, exhaustively — this is the highest-value unit test in the entire project.
2. **Integration**: `docs/PLAN/11`'s named delegation flow — create a Project Grant, have the receiving org assign an allowed role, verify a disallowed role is rejected; SAML SP-initiated login; social login callback handling.
3. **E2E**: Flow 2 and Flow 3 from `docs/UI-UX/04` through the console; SAML login through a real SP fixture.
4. **Security**: every abuse case on every Phase 4 task, plus `docs/PLAN/11`'s explicit cases — a receiving org cannot assign a role outside `granted_role_keys`, and a revoked Project Grant immediately invalidates access.
5. Fuzz SAML assertion parsing (`docs/PLAN/11`).
6. Dedicated SSRF test suite against the webhook subsystem.

**Definition of Done**
- [ ] Every Phase 4 abuse case has a passing test.
- [ ] The subset-validation test suite covers narrowing, revocation, and race conditions.
- [ ] SAML fuzzing runs in CI.
- [ ] Webhook SSRF defenses are verified against a realistic attempt set.
- [ ] The suite is green in CI.

---

## P4-16 — Phase 4 Acceptance Validation

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P4-15 |
| **Plan refs** | `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 4, `docs/PLAN/09-SECURITY.md` |
| **Spec required** | No |
| **Surface** | all |

**Steps**
1. Verify each `docs/PLAN/17` Phase 4 criterion with recorded evidence:
   - A SAML-only legacy application completes a full SP-initiated login.
   - A Project Grant restricts the receiving organization to exactly the granted roles, and a non-granted role is rejected with a clear error.
   - Revoking a Project Grant immediately removes access for all users who held roles through it.
2. Decide whether Phase 4b (ABAC) is warranted at all. `docs/PLAN/08` Part D and `docs/PLAN/16` both say to build it only when a concrete need appears that RBAC plus Project Grants genuinely cannot express. Record the decision — including "not needed" — in `MEMORY/DECISIONS.md`.
3. Re-run the load test with delegation in the authorization path.
4. Run the threat-model review for whichever phase comes next.
5. Write the phase summary in `MEMORY/`; update `PROGRESS.md`; tag; publish the changelog.

**Definition of Done**
- [ ] All three `docs/PLAN/17` Phase 4 criteria verified with evidence.
- [ ] The Phase 4b go/no-go decision is recorded with its justification.
- [ ] Load test results are recorded.
- [ ] A phase summary exists in `MEMORY/`.

---

## Phase 4 Exit Checklist

From `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 4:

- [ ] A SAML-only legacy application can complete a full SP-initiated login.
- [ ] A Project Grant restricts the receiving organization to exactly the granted roles — an attempt to assign a non-granted role is rejected with a clear error.
- [ ] Revoking a Project Grant immediately removes access for all users who held roles through it.

Plus, from this phase's own scope:

- [ ] Social login works for all three providers with verified-email handling.
- [ ] Webhooks deliver reliably with SSRF defenses verified.
- [ ] SCIM is either delivered against a named integration or explicitly deferred in `BACKLOG.md`.
- [ ] The console exposes the Project Grants tab, the Granted Projects list, and the SAML application type.
