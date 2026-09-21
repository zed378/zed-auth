# P4-08 — SAML SP-Initiated and IdP-Initiated Flows

**Task**: `TASKS/PHASE-4-ENTERPRISE-INTEROP.md` § P4-08
**Plan refs**: `docs/PLAN/03-ARCHITECTURE.md` § SAML Identity Provider, `docs/PLAN/17` § Phase 4
**Depends on**: P4-07 (the assertion machinery), P1-11 (sessions), P1-12 (the hosted login)
**Threat review**: `MEMORY/records/2026-09-15-P3-15-phase-4-threat-review.md` T4-6, T4-7

---

## 0. Business objective

`docs/PLAN/17`'s Phase 4 criterion: **a SAML-only legacy application completes a full
SP-initiated login.** `P4-07` can build and sign an assertion; nothing can ask for one.
This card is the browser-facing half.

## 1. What is added

```
GET  /saml/metadata                 the IdP's metadata, now that it can name a real SSO URL
GET  /saml/sso                      HTTP-Redirect binding: AuthnRequest in the query
POST /saml/sso                      HTTP-POST binding: AuthnRequest in a form field
POST /saml/slo                      Single Logout, or a documented refusal (see §7)
```

Metadata moved here from `P4-07` deliberately: it advertises a `SingleSignOnService`
location, and publishing one before the endpoint exists is a promise rather than a
capability.

## 2. Functional requirements

| # | Requirement |
|---|---|
| F-1 | An `AuthnRequest` arriving on either binding is parsed under `P4-07`'s bounds and, where the registration demands it, signature-verified |
| F-2 | The service provider is identified by the request's `Issuer`, and **the ACS URL and audience are read from the registration, never from the request** |
| F-3 | A live session satisfies the request without re-authentication; otherwise the hosted login runs and returns here |
| F-4 | The assertion is POSTed to the registered ACS URL as a self-submitting form |
| F-5 | `RelayState` is echoed unchanged, size-bounded, and never interpreted |
| F-6 | IdP-initiated is **opt-in per service provider** and carries no `InResponseTo` |
| F-7 | An `AuthnRequest` id is single-use, so one request cannot be answered twice |
| F-8 | A SAML login writes the same `events` taxonomy as an OIDC one |

## 3. The threat review's constraints (T4-6), restated as requirements

Each of these is a requirement of this card, not advice:

| # | Constraint |
|---|---|
| C-1 | ACS URL and audience come from the registration, by `Issuer` lookup — never from the request |
| C-2 | The **assertion** is signed, not only the response. Some service providers verify only one, and an unsigned assertion inside a signed response is the wrapping mechanism |
| C-3 | `InResponseTo` names a **stored, single-use** AuthnRequest id |
| C-4 | The HTTP-Redirect binding is DEFLATE-compressed. A bound on the compressed query string does not bound the inflated document — the **inflated** byte count must be bounded explicitly (decompression-bomb advisories exist against the main Go SAML library) |
| C-5 | `NameID` must **never** default to email. A persistent, per-service-provider identifier: an SP keyed on email hands a deactivated user's account to whoever is issued that address next, and a shared email lets two SPs correlate users |
| C-6 | `AuthnContextClassRef` comes from the session's recorded `auth_methods` — the same source as the OIDC `amr` claim — never asserted independently. A `RequestedAuthnContext` the session cannot satisfy triggers step-up or a `NoAuthnContext` status, never silence |

T4-7 adds the cookie and `form-action` interaction: the self-submitting POST to a partner's
ACS URL must not be blocked by the login page's CSP, and the session cookie's `SameSite`
must still permit the redirect back. Both are checked against the existing headers rather
than relaxed for SAML.

## 4. Data model

`saml_service_providers` (migration 040) gains nothing. New:

```
saml_authn_requests
  id            text primary key   -- the AuthnRequest's own ID
  org_id        uuid
  sp_id         uuid → saml_service_providers
  relay_state   text               -- bounded, echoed, never interpreted
  created_at, expires_at, consumed_at
```

`consumed_at` is what makes C-3 single-use, and it is set by the same atomic update that
reads the row — the `P4-07` replay lesson applied a second time.

## 5. Abuse cases

| # | Scenario | Control |
|---|---|---|
| A-1 | An `AuthnRequest` naming an ACS URL of the attacker's choosing | C-1: the registration decides |
| A-2 | A decompression bomb on the Redirect binding | C-4: the inflated size is bounded |
| A-3 | One `AuthnRequest` answered twice | C-3: `consumed_at`, set atomically |
| A-4 | `RelayState` used as an open redirect or injected into the form | F-5: echoed as an opaque, bounded, HTML-escaped value |
| A-5 | An IdP-initiated assertion accepted by a service provider that did not want one | F-6: opt-in per registration |
| A-6 | A `RequestedAuthnContext` the session cannot meet, silently ignored | C-6: step-up or `NoAuthnContext` |
| A-7 | A SAML login invisible in the audit log | F-8 |

## 6. Tests

- Integration: the full SP-initiated flow on both bindings, against a real session.
- The bomb, the replayed request id, the ACS URL from the request, the oversized
  `RelayState` — each its own test.
- **`docs/PLAN/17`'s criterion end to end**, as an acceptance check on staging, the way
  `scripts/acceptance-delegation.sh` does for delegation.
- Mutations on: the registration lookup, the inflated bound, `consumed_at`, the
  `RelayState` bound, the opt-in flag.

## 7. Single Logout — the decision to make explicitly

The card allows implementing it or documenting its absence, and an undocumented gap in
logout is a security surprise. The decision here is to **document the absence** for this
card: SLO's front-channel variant requires iterating every service provider in a session
and is a well-known source of partial logouts, where a user is told they are signed out of
things they are not. That is a worse failure than not offering it.

`POST /saml/slo` therefore answers a `RequestDenied` status with a clear reason, the
metadata does **not** advertise an SLO endpoint, and the limitation is published in
`docs/IDENTITY-PROTOCOL/04`. Ending the session here still ends it — a service provider's
own session is its own to end.

## 8. Not in this card

Console management of SAML applications is `P4-09`.
