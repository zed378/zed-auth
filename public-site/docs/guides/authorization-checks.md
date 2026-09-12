---
id: authorization-checks
title: Use /v1/authz/check for real-time decisions
description: Ask the service at the moment of the action — what it reads, how fast a revocation lands, and why a failure is not a denial.
sidebar_position: 4
---

# Use `/v1/authz/check` for real-time decisions

A token's role claims are a snapshot. This endpoint is the live answer, read from grant
data at the moment you ask.

Use it where being a few minutes stale is not acceptable: anything destructive, anything
involving money, anything an auditor will ask you to justify. Use the token's claims
everywhere else — a round trip per request is a cost, and for most requests it buys
nothing.

## The call

```bash
curl -sX POST "$API/v1/authz/check" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
        "subject":  { "user_id": "usr_8821" },
        "action":   "approve",
        "resource": { "type": "purchase_request", "id": "pr_9931" }
      }'
```

```json
{
  "allowed": true,
  "matched_policy": "finance_approver",
  "reasons": ["role finance_approver carries purchase_request:approve"]
}
```

**The permission asked about is `resource.type` + `:` + `action`.** So the call above
asks whether the subject holds a role carrying `purchase_request:approve`. There is no
separate permission field: the two halves you already have are the permission.

## What is not in the request

The organization and the project come from **your access token**, and neither can be
named in the body. A caller cannot ask about a tenant that is not theirs, because there
is nowhere in the request to put one.

`resource.id`, `resource.attributes` and `context` are accepted and are **not read** by
the role-based decision — a role is held over a kind of resource, not over one instance.
They are what attribute-based policies will read in Phase 4b, and accepting them now
means a consumer sending them today does not change its code later.

None of the three is ever logged. `resource.attributes` in particular is your business
data, and this service has no business keeping it.

## How fast a revocation lands

This is the part worth being precise about, because "real-time" is doing a lot of work in
most documentation.

Grants and role definitions are cached for **30 seconds**, and **the cache is cleared the
moment either changes**. In the normal case — a revocation through the API — the next
check after it is already correct. There is no window to wait out.

The 30 seconds is a backstop for the two cases clearing cannot cover:

- the cache was unreachable at the moment of the change;
- somebody changed a grant outside the API, directly in the database.

**If either happens, a revoked permission may still be honoured for up to 30 seconds.**

That is the real bound. If you are building a step that cannot be undone, that is the
window you are accepting — and it is worth knowing it exists rather than discovering it
during an incident.

Compare it with the alternative: trusting the token's claims instead gives you a window
of up to the full access-token lifetime, which is ten minutes.

## Treat anything that is not a 200 as denied

If the decision cannot be reached — a dependency is unavailable — the response is
**`503`, not `200` with `allowed: false`**.

The difference is the whole design of the endpoint's failure mode:

- `allowed: false` means *we checked, and the answer is no*. A caller may cache it, log
  it as a policy decision, show the user a permissions message.
- `503` means *no decision was reached*. Nothing was checked. Caching it would turn a
  brief outage into a persistent denial, and reporting it as a policy decision would
  make an outage look like somebody's access was removed.

In your service:

```go
decision, err := authz.Check(ctx, req)
switch {
case err != nil:
    // Not a denial — a failure to decide. Refuse the action, and say so in
    // those terms: "we could not verify your permissions", never "you do not
    // have permission".
    return errServiceUnavailable
case !decision.Allowed:
    return errForbidden
}
```

Both refuse the action. They are still different events, and conflating them in your logs
means an outage and a permissions problem look identical when you are trying to tell them
apart at 3am.

## Latency, and what to do about it

Every protected request in your application pays for this call, which is why the cache
exists and why the permission model is a lookup rather than a policy evaluation.

Two things that keep it cheap:

**Ask once per action, not once per rendered element.** A page showing twenty buttons
should not make twenty checks. Decide what the user may do when you load the page, from
the token's claims, and check at the point of the action.

**Do not use it as a role lookup.** It answers one question about one action. Reading the
token's claim tells you the whole set for a project in no time at all, and is the right
tool for deciding what to display.

## `matched_policy`

Under role-based access control the role **is** the policy that matched, so
`matched_policy` is a role key. It is empty on a denial, because nothing matched.

Do not parse it to reconstruct the permission model. It is there to answer "why was this
allowed" in a log, and when attribute-based policies arrive it will carry a policy name
instead.

## What this endpoint is not

It does not tell you what a user *could* do — only whether one specific action is
permitted. It is not a replacement for reading role claims when you need the whole set,
and it is not an audit query: the [audit log](/docs/api-reference) records what happened,
which is a different question from what is allowed.
