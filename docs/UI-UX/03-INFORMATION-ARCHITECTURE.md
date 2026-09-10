# 03 — Information Architecture

> **Scope note**: this document covers the **management console**'s IA. The public site (landing, docs, about) has its own, much shallower, SEO/discovery-oriented IA — see `20-PUBLIC-SITE-SPECIFICATIONS.md`. The two are never merged into one navigation, since an anonymous visitor and an authenticated admin have almost nothing in common in terms of what they're looking for.

This mirrors the technical navigation structure in `PLAN/06-FRONTEND-ARCHITECTURE.md`, expanded here with the reasoning behind the grouping and hierarchy decisions, since IA choices are a design concern as much as an engineering one.

## Top-Level Structure

```
┌─ Instance (visible only to INSTANCE_OWNER)
│   ├─ Organizations
│   ├─ Instance-wide policies
│   └─ Instance audit log
│
└─ Organization (the main workspace for most admins)
    ├─ Overview
    ├─ Projects
    │   └─ [Project detail]
    │       ├─ Applications
    │       ├─ Roles
    │       ├─ Authorizations
    │       └─ Project Grants
    ├─ Users
    │   └─ [User detail]: Profile, Grants, Sessions, MFA
    ├─ Granted Projects
    ├─ Policies (access + ABAC)
    ├─ Audit Log
    └─ Settings
```

## Why This Grouping (Not an Alternative)

- **Instance is separated at the top, not nested inside an organization**, because it's structurally different — most admins will never see it, and nesting it would suggest it's "one organization among others," which is wrong (`PLAN/04-DATA-MODEL.md`).
- **Projects sit above Users in the primary nav**, even though most day-to-day admin work (per `02-USER-JOURNEYS.md` Journey 1) starts from Users. This is intentional: Projects/Roles are the more stable, less-frequently-changed structure, while Users is where volume and turnover happen. Putting Users as its own top-level item (not nested under Projects) reflects that a user's identity is independent of any one project, matching the data model (`PLAN/04-DATA-MODEL.md`: `users` belongs to `organizations`, not `projects`).
- **Granted Projects is a separate top-level item from Projects**, not a tab within it, because for a receiving organization (persona Reza, `01-USER-PERSONAS.md`) these are conceptually "projects other people own that we have limited access to" — visually and structurally distinct from "projects we own."
- **Policies groups both access policies (password/MFA/session) and ABAC policies** under one nav item with sub-tabs, rather than two separate top-level items, because both are fundamentally "rules that govern access for this organization" from an admin's mental model, even though they're technically different subsystems (`PLAN/08-AUTHORIZATION.md`).

## Depth Rule

No screen should require more than **3 levels of navigation** to reach from the organization workspace root (Organization → Projects → [Project] → tab). Anything requiring deeper nesting is a signal to reconsider the IA rather than add a fourth level — this keeps the console fast to navigate for admins who use it dozens of times a day (`00-DESIGN-DIRECTION.md`).

## Breadcrumb & Wayfinding

Every nested screen shows a breadcrumb reflecting the actual data hierarchy (e.g. `Acme Org / POS Project / Roles`), which doubles as navigation — clicking any breadcrumb segment jumps directly there. This is especially important for Project Owners (Sari) and Vendor Admins (Reza) who may work across the "Projects" vs. "Granted Projects" distinction and need to always know unambiguously whose project they're currently looking at.

## Search

A global search (accessible from anywhere in the console) resolves across organizations (if the admin has multi-org visibility), projects, and users by name/email/id — necessary once an organization has more than a handful of users, to avoid forcing admins through the full nav tree for a lookup they do dozens of times a day.

Continue to [04 — User Flows](./04-USER-FLOWS.md).
