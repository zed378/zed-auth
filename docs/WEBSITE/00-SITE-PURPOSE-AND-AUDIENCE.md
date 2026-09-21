# 00 - Site Purpose and Audience

> Category: **Public Website** (`docs/WEBSITE/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-18, P0-19, P1-25, P2-15, P3-13 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

Say who the public site is for and what it is not allowed to become, because both
constrain every other decision about it.

## Scope

`public-site/`.

## As Built

### Three audiences, in the order they arrive

1. **An engineer evaluating the product.** Lands on `/`, wants to know in thirty seconds
   what it does and whether it is real. Leaves for `/docs/quickstart` or leaves entirely.
2. **An integrator building against it.** Lives in `/docs/` and `/docs/api-reference`.
   Needs the exact shape of a request, not prose about identity.
3. **Someone deciding whether to trust it.** Reads `/about`, the changelog, and — more
   than anything — notices whether the site claims things the product does not do.

### The site's job is discovery and accuracy, in that order of visibility and the reverse
order of importance

The tagline — *"One login. Every app. Full control over who can do what."* — is
positioning. It describes what the product is *for*. Directly beneath it sits a status
statement saying what is actually shipped, and that adjacency is the site's whole editorial
stance.

### What the site must never become

**A place where a capability is implied.** `docs/UI-UX/21` writes four landing-page
capabilities in the present tense. Through Phase 0, copying them as written would have
claimed four things that did not exist — so all four sat on the page as *design*, each
labelled with the roadmap phase that delivers it. Two are now labelled "Shipped — Phase 1".

The rule has a second direction, and it is the one nobody complains about:

> A card still labelled with a phase after that phase shipped is as inaccurate as one
> claiming something that does not exist.

**A place where internal documents leak.** `docs/PLAN/20` names three that must never be
published: `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`, because publishing it hands
an attacker the list of things that were considered and, by omission, the ones that were
not; `docs/PLAN/14-DEPLOYMENT.md`, for infrastructure topology; and
`docs/PLAN/18-RISK-REGISTER.md`, entirely.

**A second implementation of the API reference.** It is generated. See
[`03-GENERATED-API-REFERENCE.md`](./03-GENERATED-API-REFERENCE.md).

### What the site does not claim, and says so

There is **no hosted offering**. "Shipped" on this site means built, deployed and
demonstrable — not that a visitor can sign up. Standing up a new deployment still needs
database access to create the first organization and administrator (`PG-26`), and the
status sentence exists to keep that from being glossed over.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Copy claims only what is shipped | standing rule | `public-site/scripts/check-claims.mjs`, `public-site/CLAIMS.md` |
| Every capability carries a phase label | required | `public-site/scripts/check-claims.mjs` |
| A stale label fails too | required | same |
| Three internal documents never published | required | `public-site/scripts/check-no-internal-leak.mjs` |
| API reference | generated, never hand-written | `AGENTS.md` rule 5 |

## Verification

- `public-site/CLAIMS.md` — the audit, dated, with the basis for every claim.
- `npm run check` in `public-site/` — tokens, contrast, boundary, typecheck, build,
  claims, leak.

## Related Documents

- [`02-CONTENT-GOVERNANCE.md`](./02-CONTENT-GOVERNANCE.md)
- [`../UI-UX/21-CONTENT-AND-COPY-STRATEGY.md`](../UI-UX/21-CONTENT-AND-COPY-STRATEGY.md)
