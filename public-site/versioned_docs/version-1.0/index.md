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

:::warning[In development]

Zed Auth is in Phase 0 of its [roadmap](https://github.com/zed378/zed-auth/blob/main/docs/PLAN/16-IMPLEMENTATION-ROADMAP.md).
The service runs and is deployed, and the endpoints it currently serves are the
operational probes in the [API reference](/docs/api-reference).

The authentication and authorization endpoints are being built. Documentation for them
describes the design and says which phase delivers it, rather than describing something
you can call today.

:::

## Start here

**[Concepts](/docs/concepts/model)** — the model Zed Auth is built on: instances,
organizations, projects, applications, users, roles and grants. Safe to read now,
because it describes the design rather than a shipped endpoint, and it is what makes
the rest of the documentation make sense.

**[Quickstart](/docs/quickstart)** — the shortest path to a working login flow. Not
available yet; the page says what it will cover and what has to ship first.

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
