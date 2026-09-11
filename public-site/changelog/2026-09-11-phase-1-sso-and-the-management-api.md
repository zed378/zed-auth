---
slug: phase-1-sso-and-the-management-api
title: Phase 1 — single sign-on and the management API
authors: [zed]
tags: [release]
date: 2026-09-11
---

You can log in now. A user signs in once, and a second application — a different
`client_id`, a different hostname, a different session — opens without asking again.
That sentence was the whole point of Phase 1, and there are two applications in the
repository that demonstrate it rather than assert it.

The [quickstart](/docs/quickstart) is real. Every command on it was executed, in order,
against a running deployment.

<!-- truncate -->

### Added

- **Single sign-on over OIDC and OAuth 2.1.** Authorization Code with PKCE, `S256` only;
  a hosted login page; RP-initiated logout that ends the session at the provider rather
  than only in one application. Authorization codes are single-use, redeemed atomically,
  and every failure to redeem one answers the same `invalid_grant` — an error
  distinguishing "unknown code" from "wrong redirect URI" tells whoever holds a code
  that the code is real.
- **Token signing with rotation.** Keys live in the database with a four-state
  lifecycle, a retiring key stays published through an overlap window, and the key set
  is served with a short cache lifetime. A consumer that caches the key set and refetches
  on an unknown `kid` never notices a rotation happening.
- **The management API** — organizations, projects, applications, users and the audit
  log, with keyset pagination and one error envelope for every non-2xx response. There
  are no console-only endpoints. That is a constraint the project holds itself to, not a
  description of where the work happens to have reached.
- **Users without passwords crossing the wire.** No password appears in any request or
  any response, in either direction. An account is created by invitation, and the person
  who owns it sets the password through a link sent to their own address — which is also
  the only thing that proves the address is theirs.
- **The management console** — sign-in, the organization overview, projects,
  applications, users and the audit log. Every authorization decision it appears to make
  is made again by the API; hiding a button has never been a security control.
- **An append-only audit log you can read.** Newest first, filterable by event type,
  actor and time range, with cursor pagination that carries both the timestamp and the
  id — six events written in the same instant page correctly.
- **Two demo applications**, one confidential and one a public SPA, in
  [`demo/`](https://github.com/zed378/zed-auth/tree/main/demo). They are the SSO proof
  and a reference implementation: about 200 lines of token verification, standard
  library only, strict on purpose. Copy them.

### Changed

- **This site says Phase 1 is built**, and the capability cards that said "Phase 1" now
  say "Shipped". The check that keeps the page honest used to run in one direction —
  it caught a capability claimed before it existed. It now also catches one still
  described as planned after it shipped, which is the same inaccuracy failing in the
  direction nobody notices.

### Known limits

Said plainly, because a release note that only lists additions is a sales page:

- **No hosted signup.** You run this yourself, from source. Standing up a *new*
  deployment still needs database access for the first organization and the first
  administrator — the API path for that does not exist yet.
- **Roles are not in tokens.** The claim namespace is reserved and empty; Phase 2 fills
  it. Project Grants and attribute-based policies are Phase 4.
- **Refresh tokens do not rotate.** A stolen one is replayable for its lifetime, which
  is why that lifetime is shorter than it will be once Phase 3 adds reuse detection.
- **One virtual machine.** That does not meet the availability requirement this
  project's own deployment plan sets for production, and it is recorded as an accepted
  interim rather than quietly tolerated.
