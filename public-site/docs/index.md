---
id: docs-home
title: Documentation
description: Integrate Zed Auth, from your first login flow to delegating access across organizations.
sidebar_position: 1
slug: /
---

# Documentation

Everything you need to integrate, from your first login flow to delegating access
across organizations.

:::info[Where the project is]

Phases 1 and 2 are built: single sign-on over OIDC and OAuth 2.1, the management API, the
hosted login and password pages, the audit log, per-project roles and grants, live
authorization checks, and more than one organization. The
[quickstart](/docs/quickstart) was executed end to end against a running deployment —
every command on it is a command that ran.

Phase 3 is built too: two-step verification with authenticator apps and passkeys,
recovery codes, refresh token rotation, session management and sign-in anomaly detection.
Its acceptance checks were executed against the running deployment, and its guides
describe what the code does today.

Project Grants, social sign-in and SAML arrive in Phase 4; attribute-based policies in
Phase 4b. Pages describing those say so rather than describing something you can call
today. The [roadmap board](https://github.com/zed378/zed-auth/blob/main/TASKS/PROGRESS.md)
is what the project actually works from.

:::

## Start here

**[Concepts](/docs/concepts/model)** — the model Zed Auth is built on: instances,
organizations, projects, applications, users, roles and grants. Read it before the
quickstart if you want the reasoning; read it after if you want to get something
working first.

**[Quickstart](/docs/quickstart)** — the shortest path to a working login flow: register
an application, run Authorization Code with PKCE, and verify the token locally without
calling back here.

**[API reference](/docs/api-reference)** — generated from the OpenAPI specification, so
it cannot describe an endpoint the service does not serve.

## How this documentation is organized

| Section | What it answers |
|---|---|
| [Concepts](/docs/concepts/model) | What are the pieces and how do they fit together? |
| [Guides](/docs/guides) | How do I accomplish a specific task? |
| [API reference](/docs/api-reference) | What exactly does this endpoint accept and return? |
| [Console](/docs/console) | How do I do this through the management interface? |

Search covers all of them. If you do not know the term this project uses for something,
search for the term you would use — the index covers page content, not just titles.
