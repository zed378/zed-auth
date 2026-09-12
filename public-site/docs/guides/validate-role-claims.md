---
id: validate-role-claims
title: Validate role claims in your service
description: Read the roles in an access token correctly — the exact claim format, and what it is not safe to conclude from it.
sidebar_position: 3
---

# Validate role claims in your service

Your service receives an access token and needs to decide what its holder may do. This is
how to read it, and — more importantly — what reading it does **not** tell you.

## Verify the token first

Before any claim is worth looking at:

1. The signature verifies against a key from `/.well-known/jwks.json`, selected by the
   token's `kid`.
2. `iss` is your Zed Auth issuer, exactly.
3. `aud` contains **your** `client_id`. A token minted for another application is not a
   token for you, even though it verifies.
4. `exp` has not passed, and `nbf`/`iat` are sane.
5. The header's `typ` is `at+jwt`. An ID token also verifies, also has your `aud`, and is
   **not** an access token — accepting one means accepting a credential the browser was
   given for a different purpose.

Any standard JWT library does all of this. The two people usually skip are `aud` and
`typ`, and both are the difference between a check and the appearance of one.

## The claim

Roles live under a claim named for the project the token was issued for:

```json
"urn:authservice:iam:org:project:PROJECT_ID:roles": {
  "cashier": { "org_id": "ORG_ID" }
}
```

`PROJECT_ID` is your project's id, substituted into the key. The value is an **object
keyed by role key**, not an array — so a duplicate role key collapses rather than
producing a malformed claim.

In a token for a user holding two roles:

```json
"urn:authservice:iam:org:project:prj_7f3a:roles": {
  "cashier":  { "org_id": "org_19bd" },
  "auditor":  { "org_id": "org_19bd" }
}
```

The claim is **absent** rather than empty for a user who holds no roles in your project.
Read it defensively: an absent claim and an empty object both mean no roles.

### Why `org_id` is inside each value

It looks redundant — every role in the token comes from the same organization, so the
same value repeats.

It is there for Phase 4. Once a project can be delegated to another organization, the
same role name becomes reachable from two contexts, and this is what tells them apart. It
is emitted from the first release because adding a field to a claim consumers already
parse is a breaking change for every one of them, and emitting it now costs nothing.

Today: read it if you like, and do not require it to differ.

### Administrative roles

Roles governing Zed Auth itself are a separate claim and are not project-scoped:

```json
"urn:authservice:manager_roles": ["ORG_ADMIN"]
```

An array of strings, absent for the overwhelming majority of users. **Your service almost
certainly should not read this.** It says somebody administers the identity provider, not
that they should be able to approve a purchase order in your application.

## Reading it

```go
const roleClaimPrefix = "urn:authservice:iam:org:project:"

// Roles returns the role keys this token carries for one project.
func Roles(claims map[string]any, projectID string) []string {
    raw, ok := claims[roleClaimPrefix+projectID+":roles"].(map[string]any)
    if !ok {
        // Absent, or not an object. Both mean no roles — never an error, and
        // never a reason to fall back to a default set.
        return nil
    }
    keys := make([]string, 0, len(raw))
    for key := range raw {
        keys = append(keys, key)
    }
    return keys
}
```

```typescript
const ROLE_CLAIM_PREFIX = "urn:authservice:iam:org:project:";

export function rolesFor(claims: Record<string, unknown>, projectId: string): string[] {
  const claim = claims[`${ROLE_CLAIM_PREFIX}${projectId}:roles`];
  if (typeof claim !== "object" || claim === null) return [];
  return Object.keys(claim as Record<string, unknown>);
}
```

Then check for the role your endpoint needs. Not for a list of roles you happen to know
about — a role added next month should not silently pass because your check was written
as "anything except `viewer`".

## What the token does not tell you

**The claim is a snapshot taken when the token was issued.** A role revoked one minute
ago is still in a token minted two minutes ago, and nothing can reach back into a token
somebody already holds. Access tokens live ten minutes, so that is the outer bound of how
stale a claim can be.

For most decisions that is fine, and it is the point of a self-contained token: no round
trip, no dependency on the identity provider being reachable, no latency added to every
request.

It is **not** fine for:

- anything destructive or irreversible,
- anything involving money,
- anything an auditor will ask you to justify afterwards.

For those, ask at the time of the action:
[use `/v1/authz/check` for real-time decisions](/docs/guides/authorization-checks).

There is no middle option where the token is "mostly live". It is a snapshot or it is a
round trip, and pretending otherwise is how a revocation that everybody believed was
immediate turns out to have been up to ten minutes late.

## The bound on claim size

A token carries at most **64 role keys for one project**. Past that the list is
truncated rather than the claim dropped, so what remains is always a true subset of what
the user holds.

The consequence is stated deliberately: a consumer may **deny something it should have
allowed**, and will never **allow something it should have denied**. If your users can
hold more than 64 roles in one project, the role model is usually asking for fewer,
broader roles — but until then, the failure direction is the safe one.

## A checklist

- [ ] Signature verified against the published key set, by `kid`
- [ ] `iss` matches your issuer exactly
- [ ] `aud` contains your own `client_id`
- [ ] `typ` is `at+jwt`, not `JWT`
- [ ] `exp` checked
- [ ] The role claim is read by its full key, with your project id substituted
- [ ] An absent claim is treated as no roles, never as an error or a default
- [ ] Decisions that cannot be undone ask the service rather than trusting the claim
