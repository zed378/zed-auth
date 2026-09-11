---
id: console-index
title: Using the console
description: How to work with the Zed Auth management console.
sidebar_position: 1
---

# Using the console

The management console is the web interface for everything Zed Auth manages:
organizations, projects, applications, users, roles and grants.

:::note[What exists today]

Sign-in, the organization overview, projects, applications, users and the audit log.
Roles and grants are not there because they are not built — Phases 2 and 4.

Screens are built alongside the API endpoints they use, and never ahead of them: a
screen with no endpoint behind it is a screen that has to lie about something.

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

| Screen | What it is for |
|---|---|
| Sign in | The same Authorization Code + PKCE flow every other application uses. The token is held in memory for the tab and never in `localStorage`. |
| Organization overview | Whether anything needs attention, in five seconds: active users, projects, pending invitations, and recent activity. |
| Projects | Projects and, inside one, its applications — registering a client, rotating its secret, editing its redirect URIs. |
| Users | The list, the detail, and the two-step invitation. Step two is where access is considered, and "no access yet" is an explicit choice rather than a skipped step. |
| Audit log | Newest first, filterable by event type, actor and time range, with each event's payload as it was stored. |

Not there yet: roles and grants (Phases 2 and 4), sessions per user (the service revokes
them today; the screen is what is missing), and instance-wide administration.

The full inventory is specified in
[`docs/UI-UX/08-PAGE-SPECIFICATIONS.md`](https://github.com/zed378/zed-auth/blob/main/docs/UI-UX/08-PAGE-SPECIFICATIONS.md).
