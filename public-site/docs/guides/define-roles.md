---
id: define-roles
title: Define roles and assign them
description: Create roles for your application, give them permissions, and grant them to users.
sidebar_position: 2
---

# Define roles and assign them

By the end of this you will have a role that carries permissions, a user who holds it,
and a token that says so. Every command is a real call against the Management API.

You need an access token for an administrator with `ORG_ADMIN` over the organization.
The [quickstart](/docs/quickstart) ends with one.

```bash
ORG=org_...            # your organization id
PROJECT=prj_...        # the project your application belongs to
TOKEN=...              # an ORG_ADMIN access token
API=https://auth.example.com
```

## 1. Decide the permissions before the roles

A permission key is `resource:action` — the thing and the verb:

```
sale:create
sale:read
billing.invoice:write
```

Lower-case, and the resource may be dotted to group a family (`billing.invoice`). The
exact rule is in the [API reference](/docs/api-reference); the short version is that it has to
be safe to put inside a JWT claim, so the character set is deliberately narrow.

Write the permissions your application actually checks for first, then group them into
roles. Doing it the other way round produces roles named after job titles, and job titles
change more often than what the software does.

## 2. Create the role

```bash
curl -sX POST "$API/v1/organizations/$ORG/projects/$PROJECT/roles" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
        "key": "cashier",
        "display_name": "Cashier",
        "permission_keys": ["sale:create", "sale:read"]
      }'
```

The `key` is what appears in a token and what a grant references. It is **unique within
the project and cannot be changed afterwards** — a grant names a role by key and there is
no foreign key to cascade, so renaming would silently remove access from everyone holding
it.

`admin` in one project is unrelated to `admin` in another. Roles are scoped per project
on purpose: the alternative is a role name that means one thing in the billing system and
something else in the warehouse, and nobody notices until it matters.

Five names are reserved — `instance_owner`, `org_owner`, `org_admin`, `project_owner`,
`member` — because they are the names of the administrative roles that govern Zed Auth
itself, and a project role sharing one would arrive in a token beside a manager role
meaning something different.

### A role may have no permissions

```json
{ "key": "auditor", "display_name": "Auditor", "permission_keys": [] }
```

This is allowed and is sometimes right: a label is useful before the permissions behind
it exist. It grants nothing until you add them.

## 3. Grant it to a user

```bash
curl -sX POST "$API/v1/organizations/$ORG/users/$USER/grants" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{ "project_id": "'"$PROJECT"'", "role_keys": ["cashier"] }'
```

One grant per user per project, holding every role that user has there. To change the
set, replace it:

```bash
curl -sX PATCH "$API/v1/organizations/$ORG/users/$USER/grants/$PROJECT" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{ "role_keys": ["cashier", "auditor"] }'
```

**The whole set, every time.** A partial update of an array is ambiguous — add, remove or
replace? — and the audit event records what changed, which needs a complete before and
after.

`role_keys` may not be empty. A grant that grants nothing should not exist; to remove
access, delete the grant:

```bash
curl -sX DELETE "$API/v1/organizations/$ORG/users/$USER/grants/$PROJECT" \
  -H "Authorization: Bearer $TOKEN"
```

That takes effect immediately — the row is deleted rather than flagged. A token already
issued keeps the roles it was minted with until it expires, which is what
[`/v1/authz/check`](/docs/guides/authorization-checks) exists for.

### You cannot grant to yourself

An `ORG_ADMIN` administers every user in the organization and is one of them, so no
permission rule can express "everyone except you". It is refused explicitly. Ask another
administrator.

## 4. Read it back

```bash
curl -s "$API/v1/organizations/$ORG/users/$USER/grants" \
  -H "Authorization: Bearer $TOKEN"
```

```json
{
  "grants": [
    { "user_id": "usr_...", "project_id": "prj_...", "role_keys": ["cashier"] }
  ]
}
```

An empty list is the normal state for a new account, not an error. **A user with no grant
has no access at all** — there is no implicit or default role, so nothing is inherited
and there is nothing to reason about that a role does *not* carry.

## 5. See it in a token

The user signs in through your application, and their access token carries the role:

```json
"urn:authservice:iam:org:project:prj_...:roles": {
  "cashier": { "org_id": "org_..." }
}
```

What to do with that is the next guide:
[validate role claims in your service](/docs/guides/validate-role-claims).

## Deleting a role

```bash
curl -sX DELETE "$API/v1/organizations/$ORG/projects/$PROJECT/roles/$ROLE" \
  -H "Authorization: Bearer $TOKEN"
```

**Refused while any grant still references it**, with the count. It is not cascaded, and
that is deliberate: a cascade removes access from everyone holding the role, in every
application reading it, in response to a request that looks like tidying up — and nothing
afterwards explains why those people lost access.

Remove the grants first, or leave the role in place. `grant_count` on the role tells you
how many there are before you try.

## What this guide does not cover

Delegating a project to another organization so *they* assign its roles — a project
grant — arrives in Phase 4. Nothing above changes when it does.
