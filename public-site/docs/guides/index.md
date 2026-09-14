---
id: guides-index
title: Guides
description: Task-based how-tos for Zed Auth.
sidebar_position: 1
---

# Guides

Task-based how-tos: "set up SSO for your app", "require a stronger sign-in for a
sensitive action", "require two-step verification for everyone".

:::note[Start with the quickstart]

Setting up SSO for an application — the first guide anyone wants — is the
[quickstart](/docs/quickstart), end to end, with the security-relevant parameters
explained where they appear.

A guide describes a sequence of real API calls. The planned ones below need capabilities
that are not built, and publishing them now would mean publishing instructions that
cannot be followed.

:::

## Available

### Integrating an application

| Guide | Where |
|---|---|
| Set up SSO for your application | [Quickstart](/docs/quickstart) |
| Validate role claims in your service | [Validate role claims](/docs/guides/validate-role-claims) |
| Use `/v1/authz/check` for real-time decisions | [Authorization checks](/docs/guides/authorization-checks) |
| Require a stronger sign-in for sensitive actions | [Step-up with `amr`](/docs/guides/step-up-with-amr) |
| Handle refresh token rotation correctly | [Refresh token rotation](/docs/guides/refresh-token-rotation) |

### Administering an organization

| Guide | Where |
|---|---|
| Define roles and assign them | [Define roles](/docs/guides/define-roles) |
| Require two-step verification | [Require MFA](/docs/guides/require-mfa) |

### For the people signing in

| Guide | Where |
|---|---|
| Set up two-step verification, and recover from a lost device | [Two-step verification](/docs/guides/two-step-verification) |

## Planned

| Guide | Arrives with |
|---|---|
| Provision an organization from CI | Needs the bootstrap path in `PG-26` |
| Rotate a signing key without downtime | Rotation works today through the operator tool; the procedure is in [`deploy/SECRETS.md`](https://github.com/zed378/zed-auth/blob/main/deploy/SECRETS.md#rotation-runbooks). A public guide is not written yet |
| Sign in with Google or Microsoft | Phase 4 |
| Delegate a project to a partner organization | Phase 4 |
| Add an attribute-based policy | Phase 4b |
