# P4-07 (part 1) — the SAML assertion machinery

**Date**: 2026-09-21
**Task**: `TASKS/PHASE-4-ENTERPRISE-INTEROP.md` § P4-07
**Spec**: `MEMORY/specs/P4-07-saml-idp-core.md`
**Branch**: `feat/P4-07-saml-idp-core`
**Status**: the security layer and issuance are built and verified; metadata and the
OIDC-session bridge are not. The card stays open.

---

## What is built

`backend/internal/saml/`, six files and their tests:

| File | Property |
|---|---|
| `xmlsafe.go` | A document is bounded, DTD-free, entity-free and depth-bounded **before** anything parses it |
| `verify.go` | The element whose signature was checked is the only element a caller can read |
| `conditions.go` | The audience names this service provider and the window has not passed |
| `replay.go` | An assertion ID is accepted once |
| `keys.go` | The SAML key is RSA, certified, and separate from the OIDC key |
| `issue.go` | An assertion says only what the service provider was registered to receive |

Migration 040 adds `saml_service_providers` and `saml_assertion_ids`, both under RLS.
Migration 041 adds `signing_keys.certificate`, required for `saml` and absent for `oidc`.

## The decision that changed while building (ADR-027)

The spec chose `crewjam/saml`. The code uses `goxmldsig` and `etree` directly — what
`crewjam/saml` itself uses underneath — and the spec has been corrected rather than left
describing a plan nobody followed.

The reason is the card's own subject. This is the layer where a library's behaviour would
*be* the security guarantee, and the whole package is written on the premise that it must
not be. `crewjam/saml`'s value is metadata, bindings and the flows, which are `P4-08`, and
it remains on the table there — protocol plumbing is a different kind of dependency from a
security decision.

## What the mutations found

Nineteen mutations. Sixteen turned a test red immediately. **Three did not, and all three
were the same defect: a real check that no test exercised.**

### The DOCTYPE guard was unverified

Deleting the DTD refusal entirely left every test green. The XXE and billion-laughs
documents were being refused for their **undefined entities**, not for their DTD — so the
DTD check was live code that nothing proved worked.

Fixed by a document that carries a DOCTYPE and references no entity at all, which only
the DTD check can refuse.

### The wrapping guard was unverified, and the design was wrong

The first wrapped document was refused by the *signature* check, because goxmldsig found
no signature on the forged root. The wrapping comparison never ran.

Building the attack that actually validates — the genuine `Signature` moved up to be a
child of the forgery, referencing an element parked under `Extensions` — showed something
better: goxmldsig refuses any signature that does not reference **the element it was
handed**. So the defence is a choice about what to pass in, not a check performed
afterwards:

> Validate the element that is about to be consumed, not the document.

`Verify` now finds the element the caller asked for and validates *that*. A signature over
anything else is not noticed late; it never validates. The identity comparison stays as
defence in depth, and is honestly labelled: no document this package can construct makes
it fire, so no mutation can turn it red.

A second mutation — returning the envelope instead of the verified element — also survived
until a `Response`-wrapping-`Assertion` test existed, which is the ordinary SAML shape and
the only case where the two differ.

### Round-trip stability, which the threat review demanded and I had missed

T4-6 requires `mattermost/xml-roundtrip-validator` to run **before** signature
verification: Go's `encoding/xml` can parse a document, re-serialise it, and produce
something that parses differently, so the bytes that were signed are not the bytes that
are read. It is now in `ReadDocument`.

No document this package accepts reaches its refusal — the strict token walk and the
directive rule catch every class it knows about first — so it is a second check that
cannot be made to fail, and it is labelled as such rather than counted.

### Replay: the primary key is the defence, not the upsert

Replacing the atomic upsert with `SELECT`-then-`INSERT` did **not** fail the concurrency
test, and the honest reason is that it should not: the primary key stops the second
acceptance in both shapes. What the upsert buys is a clean `ErrReplayed` instead of an
occasional raw constraint violation — a better error, not a stronger guarantee.

So the property is tested where it lives: a test inserts a duplicate straight past the
application logic and asserts the database refuses it. Dropping `PRIMARY KEY` from the
migration turns that red.

## The gate found a signature bypass in the signature library

`goxmldsig` was pinned at v1.4.0 on the evidence that `govulncheck` reported nothing
against it. That evidence was worthless: nothing imported it yet, so no path was
reachable. The first gate run after `internal/saml` actually called it failed with
**GO-2026-4753, "Loop Variable Capture Signature Bypass"** — reachable from both `Issue`
and `Verify`, in the library chosen to validate signatures.

Upgraded to v1.6.0; `govulncheck` is clean again, with only the two pre-existing
unreachable findings (`grpc`, `x/crypto/openpgp`). ADR-027 records the correction rather
than the original claim.

The lesson is narrow and worth keeping: **an advisory check against a dependency nothing
calls proves nothing.** Re-run it after the first caller exists.

## Two checks that cannot be made to fail

`Verify` refuses an empty certificate list before calling goxmldsig. Removing it changes
nothing, because goxmldsig refuses an empty trust store too. The round-trip validator is
the second, for the reason above.

Both stay: each is the property this package promises, and a promise should not be a
comment about a dependency's current behaviour. Both are recorded here as unverifiable
rather than counted as caught mutations.

## Fuzzing

`FuzzReadDocument` and `FuzzVerify`, 185k executions in 20s with no crashes. The contract
is narrow because a fuzzer cannot know what a SAML document means: never panic; return no
bytes with an error; return the bytes **exactly as they arrived** on success, because a
caller has to canonicalise what was sent rather than what a parser reconstructed.

## A gap the migration exposed

Migration 041 makes a certificate required for a `saml` key — and `signing.Store.Insert`
had no way to write one, so the first SAML key would have been refused by its own CHECK.
`KeyPair` and `Key` now carry `CertificatePEM`, `Insert` writes it (NULL rather than an
empty string, so `IS NOT NULL` means something), and `Load` reads it back. An integration
test inserts both an OIDC key and a SAML key and asserts neither key set can resolve the
other's key.

## What issuance does and does not say

- The **Recipient** is the registered ACS URL, never one a request supplied. An assertion
  naming wherever the request pointed is an open redirect with a signature on it.
- The **AuthnInstant** is when the user authenticated, not when the assertion was built.
  A service provider deciding whether a sign-in is recent enough asks about the first, and
  reporting the second makes every assertion look freshly authenticated.
- **Attributes are an allow list.** A service provider that registered for nothing receives
  nothing, not everything, and a new attribute reaches no integration until somebody
  registers it.
- `bearer` subject confirmation is stated plainly rather than implying a holder-of-key
  binding no registration here carries.

Four more mutations, all red: release everything; take the Recipient from the request;
report the issuance as the authentication instant; drop the certificate/key match check.

## Still owed on this card

- `GET /saml/metadata`.
- A session established via OIDC satisfying a SAML request — largely `P4-08`'s flow, and
  the card's Definition of Done lists it here.
- Wiring the package into the service: nothing creates a SAML key or reads a registration
  from `saml_service_providers` yet, so the code is complete and unreachable. That
  wiring arrives with `P4-08`'s flows, which is what would call it.
