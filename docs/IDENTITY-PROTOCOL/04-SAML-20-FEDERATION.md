# 04 - SAML 2.0 Federation

> Category: **Identity Protocol** (`docs/IDENTITY-PROTOCOL/`) &nbsp;|&nbsp; Status: Draft specification &nbsp;|&nbsp; Owner task: P4-07, P4-08, P4-09

## Purpose

Records what SAML 2.0 federation is expected to do once built — this service acting as an Identity Provider (IdP) issuing assertions to a customer's Service Providers (SPs) — and the constraints already decided against the cards that will build it.

## Why It Is Not Built Yet

No SAML code exists anywhere in this repository: there is no `internal/saml` package, no SAML-related route, and no SAML entry in the OIDC discovery document. `docs/PLAN/16-IMPLEMENTATION-ROADMAP.md`'s phased approach places SAML in Phase 4 (Enterprise Interop), behind Phases 0–3, which are complete. The `signing_keys` table already has a `purpose` column with a `saml` value reserved (`backend/migrations/20260908000005_sessions_and_tokens.up.sql`) specifically so that a future SAML assertion-signing key set is a data-model non-event — but nothing writes or reads that value today.

`TASKS/PHASE-4-ENTERPRISE-INTEROP.md` cards `P4-07` (SP integration), `P4-08` (IdP-initiated / browser bindings), and `P4-09` (metadata and certificates) own this work, and none has started.

## Constraints Already Decided

A dedicated pre-implementation threat-model review (`MEMORY/records/2026-09-15-P3-15-phase-4-threat-review.md`, findings **T4-6** and **T4-7**) examined the Phase 4 cards as written and found their abuse-case tables modeled the *consuming* side of SAML (a Service Provider defending against a malicious IdP), when this service is the **Identity Provider** and never consumes an assertion. The following constraints come directly from that review and must be honored regardless of which engineer implements the cards:

### T4-6 — IdP-side threats, not SP-side ones

- **The ACS URL and Audience are looked up from the registered application by the AuthnRequest's `Issuer`, never taken from the request itself.**
- **The Assertion must be signed, not only the Response** — some SPs verify only one of the two, and an unsigned assertion inside a signed response is the mechanism signature-wrapping attacks on SPs exploit.
- **`InResponseTo` must name a stored, single-use AuthnRequest id**, so one AuthnRequest cannot be answered twice.
- **The HTTP-Redirect binding is DEFLATE-compressed**; a size bound on the compressed query string does not bound the inflated document. The inflated byte count must be bounded explicitly (decompression-bomb advisories exist against the main Go SAML library).
- **`NameID` must never default to email.** Use a persistent, per-SP identifier. An SP keyed on email hands a deactivated user's account to whoever is later issued that address, and an email shared across SPs lets them correlate users.
- **`AuthnContextClassRef` must come from the session's recorded `auth_methods`** (the same source as the OIDC `amr` claim — `docs/SESSION-MANAGEMENT/00-SESSION-ARCHITECTURE.md`), never asserted independently. A `RequestedAuthnContext` the session cannot satisfy must trigger step-up or a `NoAuthnContext` status, never be silently ignored.
- **XML parsing needs explicit care in Go.** `encoding/xml` (which the Go SAML and XML-DSig libraries build on) returns a DTD as an uninterpreted directive and resolves no external entity — a test that feeds it an XXE or billion-laughs document will pass regardless of configuration, which is vacuous verification. The known Go-specific failure is round-trip instability, where the parse that verifies a signature and the parse that reads the document disagree about namespaces; `mattermost/xml-roundtrip-validator` exists specifically for this and must be run before signature verification. The library choice must be recorded as an ADR naming its advisory history at the pinned version.

### T4-7 — Cookies, script, and `form-action` collide with existing controls

Three deliberate, already-shipped controls collide with SAML's browser bindings, each with a tempting fix that would weaken the control:

