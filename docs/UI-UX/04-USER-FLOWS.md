# 04 — User Flows

Detailed, screen-by-screen flows for the journeys introduced in `02-USER-JOURNEYS.md`. Each flow lists the screens involved, decision points, and the specific UX safeguards required.

## Flow 1: Invite User + Assign First Role (Org Admin)

```
Users list
  → Click "Invite user"
    → Modal/panel: enter email + display name
    → Same panel, step 2: pick Project → pick Role (multi-select, at least one required OR explicit "no access yet" option)
    → Confirm → invite sent
  → User appears in Users list with status "invited"
  → (later) status auto-updates to "active" once the invitee completes signup
```

**Safeguards**: 
- Steps for entering info and picking role must be **one flow**, not two separate screens (per `02-USER-JOURNEYS.md` Journey 1) — a progress indicator (Step 1 of 2) keeps it from feeling like a single overloaded form.
- "No access yet" must be an explicit, visible choice, not just "skip this step" — so an admin never accidentally believes they granted access when they didn't.

## Flow 2: Create and Revoke a Project Grant (Project Owner)

```
Project detail → Project Grants tab
  → "Create Grant"
    → Select target organization (search/select)
    → Select roles to share (multi-select; every project role is shown and selectable,
      unselected roles are explicitly "not shared" rather than hidden)
    → Confirmation step: plain-language summary
      ("Organization X will be able to assign: [role list] to their own users")
    → Confirm → grant created, status "active"

Revoke:
  → Open existing grant → "Revoke"
    → Confirmation step: consequence summary
      ("This will remove access for N users currently holding a role through this grant")
    → Confirm (may require re-typing the organization name for high-impact grants)
    → Grant status → "revoked", dependent user_grants invalidated immediately
```

**Safeguards**: this is the highest-stakes flow in the console (`02-USER-JOURNEYS.md` Journey 2) — both creation and revocation require an explicit consequence-preview step before the final confirm, per `09-INTERACTION-DESIGN.md`.

## Flow 3: Vendor Admin Self-Manages Access (Restricted Grant Context)

```
Granted Projects list
  → Select a granted project
    → Shows: allowed roles only (roles not granted are not shown at all, not shown-disabled)
    → "Assign user" → pick own org's user → pick from allowed roles → confirm
```

**Safeguards**: never show a role the receiving org isn't allowed to assign, even in a disabled state — showing it at all (even disabled) invites confusion about whether it's "coming soon" vs. "not for you" (`02-USER-JOURNEYS.md` Journey 3).

## Flow 4: End-User Self-Service — Revoke a Lost-Device Session

```
Personal account settings → Sessions tab
  → List of active sessions (device/browser, approximate location from IP, last active time)
  → "Revoke" on the relevant session → immediate confirmation, no further modal needed
    (low-risk, reversible-by-re-login action — doesn't need the heavy confirmation
    pattern used for Project Grants)
  → Session disappears from the list; underlying session is invalidated server-side
    within the same request (not eventually-consistent)
```

**Safeguards**: this must feel and be **instantaneous** — a delay here, during a moment the user is specifically worried about their account security, undermines trust in the whole system (`02-USER-JOURNEYS.md` Journey 4).

## Flow 5: Author and Activate an ABAC Policy (Phase 4b)

```
Policies (ABAC) → "New policy"
  → Rego editor (syntax highlighting, inline validation errors)
  → "Run dry-run" → shows a diff-style comparison:
      requests that would be allowed/denied differently vs. the currently active policy
  → Review diff → "Activate" (only enabled once at least one dry-run has been run)
    → Confirmation: "This becomes version N, replacing version N-1. You can roll back anytime."
  → Policy status → "active"

Rollback:
  → Policy detail → version history → "Roll back to version N-1" → single-click, immediate
```

**Safeguards**: "Activate" is disabled until a dry-run has actually been performed at least once — the UI should make skipping this step require a conscious extra action, not be the path of least resistance (`02-USER-JOURNEYS.md` Journey 5, `PLAN/08-AUTHORIZATION.md` Part D).

## Cross-Flow Principle: Consequence Before Confirmation

Every flow above that can **remove or grant access** follows the same shape: **preview the consequence in plain language → then ask for confirmation**, never the reverse. This is the single most important interaction pattern in the whole console, formalized further in `09-INTERACTION-DESIGN.md`.

Continue to [05 — Design System](./05-DESIGN-SYSTEM.md).
