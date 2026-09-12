# P2-09 — Tenant Resolution Strategy

| | |
|---|---|
| **Date** | 2026-09-12 |
| **Task** | `TASKS/PHASE-2-RBAC-MULTITENANCY.md` § P2-09 |
| **Phase** | Phase 2 — RBAC & Multi-Tenancy |
| **Surface** | backend |
| **Decision** | [ADR-023](../DECISIONS.md) |
| **Branch** | `feat/P2-09-tenant-resolution` |
| **Status** | Complete — not yet on staging |

---

## Writing the decision down revealed one had already been made

`docs/PLAN/08` Part B lists four ways to decide which organization a request belongs to: a subdomain, a path segment, the user's email domain at login, or a single default organization — and names the last as the MVP choice.

The service does **none of them**. The organization is the one that owns the OIDC client the request is authenticating to, determined before a password is typed, from a value the caller must already supply for the protocol to work at all.

Nobody chose that. It fell out of `P1-05` giving applications an `org_id` and `P1-06` resolving the client before anything else happens. It has been the strategy since Phase 1 and was written down nowhere.

## Why it is better than the four, rather than merely different

Each of the plan's options resolves the tenant from something the **request** carries. This resolves it from something the **service already knows**, and that is where the security difference sits.

A subdomain is a `Host` header — client-supplied, and trusting it means trusting a proxy chain to rewrite it honestly. A path segment leaks the organization's name into every URL, log and referrer. Email-domain routing fails for exactly the users most likely to need it — shared domains, personal addresses, contractors — and resolves the tenant *after* the address is typed, which means the login page must be rendered before the organization's branding and password policy are known. A single default organization does not survive the second tenant.

The client-based rule has a property none of them has: **there is nothing for a caller to supply, and therefore nothing to forge.** A request carrying `X-Org-Id: someone-else` changes nothing because nothing reads it. An unknown client is refused before any organization is chosen, so there is no default to be tricked into falling back to.

It also subsumes the MVP mode the plan wanted: with one organization owning every client, every request resolves to it with no special case. Single-organization deployments are unchanged — which is the point, since that is what Phase 1 shipped.

## How step 4 is tested, and why not behaviourally

"A client-supplied header cannot change the resolved tenant" is a claim about **every** header, and a behavioural test can only send the ones somebody thought of. Sending `X-Org-Id` proves nothing about `X-Tenant-Id`.

So the test reads the source for any sign that a tenant is being taken from the request — a header, a query parameter, a form field. The property it actually protects is against the *convenience*: an `X-Org-Id` for a support tool, an `org` parameter for a debugging session. Each looks harmless in isolation, and each turns the tenant into something the request asserts.

`chi.URLParam(r, "org_id")` is deliberately **not** forbidden. The Management API takes the organization in the path by design and checks it against the caller's manager roles (`P1-15`). The path is part of the contract; a header is not.

A second test pins a distinction worth keeping explicit: `AUTH_TRUST_PROXY_HEADERS` lets a deployment assert that `X-Forwarded-For` comes from a proxy it controls, and that trust is about the client's **address**. It must never extend to naming a tenant.

## Verified

| | |
|---|---|
| Architecture | 2 tests, one of them proven by planting an `X-Org-Id` read and watching it get caught |
| Existing behaviour | Unchanged — this task added no production code, which is the correct outcome for a decision that was already implemented |
| Gates | Green |

## Recorded rather than fixed

**`PG-33`**: Part B should name this strategy, with the other four available as additions for a deployment that needs a tenant chosen before a client is named. One paragraph of plan text — and a plan change, so it goes through the deliberate process rather than through the task that noticed.
