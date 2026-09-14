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

Everything an organization administrator needs for Phases 1 to 3: projects and
applications, users, roles and who holds them, access policies including required
two-step verification, each member's sessions and second factors, the audit log, and
**Your account** for everybody. Granted Projects is Phase 4. Organization settings
(branding) is specified and not scheduled.

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
| Roles and Authorizations | Inside a project: the roles it defines and the permission keys each carries, and which users hold which roles. |
| Users | The list, the detail, and the two-step invitation. A user's detail has a **Sessions** tab, where an administrator can see and revoke where that member is signed in, and a **Multi-factor** tab, read-only except for the reset used when somebody has lost their device and their recovery codes. |
| Policies | Password rules, session lifetime, permitted sign-in methods, and whether a second factor is required — with how many members have none before you switch it on. A change that takes access away asks for confirmation and says who it affects. |
| Audit log | Newest first, filterable by event type, actor and time range, with each event's payload as it was stored. |
| Your account | For every signed-in user, not only administrators, and the one screen built for a phone: profile, password change, second factors and recovery codes, and every place you are signed in. |

Not there yet: Granted Projects (Phase 4), organization settings such as branding (not
scheduled), and instance-wide administration.

The full inventory is specified in
[`docs/UI-UX/08-PAGE-SPECIFICATIONS.md`](https://github.com/zed378/zed-auth/blob/main/docs/UI-UX/08-PAGE-SPECIFICATIONS.md).
