# P4-09 — SAML Application Type and Metadata Management

**Date**: 2026-09-21
**Task**: `TASKS/PHASE-4-ENTERPRISE-INTEROP.md` § P4-09
**Branch**: `feat/P4-09-saml-app-type`
**Depends on**: `P4-07`, `P4-08`, `P1-18`

---

## What is built

| Piece | What it guarantees |
|---|---|
| `saml.ParseSPMetadata` | A partner's metadata is read through the same XML gate as an assertion — size and depth bounded, DTDs and entities refused, round-trip stable — and each field is checked against what this service will actually use |
| `saml.Registrations` | Create, read, list and replace a registration inside the caller's tenant transaction |
| `application` handler | A `saml` application and its registration are one body, one transaction; the list carries each registration in one query per page |
| `/saml/metadata` | Publishes the key that signs **and** the key that will sign next |
| `SamlAttribute` enum | The releasable attributes are one closed set, held together across the contract, the validator and the subject builder by two tests |
| Console | Register from metadata or by hand, see where sign-in ends, a certificate warning in words, and edit the two switches with their cost beside them |

**No migration.** Everything needed was already in 040–045.

**Mutations**: 9 on the parser, 3 on published keys, 13 on the API, 5 on the attribute
contract, 1 on the list, 7 on the console — **38, all caught** after the fixes below.

## Decisions worth keeping

### One body, one transaction

A second call to a sub-resource would leave a window in which the application exists and
matches no AuthnRequest. An administrator who stops halfway would have something that
looks registered and is not. A duplicate entity ID now rolls the application back with it,
and the test proves it by listing the project afterwards.

### The metadata is parsed on the server, never in the console

It is untrusted XML. A client-side parser would be a second one to keep as careful as the
first, and a console that pre-parsed it would be showing the administrator a reading the
server might not agree with.

### Rules for choosing among several ACS URLs are stated, not inherited

HTTP-POST only, `https` only, `isDefault` before the lowest `index`. Otherwise the choice
is whatever order a library happens to iterate, and a registration pointing at a URL this
service will never POST to cannot work.

### `previous` keys are not published

This service issues rather than consumes. A demoted key verifies nothing anybody asks
about; publishing it would keep it trusted after it stopped being used.

### The expiry warning is the server's, not the browser's

Thirty days, derived at read time so it cannot go stale against the certificate it
describes, and the console does not second-guess it with the administrator's own clock.
The first version of the badge did — and the lint rule that refused `Date.now()` during
render was right twice over.

## What was found

Five defects, and every one compiled, and most passed a test of one half.

**`Published` silently dropped every `next` key.** It asked `signing.Key.Signer` for each
key, and that accessor refuses anything that is not `current` — by design. The document
went back to naming one certificate and the rotation overlap this task existed for did not
exist. The metadata test called `saml.Metadata` directly with two keys; the handler test
used a fake. Fixed by returning certificates rather than keys — publication needs no
private half, so asking only for what it needs makes the mistake unavailable — and by an
integration test over the real `signing.Store`, which reintroducing the `Signer` call
turns red.

**The OIDC grant default was applied to SAML applications**, which take no grants, so
every creation was refused for a grant nobody asked for. The first fix returned `nil` into
a NOT NULL column.

**`translateWriteError` asserted `*pq.Error`.** The driver is pgx, which never produces
one, so the branch could not execute and a duplicate entity ID was a 500. It now matches
the constraint name, as `organization`, `project` and `grant` already do.

**`attribute_release` accepted any string.** A name nobody produces saves cleanly and
releases nothing — `Email` with a capital would leave an administrator believing the
service provider receives an address. One closed list now, refused against, published as
an enum, and tested against what a subject actually carries. The spec-side test reads
`openapi.yaml` itself, because a hand-written list of generated constants would not notice
a fifth value.

**Two tests were vacuous.** "A SAML application needs a registration" passed because a
later layer refused the empty one for a different field; "the XML gate's refusal is
generic" looked for `doctype` in a message that says `DTD`. Both now assert the specific
thing.

## Redundancy that is deliberate

An `http` ACS URL and a signed-without-certificate registration are refused twice — by the
application check, which names the field, and by migration 040's CHECK. A mutation removing
one layer survives by design; removing both is caught.

## Still owed

- **BL-08** and **BL-10**, carried from `P4-08`: the sweepers nothing calls, and the guard
  against reading an RLS table under instance scope.
- A registration holds one certificate. A partner mid-rotation publishes two; the parser
  takes the first usable one and says so in its doc comment.