1. **`SameSite=Lax`** on the session cookie (`docs/SESSION-MANAGEMENT/00-SESSION-ARCHITECTURE.md`) means an HTTP-POST-bound AuthnRequest, arriving as a cross-site POST, will not carry the cookie — the user is prompted every time. Switching the cookie to `SameSite=None` would remove the protection `internal/login/csrf.go`'s double-submit design relies on. The accepted approach: accept the POST-binding request, store it server-side under a handle, and `303` to a `GET` on the issuer's own origin — Lax cookies are sent on top-level GET navigations, so SSO still works with the cookie unchanged.
2. **No script on hosted pages** (`TASKS/BACKLOG.md` `PG-40`) collides with the auto-submitting form typically used to POST a response to an ACS. The response page must be a no-script form with a visible Continue button, or a hash-pinned inline script under `PG-40`'s existing rules.
3. **`Content-Security-Policy: form-action`** — a prior finding (`P1-27`) showed Chrome applies `form-action` along the redirect chain, so a response page with `form-action 'self'` cannot POST to the SP. `form-action` must name the **registered** ACS origin, resolved the same way the existing `formAction()` helper resolves origins today, never an origin taken from the request.

IdP-initiated SSO must be **opt-in per application** and must start only from a POST with a CSRF token on this service's own launcher page — a bare GET URL naming an SP would let any third-party page silently sign a user into that SP with an attacker-chosen `RelayState`. `SessionNotOnOrAfter` must be no later than the organization's `session_lifetime_hours`. Revoking a session (`docs/SESSION-MANAGEMENT/00-SESSION-ARCHITECTURE.md`) does **not** end an SP's own session unless Single Logout is separately implemented — this limitation must be stated plainly wherever SAML is documented once built.

### From T4-8 (metadata) — noted for completeness, owned by `P4-09`

Metadata import is scoped to **upload and manual entry only** — URL-based metadata fetching is explicitly out of scope for Phase 4, because it would be an unbuilt SSRF surface with a planned test that could only pass vacuously. If URL import is ever added later, it must go through the same egress-filtering client planned for webhooks (T4-11), never a bare HTTP client.

## Key Topics To Specify

- Assertion and Response XML structure, signing key selection (`purpose = 'saml'`), and the round-trip-validated parsing pipeline.
- Per-application SP configuration: entity ID, ACS URL(s), audience, `NameID` format and source attribute.
- IdP-initiated launcher page and its opt-in flag.
- Metadata upload/manual-entry flow and the audit visibility of an SP certificate change (a credential change, not a configuration edit — T4-8).
- Certificate rotation runbook and expiry alerting (paralleling `docs/SESSION-MANAGEMENT/03-JWKS-KEY-ROTATION.md`'s `keyctl`, but SPs do not refetch on their own schedule the way OIDC consumers can).

## Acceptance Criteria

- [ ] SP-initiated SSO completes in a real browser (not `curl`), against a Lax session cookie, per T4-7.
- [ ] The Assertion itself is signed, verified independently of the Response signature.
- [ ] `InResponseTo` replay (answering one AuthnRequest twice) is refused and tested.
- [ ] A decompression-bomb-shaped HTTP-Redirect payload is refused before full inflation.
- [ ] `NameID` is a persistent per-SP identifier, never email, by default.
- [ ] `AuthnContextClassRef` reflects the session's actual `auth_methods`; a `RequestedAuthnContext` the session cannot satisfy triggers step-up or a `NoAuthnContext` response rather than being ignored.
- [ ] IdP-initiated SSO is opt-in per application and reachable only from a CSRF-protected POST on this service's own origin.
- [ ] `form-action` on the response page names the registered ACS origin, resolved through the existing origin-resolution helper, never the request.
- [ ] A round-trip XML validator runs before signature verification; XXE/entity-expansion tests are labeled as regression guards against a future parser swap, not as proof of IdP-side safety.
- [ ] The library choice is recorded as an ADR naming its advisory history at the pinned version.
- [ ] Documentation states plainly that session revocation does not end an SP's session without Single Logout.

## Open Questions

- Whether Single Logout will be built in Phase 4 or deferred — affects the accuracy of any "revoking a session signs a user out everywhere" claim once SAML ships.
- Whose organization's key material signs assertions when a delegated (cross-organization) user reaches an SP through a Project Grant — unresolved by ADR-025, which addresses OIDC-path policy stacking only.

## Related Documents

- `docs/SESSION-MANAGEMENT/00-SESSION-ARCHITECTURE.md`
- `docs/SESSION-MANAGEMENT/03-JWKS-KEY-ROTATION.md`
- `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`
- `TASKS/PHASE-4-ENTERPRISE-INTEROP.md` § P4-07, P4-08, P4-09
- `MEMORY/records/2026-09-15-P3-15-phase-4-threat-review.md` (T4-6, T4-7, T4-8)
- `TASKS/BACKLOG.md` PG-40 (no-script hosted pages)
