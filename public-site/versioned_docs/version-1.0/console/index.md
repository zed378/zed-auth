---
id: console-index
title: Using the console
description: How to work with the Zed Auth management console.
sidebar_position: 1
---

# Using the console

The management console is the web interface for everything Zed Auth manages:
organizations, projects, applications, users, roles and grants.

:::warning[The console shell exists; its screens do not]

The console currently renders its navigation and nothing behind it. Its screens are
built alongside the API endpoints they use, from Phase 1 onward.

:::

## What the console is

An ordinary OIDC client. It logs in through the same Authorization Code + PKCE flow any
other application uses, and it calls the same public REST API. There is no private
interface behind it.

That is a deliberate constraint rather than an implementation detail. It means:

- **Anything you can do in the console, you can automate.** There is no capability
  reachable only by clicking.
- **The permissions you see are the permissions you have.** The console hides controls
  your access token does not carry the role for — and the API enforces the same check
  independently, so a hidden button is never the thing protecting an action.
- **The team uses the login flow it ships.** If the OIDC flow breaks, the people who
  can fix it find out first.

## Screens

Documented as they are built. The full inventory is specified in
[`docs/UI-UX/08-PAGE-SPECIFICATIONS.md`](https://github.com/zed378/zed-auth/blob/main/docs/UI-UX/08-PAGE-SPECIFICATIONS.md).
