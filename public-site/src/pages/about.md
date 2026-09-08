---
title: About
description: Why Zed Auth exists, and the principles it is built on.
---

# About Zed Auth

Zed Auth exists because identity infrastructure should not be the hardest part of
building a new service. It is built API-first and standards-based, designed to grow
from a single internal tool into a multi-tenant platform without a rewrite.

## Why this exists

Teams building more than one service end up with the same problem three times: a login
form per application, a user table per application, and permission logic that drifts
apart until nobody can answer "who can do what" without reading code.

The usual answers are to buy a product that assumes a shape your organization does not
have, or to build one and discover that the hard parts — token rotation, session
revocation, delegating access to a partner organization without handing over
control — are all the parts you deferred.

## Design principles

These are the principles the implementation is actually held to, not aspirations.

**Standards over invention.** OIDC and OAuth 2.1, not a proprietary protocol. A
consumer application should be able to integrate using any conformant library rather
than one we publish.

**API-first, with no exceptions.** Every capability in the management console is
available through the same public REST API. If the console can do something the API
cannot, that is a defect rather than a feature of the console.

**Authorization is enforced by the server, every time.** The console hides controls a
user cannot use, because showing them is bad design — but hiding a button is never the
control. The API checks on every request, and cross-tenant isolation is a property of
the database rather than of the code that queries it.

**Decisions are written down, including the wrong ones.** The engineering plan, the
threat model, and an architecture decision record for every significant choice live in
the repository. Several of those records document something that was built, found to be
wrong, and changed — that history is more useful than a clean one.

## Status

In development. The service runs and is deployed; the authentication and authorization
endpoints are being built. The [changelog](/changelog) records what has actually
shipped, and the [concepts documentation](/docs/concepts/model) describes the model the
implementation is working toward.

Source, plan and decision records: [github.com/zed378/zed-auth](https://github.com/zed378/zed-auth).

## Get in touch

Questions, integration plans, or a security issue to report — see [contact](/contact).
