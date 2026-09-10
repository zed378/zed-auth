# 19 — Frontend Implementation Chain

Design documents (`00`–`18` in this folder) describe intent. This document defines the mandatory chain every screen/component must be run through before it's considered implementable, so a developer never has to guess how an abstract design decision becomes actual behavior. This is the frontend-specific counterpart to `PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`.

## The Chain

```
Design → Component → State → Interaction → API Dependency
   → Loading → Error → Empty → Permission → Responsive → Accessibility → Test
```

Every step must be explicitly answered — "not applicable" is an acceptable answer for some steps on some screens, but it must be a stated conclusion, not a silent omission.

## What Each Link Means

| Step | Question to answer | Reference |
|---|---|---|
| **Design** | What does this look like, and why (visual hierarchy, above-the-fold)? | `18-DETAILED-PAGE-SPECIFICATIONS.md`, `06-VISUAL-LANGUAGE.md` |
| **Component** | Which existing components (`07-COMPONENT-SPECIFICATION.md`) does this compose from? Does a new component need to be added to the design system first? | `05-DESIGN-SYSTEM.md`, `07-COMPONENT-SPECIFICATION.md` |
| **State** | What are all the distinct states this screen/component can be in (not just "has data")? | See Loading/Error/Empty/Permission below — these are the state categories, not an exhaustive list on their own |
| **Interaction** | What happens on click/hover/focus/keyboard for every interactive element? | `09-INTERACTION-DESIGN.md`, `11-MICRO-INTERACTIONS.md` |
| **API Dependency** | Which exact endpoint(s) does this call, with which parameters, and what's the exact response shape expected? | `PLAN/05-API-CONTRACT.md` |
| **Loading** | What does this look like while the API dependency above is pending? | `14-EMPTY-LOADING-ERROR-STATES.md` |
| **Error** | What does this look like if the API dependency fails, and specifically how (validation error vs. server error vs. network error — these are different, per `14-EMPTY-LOADING-ERROR-STATES.md`)? | `14-EMPTY-LOADING-ERROR-STATES.md` |
| **Empty** | What does this look like when the API succeeds but returns no data — and is that "genuinely empty" or "filtered to empty" (`14-EMPTY-LOADING-ERROR-STATES.md`)? | `14-EMPTY-LOADING-ERROR-STATES.md` |
| **Permission** | Which role(s)/manager_role(s) (`PLAN/08-AUTHORIZATION.md`) can see or act on this at all? Does the frontend check match exactly what the API enforces (`08-PAGE-SPECIFICATIONS.md`'s "genuinely unreachable, not just hidden" rule)? | `PLAN/08-AUTHORIZATION.md`, `08-PAGE-SPECIFICATIONS.md` |
| **Responsive** | How does this behave at each breakpoint in the grid system (`12-RESPONSIVE-BEHAVIOR.md`)? | `12-RESPONSIVE-BEHAVIOR.md` |
| **Accessibility** | Keyboard reachability, screen-reader labels, contrast, focus behavior — all per `13-ACCESSIBILITY.md`. | `13-ACCESSIBILITY.md` |
| **Test** | Which layer(s) of `PLAN/11-TESTING.md`'s pyramid cover this (component test for validation logic, E2E for the full flow)? | `PLAN/11-TESTING.md` |

## Why This Exists

Without this chain, it's common for a design to specify only the "Design" and "Interaction" columns, leaving Loading/Error/Empty/Permission/Accessibility to be invented ad hoc by whoever implements it — leading to inconsistency across the console (exactly the failure mode `05-DESIGN-SYSTEM.md` and `14-EMPTY-LOADING-ERROR-STATES.md` exist to prevent). Running every component through the full chain up front makes that inconsistency structurally harder to introduce.

## Worked Example: "Revoke Session" Button (Sessions Tab)

| Step | Answer |
|---|---|
| Design | Small `secondary`-styled button per session row, per `18-DETAILED-PAGE-SPECIFICATIONS.md`'s Users List row-action pattern conventions |
| Component | `Button` (secondary variant), inline in a `Table` row, per `07-COMPONENT-SPECIFICATION.md` |
| State | Default, loading (mid-revocation), success (session removed from list), error |
| Interaction | Single click, no modal (low-risk per `09-INTERACTION-DESIGN.md`'s Feedback Timing table); immediate optimistic UI removal with rollback on error |
| API Dependency | `DELETE /v1/organizations/{org_id}/users/{user_id}/sessions/{session_id}`, per `PLAN/05-API-CONTRACT.md` conventions |
| Loading | Button shows spinner, same width, per `07-COMPONENT-SPECIFICATION.md` Button spec |
| Error | Inline error next to the row, session remains in the list (optimistic removal rolled back), retry available |
| Empty | N/A — this is a row action, not a list; the Sessions tab's own empty state (no active sessions) is out of scope for this component's spec |
| Permission | Visible to the session's owning user (self-service) and to `ORG_ADMIN`/`ORG_OWNER` for any user in their org; not visible to other roles |
| Responsive | Identical behavior at all supported breakpoints; button remains a single tap target at mobile width (`16-MOBILE-UX.md`, since Sessions is an in-scope mobile screen) |
| Accessibility | Button labeled "Revoke session on [device/browser]" for screen readers, not just "Revoke" — disambiguates when multiple sessions are listed |
| Test | Component test for the optimistic-update-with-rollback logic; E2E test covering `04-USER-FLOWS.md` Flow 4 end-to-end |

---

This concludes the console-specific documents in `UI-UX/`. Every new component going forward should be run through this chain before being marked ready for implementation. For the public-facing site (landing, docs, about), continue to [20 — Public Site Specifications](./20-PUBLIC-SITE-SPECIFICATIONS.md).
