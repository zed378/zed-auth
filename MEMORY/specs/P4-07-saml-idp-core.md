# P4-07 — SAML 2.0 Identity Provider: Core

**Task**: `TASKS/PHASE-4-ENTERPRISE-INTEROP.md` § P4-07
**Plan refs**: `docs/PLAN/03-ARCHITECTURE.md` § SAML Identity Provider, `docs/PLAN/05` § Standards Used, `docs/PLAN/11` § Security Testing
**Depends on**: P1-03 (keys), P1-11 (sessions), P1-18 (applications)

---

## 0. Business objective

Enterprise and legacy applications that cannot speak OIDC still need single sign-on. This
card makes the service a SAML 2.0 **Identity Provider**: it issues signed assertions about
a user who has authenticated here, to a Service Provider that has been registered.

`P4-08` adds the two flows (SP-initiated and IdP-initiated) and the bindings. This card is
the assertion machinery underneath: keys, XML, signing, verification, lifetime, audience,
replay.

## 1. The decision this card turns on

SAML's security history is dominated by two classes of bug: **XML parsing** (XXE, entity
expansion, DTD) and **signature wrapping** (a valid signature over a document, and a
*different* element actually consumed). `TASKS`' step 1 says to use a well-maintained
library rather than hand-rolling, and that instruction is correct.

The Go options, assessed rather than assumed:

| Option | Assessment |
|---|---|
| `crewjam/saml` | The de-facto Go SAML library, covers IdP and SP. Brings the whole protocol: metadata, bindings, flows |
| `russellhaering/goxmldsig` + `beevik/etree` | What `crewjam/saml` itself uses underneath — XML signing and verification, and a DOM |
| Hand-rolled | Refused by the task card, and correctly |

**Chosen for this card: `goxmldsig` and `etree` directly**, with every property this
service depends on checked in our own code.

The first draft of this spec chose `crewjam/saml`, and building the card changed the
answer. This card is the assertion *machinery* — parse safely, verify a signature, check
the audience and the window, refuse a replay. `crewjam/saml`'s value is the layer above
that: metadata, bindings, and the two flows, which are `P4-08`. Taking the whole library
now would mean depending on it for the parts where its behaviour is the security
guarantee, which is exactly what this package is written not to do. It stays on the table
for `P4-08`, where what it provides is protocol plumbing rather than a security decision.

**The security decisions are ours, and each has a test that fails when the check is
removed**: the document bounds, the DTD and entity refusal, the identity of the signed
element, the audience, the window, the replay. `goxmldsig` is the signature primitive;
it is not the policy.

`go.mod` gains two direct dependencies. The `demo` module stays at zero, and
`govulncheck` covers the new tree (its two current findings are pre-existing, in `grpc`
and `x/crypto`, and unrelated).

## 2. Functional requirements

| # | Requirement |
|---|---|
| F-1 | A SAML signing key exists, distinct from the OIDC keys, in the existing `signing_keys` table under `purpose = 'saml'` |
| F-2 | The service publishes IdP metadata (entity ID, SSO endpoint, certificate) for a registered SP |
| F-3 | An `AuthnRequest` is parsed under a hardened XML configuration: no DTD, no external entities, bounded document size, bounded entity expansion |
| F-4 | A signed `AuthnRequest` has its signature verified, and the **signed element must be the element consumed** |
| F-5 | An assertion is issued for an authenticated user, signed with the SAML key, carrying `NotBefore`/`NotOnOrAfter` and an `AudienceRestriction` naming exactly one SP |
| F-6 | Attributes released follow the same discipline as `/oauth/userinfo`: only what the SP is configured to receive |
| F-7 | An assertion ID is recorded and a replay is refused |
| F-8 | A session established by OIDC satisfies a SAML request without re-authentication |

## 3. Non-functional

- Assertion issuance adds no more than the OIDC token path's budget (`docs/PLAN/12`).
- The XML parser's bounds are constants, published in `docs/IDENTITY-PROTOCOL/`.

## 4. Data model

`signing_keys` already carries `purpose IN ('oidc','saml')` with one `current` per purpose,
so **no migration is needed for keys** — `P1-03` provisioned it.

New: `saml_service_providers` (per application) and `saml_assertion_ids` (replay).

```
saml_service_providers
  id, application_id → applications, org_id
  entity_id            text, unique per instance
  acs_url              text            -- exact match, like an OIDC redirect URI
  attribute_release    text[]          -- what this SP receives
  want_signed_requests boolean
  certificate          text            -- the SP's, for verifying its requests
  created_at, revoked_at

saml_assertion_ids
  assertion_id text primary key
  issued_at, expires_at            -- pruned like other expiring rows
```

Both under RLS, tenant-scoped by `org_id`.

## 5. API

Metadata and the flows are `P4-08`'s. This card exposes:

```
GET /saml/metadata            the IdP's own metadata
```

Everything else is internal until `P4-08`.

## 6. Abuse cases

| # | Scenario | Control |
|---|---|---|
| A-1 | XXE via a crafted assertion | DTD and external entities disabled; test feeds a malicious document |
| A-2 | Billion-laughs entity expansion | Entity expansion and document size bounded; test feeds one |
| A-3 | Signature wrapping | The signed element is the consumed element, checked by identity, not by "a valid signature exists" |
| A-4 | Assertion replay | Assertion IDs recorded with their expiry; a second presentation is refused |
| A-5 | Audience confusion — an assertion for SP A accepted by SP B | `AudienceRestriction` names exactly one SP and is checked |
| A-6 | An unregistered ACS URL | Exact match against the registered value, the same discipline as OIDC redirect URIs |
| A-7 | A SAML key used to mint an OIDC token, or the reverse | Separate purposes; a key of the wrong purpose is refused |

## 7. Tests

- Unit: the XML hardening (each of A-1, A-2), signature wrapping (A-3), audience (A-5).
- Integration: issue → consume → replay refused (A-4); an OIDC session satisfying a SAML
  request (F-8).
- **Fuzz** the assertion parser (`docs/PLAN/11`, and `internal/signing` already has one to
  model on).
- Mutations on: the signed-element identity check, the audience check, the replay insert,
  the purpose separation.

## 8. Not in this card

The flows, the bindings, `RelayState`, and Single Logout — all `P4-08`. Console management
of SAML applications is `P4-09`.

## 9. Risks

- **The library's defaults are not our guarantees.** Mitigated by checking each property
  ourselves, with a test that fails if the check is removed.
- **XML is a large attack surface** and the fuzz target is the honest response.
- **Scope creep into `P4-08`.** The line is: this card can issue and verify an assertion in
  a test; it does not serve a browser flow.
