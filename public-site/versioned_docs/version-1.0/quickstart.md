---
id: quickstart
title: Quickstart
description: The shortest path to a working login flow against Zed Auth.
sidebar_position: 2
---

# Quickstart

:::danger[This guide does not exist yet]

There is no working login flow to walk you through. Zed Auth is in Phase 0: the
service runs and serves operational probes, and the OIDC endpoints a quickstart would
use are built in Phase 1.

This page is a placeholder so that the route exists and so nobody follows a guide that
cannot work. `P1-24` replaces it with the real thing.

:::

## What it will cover

When Phase 1 ships, this guide will get a working login flow running in about five
minutes. You will register an application, redirect a user through login, and receive a
verified identity token.

The steps will be:

1. **Register an application** — create a project and an application of type `spa` or
   `web`, and set its redirect URI.
2. **Redirect the user to authorize** — an Authorization Code request with PKCE.
3. **Exchange the code for tokens** — receive an ID token and an access token.
4. **Verify the ID token** — validate the signature against the published JWKS, and
   check the issuer, audience and expiry.
5. **Call the API with the access token** — make one authenticated request and read
   the result.

## What has to ship first

| Requirement | Roadmap task |
|---|---|
| The OIDC provider and its discovery document | `P1-03`, `P1-06` |
| `POST /oauth/token` with PKCE verification | `P1-07` |
| Published JWKS with key rotation | `P1-08` |
| Application registration through the Management API | `P1-15` |

Progress is tracked in
[`TASKS/PROGRESS.md`](https://github.com/zed378/zed-auth/blob/main/TASKS/PROGRESS.md).

## In the meantime

Read the [concepts](/docs/concepts/model) — they describe the model the quickstart will
assume, and they are accurate today because they document the design rather than an
endpoint.
