# 09 — Interaction Design

Formalizes the interaction patterns implied throughout `04-USER-FLOWS.md` and `07-COMPONENT-SPECIFICATION.md` into explicit, reusable rules.

## The "Consequence Before Confirmation" Pattern

The single governing interaction rule for this console, first introduced in `04-USER-FLOWS.md`:

```
1. User initiates an access-changing action (grant, revoke, delete, deactivate).
2. System computes and displays the actual consequence in plain language
   BEFORE asking for final confirmation — never a generic "Are you sure?"
3. Confirmation button is labeled with the specific verb ("Revoke", "Delete", "Deactivate"),
   never "OK" or "Confirm".
4. For the highest-impact subset (Project Grant revocation with dependents,
   organization deletion), require typing the resource name to enable the confirm button.
```

This pattern applies to every action listed in `04-USER-FLOWS.md` and must be used identically everywhere it applies — an admin should be able to predict this pattern's shape after encountering it once.

## Concrete Interaction Rules (Apply Console-Wide)

These are non-negotiable baseline rules every screen must follow, regardless of what's specified per-page in `08-PAGE-SPECIFICATIONS.md`/`18-DETAILED-PAGE-SPECIFICATIONS.md`:

- **A card or row is only clickable if it has an actual detail destination.** Never make a summary card (e.g. a KPI card with no drill-down) look clickable (hover/cursor affordance) if clicking it does nothing.
- **Hover state is never the sole indicator that something is interactive.** Since hover doesn't exist on touch/tablet (`12-RESPONSIVE-BEHAVIOR.md`), every interactive element needs a persistent visual cue (icon, underline, button styling) that doesn't depend on a mouse being present.
- **Loading uses skeletons that preserve the final layout's geometry** (`14-EMPTY-LOADING-ERROR-STATES.md`) — content must not visibly jump/reflow once real data replaces the skeleton.
- **Error states offer retry without a full page reload.** A failed data fetch (`14-EMPTY-LOADING-ERROR-STATES.md`) always provides an inline retry action that re-attempts just the failed request, never forcing the admin to lose their current scroll position/context via a full reload.
- **Destructive action friction scales with risk level**, not a single fixed confirmation pattern for everything — see the risk-tiered table in "Feedback Timing" above and the typed-confirmation escalation for the highest-impact actions.

## Progressive Disclosure Rules

- Advanced/rare concepts (Project Grants, ABAC policies) are not shown in primary navigation until an organization has actually created at least one of them, OR the admin has an applicable manager role for it — avoids overwhelming a first-time Org Admin (Budi, `01-USER-PERSONAS.md`) with concepts they may never need.
- Multi-step flows (Invite User, Create Project Grant) show a step indicator so the user always knows how much remains, per `04-USER-FLOWS.md` Flow 1/2.

## Feedback Timing

| Action type | Expected feedback |
|---|---|
| Low-risk, reversible (revoke own session, `04-USER-FLOWS.md` Flow 4) | Immediate, no confirmation dialog, instant visual update |
| Medium-risk (deactivate a user, edit a role) | Lightweight inline confirmation, immediate visual update |
| High-risk (revoke a Project Grant, delete an organization) | Full consequence-before-confirmation pattern (see above), with an explicit success state after completion, not just a silently updated list |

## Keyboard & Focus Behavior

- All primary actions (create, invite, save) reachable via keyboard without a mouse, since admins performing many repetitive actions per day benefit disproportionately from keyboard efficiency.
- Opening a modal/side panel moves focus into it immediately; closing it returns focus to the triggering element — standard focus-trap behavior, detailed further in `13-ACCESSIBILITY.md`.

## Undo vs. Confirm

- Prefer **confirm-before** (the pattern above) over **undo-after** for anything touching access control, since a brief window of incorrect access (even if later undone) may already have been exploited — this is a deliberate deviation from the "undo is friendlier than confirm" convention common in consumer apps, justified by the security context (`PLAN/09-SECURITY.md`).
- **Undo-after** is acceptable only for purely cosmetic/non-security actions (e.g. reordering a personal dashboard widget, if such a feature exists) — none of which are currently in scope per `PLAN/01-PRODUCT-SCOPE.md`.

## Error Recovery

When an action fails partway (e.g. network error during a multi-step invite flow), the UI must preserve the user's input and clearly indicate which step failed — never silently discard entered data, since re-entering a role assignment from scratch is exactly the kind of friction that pushes admins toward risky shortcuts (like over-granting "just to be safe"). Full state specification in `14-EMPTY-LOADING-ERROR-STATES.md`.

Continue to [10 — Motion Design](./10-MOTION-DESIGN.md).
