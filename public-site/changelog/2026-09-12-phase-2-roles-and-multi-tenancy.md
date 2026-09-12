---
slug: phase-2-roles-and-multi-tenancy
title: Phase 2 — roles, grants and more than one organization
authors: [zed]
tags: [release]
date: 2026-09-12
---

Access means something now. You can define what a role carries, give it to somebody, read
it out of their token, and ask at the moment of an action whether it still holds — which
is a different question, and the docs say plainly how different.

The [authorization guides](/docs/guides) are new and they are the part worth reading
before integrating. One of them exists mostly to talk you out of trusting a token claim
for anything irreversible.

<!-- truncate -->

### Added

- **Roles and permission keys.** A role is a named set of `resource:action` permissions
  belonging to exactly one project — `admin` in one project is unrelated to `admin` in
  another, deliberately. A role's key cannot change, because a grant references it by
  name and renaming would silently remove access from everyone holding it. A role any
  grant still references cannot be deleted; the refusal names the count, rather than
  cascading a removal nobody asked for.
- **Grants** — one user's roles in one project, replaced as a whole set rather than
  patched, because a partial update of an array is ambiguous and the audit event needs a
  complete before and after. A user with no grant has no access: there is no implicit
  role and nothing is inherited.
- **Role claims in the access token**, under a claim named for the project, with each
  role's value carrying the organization it came from. That `org_id` is redundant today
  and is emitted anyway — Phase 4's delegation makes the same role name reachable from
  two contexts, and adding a field to a claim consumers already parse is a breaking
  change for every one of them.
- **`POST /v1/authz/check`** — the live decision, read from grant data rather than from
  the caller's token. A revocation is honoured by the next call. If the decision cannot
  be reached the answer is `503`, never `200` with `allowed: false`: those are different
  events, and conflating them makes an outage look like a policy change.
- **Decision caching with a stated window.** Grants and role definitions are cached for
  30 seconds and the cache is cleared when either changes, so the normal case has no
  delay at all. The 30 seconds is the backstop for a cache that was unreachable or a
  change made outside the API — and it is documented as the bound you are accepting for
  anything irreversible, rather than described as "real-time" and left at that.
- **More than one organization.** Tenant isolation is enforced in the database by row
  level security, and the organization a request acts in comes from the OIDC client it
  authenticated to — so there is nothing for a caller to supply and therefore nothing to
  forge.
- **Per-organization policy, enforced rather than stored.** Session lifetime and
  permitted sign-in methods now actually govern login; before this they were accepted,
  validated, returned by the API, and read by nothing.
- **Console: Roles, Authorizations, an organization switcher, and the Access policies
  screen.** The switcher offers exactly the organizations you administer, from a new
  `GET /v1/me/organizations` rather than from your token — and forcing a different one
  into the URL is refused by the service, not by the absence of a menu entry.

### Fixed

- **A settings update naming one password rule discarded the other two.** The API
  promised a merge "key by key"; the implementation merged one level deep, so raising a
  minimum password length silently reset a deliberate "uppercase not required" and
  dropped a deliberate "never expires". Nothing reported it. It is now a real recursive
  merge, and arrays are still replaced rather than concatenated — otherwise a sign-in
  method could never be removed.

### Not in this release

Project Grants — delegating a project to another organization so they manage their own
people's access — is Phase 4. Attribute-based policies are Phase 4b, and only if a real
requirement appears that roles genuinely cannot express. Neither is half-present: the
database refuses a delegated grant today rather than accepting one nothing enforces.
