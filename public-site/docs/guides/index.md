---
id: guides-index
title: Guides
description: Task-based how-tos for Zed Auth.
sidebar_position: 1
---

# Guides

Task-based how-tos: "set up SSO for your app", "create a Project Grant", "rotate a
signing key".

:::note[Start with the quickstart]

Setting up SSO for an application — the first guide anyone wants — is the
[quickstart](/docs/quickstart), end to end, with the security-relevant parameters
explained where they appear.

The rest of this section is thin on purpose. A guide describes a sequence of real API
calls, and most of the sequences below need capabilities that are not built. Publishing
them now would mean publishing instructions that cannot be followed.

:::

## Available

| Guide | Where |
|---|---|
| Set up SSO for your application | [Quickstart](/docs/quickstart) |
| Define roles and assign them | [Define roles](/docs/guides/define-roles) |
| Validate role claims in your service | [Validate role claims](/docs/guides/validate-role-claims) |
| Use `/v1/authz/check` for real-time decisions | [Authorization checks](/docs/guides/authorization-checks) |

## Planned

| Guide | Arrives with |
|---|---|
| Provision an organization from CI | Needs the bootstrap path in `PG-26` |
| Rotate a signing key without downtime | Phase 3 |
| Delegate a project to a partner organization | Phase 4 |
| Add an attribute-based policy | Phase 4b |
