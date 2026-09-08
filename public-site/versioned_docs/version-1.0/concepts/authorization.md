---
id: authorization
title: Authorization
description: How Zed Auth decides whether an action is allowed — roles, delegation, and policies.
sidebar_position: 2
---

# Authorization

Zed Auth answers one question on every request: **is this subject allowed to perform
this action on this resource?**

There are three layers to the answer. They stack rather than compete — each handles a
case the previous one cannot, and most systems never need the third.

This describes the design. See the [roadmap](https://github.com/zed378/zed-auth/blob/main/PLAN/16-IMPLEMENTATION-ROADMAP.md)
for which phase delivers each layer.

## Layer 1 — Roles

A user is granted a role in a project. The role carries permissions. The check is a
lookup: does this user hold a role in this project that includes this permission?

This covers the overwhelming majority of real cases, and it is fast — a property that
matters because every protected request in every consumer application pays for it.

*Arrives in Phase 2.*

## Layer 2 — Project grants (delegation)

The case roles alone cannot express: **letting another organization manage access to
your project, without letting them manage all of it.**

A supplier needs their staff to use your procurement system. You do not want to
administer their people — they join, leave and change roles without telling you. But you
also cannot give them free rein to grant any role they like.

A project grant delegates the project to their organization along with an explicit list
of roles they may assign. Their administrator then manages their own team, and can only
ever assign roles from that list.

Two properties make this trustworthy rather than merely convenient:

**The allowed set is checked on every request, not only when a grant is created.** If
you narrow what a partner may delegate, grants that already exist are constrained
immediately. Checking only at creation time would make revocation advisory — the
existing grants would keep working and nobody would notice until an audit.

**Delegation does not transit.** An organization that receives a delegated project
cannot delegate it onward. Otherwise the set of people who can reach your project would
grow without any action by you, which is the opposite of what delegation is for.

*Arrives in Phase 4.*

## Layer 3 — Attribute-based policies

For rules that depend on context rather than identity: an approval limit that varies by
amount, access restricted to a department, an action allowed only during business hours.

These are expressed as policies evaluated against attributes of the subject, the
resource and the request. They layer *over* roles rather than replacing them — the role
says you are an approver, the policy says whether you may approve this particular
request.

This layer is deliberately optional and deliberately last. Attribute-based access
control is more expressive than roles and considerably harder to reason about; a system
that starts there ends up with rules nobody can predict the behaviour of. Zed Auth only
adds it when there is a concrete requirement roles genuinely cannot express.

*Arrives in Phase 4b, and only if a real requirement justifies it.*

## What is always true

**The server decides.** The console hides controls a user cannot use, because showing
them is bad design. It is never the control. The API performs its own check on every
request regardless of what the interface displayed, because the interface is not in the
trust boundary — anyone can call the API directly.

**Every decision that changes access is recorded.** Granting a role, revoking one,
creating or narrowing a delegation: each writes an audit event in the same transaction
as the change itself. If the audit write fails, the change fails with it. That is the
intended trade — refusing to act is better than acting unrecorded, because an audit log
with silent gaps cannot be known to have them.

**Denial is the default.** A permission that has not been granted is not held. There is
no implicit inheritance that would make it necessary to reason about what a role does
*not* carry.
