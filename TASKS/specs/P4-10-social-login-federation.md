# P4-10 — Social Login Federation

## 1. Business Objective

Allow organization members to authenticate with Google, Microsoft, or GitHub while preserving one local `users` record, normal grants, and centrally enforced organization policy.

## 2. User / System Actors

- Existing local user beginning a social sign-in after linking that provider through P4-11.
- Organization policy administrator configuring `allowed_login_methods`.
- Auth Service login, session, audit, secret-resolution, and user stores.
- Google and Microsoft OIDC providers; GitHub OAuth 2.0 profile and email APIs.

## 3. Functional Requirements

1. A sign-in start request creates short-lived, single-use state containing the provider, intent, pending authorization request, PKCE verifier, nonce where applicable, and organization. P4-11's link intent additionally binds to the authenticated user and session.
2. The callback rejects missing, expired, reused, or mismatched state before any account lookup.
3. Google and Microsoft ID tokens are verified for signature, issuer, audience, expiry, and nonce using provider JWKS.
4. GitHub authorization codes are exchanged over TLS; the authenticated profile's stable numeric subject is used. The email API may return verified addresses, but email is never an identity key.
5. An identity can be linked only from an authenticated local session through P4-11. A provider response or matching email alone never links or creates a user.
6. An unlinked `(provider, subject)` is refused for sign-in with a generic message directing the user to link from an existing account.
7. A known `(provider, subject)` resolves to its linked local user, regardless of mutable email changes, and may sign in only to that user's organization.
8. `allowed_login_methods` is enforced before callback completion; unsupported or disallowed providers are refused without account disclosure.
9. Sessions record the provider in `auth_methods` and therefore in the resulting `amr` claim.
10. Provider secrets are resolved through the existing secret-reference mechanism and never serialized or logged.

## 4. Non-Functional Requirements

- Callback state and provider responses are bounded in size and expire within ten minutes.
- Provider JWKS/discovery uses a bounded HTTP client with timeouts and cached keys; an outage fails closed for the social attempt and does not affect password login.
- Provider network calls do not block database transactions or hold tenant locks.

## 5. Dependencies

P1-11 session management, P1-19 user lifecycle, P2-10 organization policy, P3-07 MFA mandate, Redis, secret resolver, and audit writer. P4-11 account linking depends on the identity lookup and validation primitives here.

## 6. Database Changes

Use the planned `user_identities` table with a unique `(provider, provider_subject)` constraint. Add only the migration needed to activate that planned table; do not match on email.

P4-10.1 implements migration `20261125000047`: `provider_subject` uses deterministic `C` collation and a 1–255-byte bound; provider values are limited to the three supported adapters. Tenant ownership follows the referenced `users` row through an RLS policy, so no duplicate organization field can disagree with its user. The runtime role may read, insert, and delete links, but may not update a binding in place. The sign-in repository exposes only `BySubject`; authenticated, audited linking remains P4-11.

## 7. API Contract

Hosted endpoints: `GET /login/social/{provider}` and `GET /login/social/{provider}/callback`, returning the existing hosted-login redirect/error treatment. No bearer Management API endpoint is introduced. The OpenAPI contract must document provider values and generic callback errors without exposing provider tokens or identity details.

## 8. Frontend Changes

The hosted login page exposes provider buttons only when the organization policy allows them. A callback error is generic and retryable. Personal account linked-identity UI is P4-11, not this card.

## 9. Backend Changes

Add a provider-neutral identity interface, OIDC verifier for Google/Microsoft, GitHub OAuth profile/email resolver, state store, identity store, callback handler, and wiring in `cmd/authservice`. Reuse login session issuance and MFA mandate checks rather than creating a second authentication path.

## 10. Authorization Rules

The provider is allowed only when the target organization's `allowed_login_methods` includes it. The callback may resolve only the already-linked user in the target organization. No provider callback may select an organization from an untrusted request parameter, and no social login grants administrative roles. Identity linking requires an authenticated session per P4-11.

## 11. Validation

Provider is a fixed enum. Redirect URIs are configured server-side and exact. State, nonce, PKCE verifier, issuer, audience, expiry, and verified email are required as appropriate to the provider. GitHub subject is the stable account ID, never email.

## 12. Error Handling

Unknown/disallowed provider, invalid state, provider error, invalid token, unlinked identity, and identity collision all return a generic hosted-login error with a retry action. Internal detail is logged only as a redacted event classification; email is not used to disclose whether a local user exists.

## 13. Edge Cases

Provider email changes, duplicate callback races, revoked provider authorization, missing primary email, multiple verified GitHub emails, provider JWKS rotation, Redis outage, an unlinked first-time identity, and an existing live session that still requires MFA.

## 14. Abuse Cases

- Unverified email takeover: never link or provision a user automatically; linking requires an authenticated session.
- Provider ID-token replay: state/nonce/expiry and single-use callback state reject it.
- Email changed to another user's address: identity resolution uses provider subject only.
- Direct callback bypass of `allowed_login_methods`: policy is re-read and enforced in the callback.

## 15. Logging / Audit Requirements

Write success/failure events containing provider, stable internal user id when known, organization id, and redacted reason. Never write provider access tokens, ID tokens, authorization codes, email addresses on unknown-user failures, or raw provider responses.

## 16. Security Controls

TLS-only provider HTTP client, strict redirect allowlist, bounded state TTL, PKCE S256, nonce for OIDC, constant-time state comparison where applicable, no automatic email linking, and per-provider rate limiting.

## 17. Testing Strategy

Unit-test state, provider policy, verified-email selection, and identity matching. Integration-test mock Google, Microsoft, and GitHub providers with real PostgreSQL/Redis. Security tests cover all four abuse cases and concurrent callback redemption. E2E covers the hosted provider selection and the generic error path.

## 18. Acceptance Criteria

All three provider adapters pass their mock end-to-end flows; an unlinked identity cannot sign in or create a user; identity linking requires an authenticated local session; subject matching survives email changes; policy disallowance is enforced at callback; sessions contain the provider method; no secret or token appears in logs/events.

## 19. Definition of Done

Code, migration, OpenAPI/generated clients, tests, task board, MEMORY record, and deployment configuration are updated. Provider credentials are represented only as secret references. CI and the relevant backend integration suite pass.

## 20. Implementation Sequence

1. Add migration and identity store.
2. Add provider configuration and bounded HTTP abstraction.
3. Implement state/PKCE/nonce store.
4. Implement OIDC adapters and GitHub OAuth adapter.
5. Add callback policy, linked-identity-only resolution, session issuance, MFA mandate, and audit.
6. Wire hosted sign-in routes and login UI; P4-11 adds the authenticated link flow.
7. Add tests, regenerate API clients, and run acceptance checks.

## 21. Rollback Strategy

Disable provider methods in organization policy and remove provider configuration. Existing password sessions and manually linked identities remain intact. Roll back code only after disabling callback routes; the identity table is additive and can remain for a forward-compatible rollback.

## 22. Technical Risks

Provider API changes, JWKS rotation, GitHub's OAuth rather than OIDC semantics, provider outages, and the extra step for users to link their provider from an existing account. Each is isolated behind an adapter and covered by mock-provider tests.
