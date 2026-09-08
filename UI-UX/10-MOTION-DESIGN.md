# 10 — Motion Design

Motion in this console is used **purely functionally** — to communicate state change and maintain spatial continuity — never decoratively. This follows directly from `00-DESIGN-DIRECTION.md`'s "clarity over decoration" principle.

## Principles

1. **Motion clarifies cause and effect.** Every animation should help the admin understand what just happened or what's about to happen, not exist for visual polish alone.
2. **Fast by default.** Since this console is used dozens of times a day by the same admins, durations lean toward the fast end of typical UI motion ranges — nothing should feel like it's making a repeat user wait to "enjoy" an animation.
3. **Respect reduced-motion preferences.** Every animation below must have a reduced/no-motion fallback (instant state change instead) honoring the OS-level `prefers-reduced-motion` setting — required, not optional, per `13-ACCESSIBILITY.md`.

## Specific Motion Patterns

| Interaction | Motion | Duration guidance | Purpose |
|---|---|---|---|
| Side panel open/close (e.g. Invite User flow) | Slide in from the right / slide out | Short | Reinforces that the panel is a temporary layer over the current screen, not a full navigation |
| Modal open/close | Fade + slight scale | Short | Distinguishes modals from side panels per `07-COMPONENT-SPECIFICATION.md`'s two-variant distinction |
| Table row appearing/removed (e.g. after inviting a user, after revoking a grant) | Brief highlight/fade on the affected row | Short | Draws attention to exactly what changed as a direct result of the admin's action, without requiring them to re-scan the whole table |
| Step transition in multi-step flows (`04-USER-FLOWS.md` Flow 1/2/5) | Horizontal slide matching the step indicator's direction | Short | Reinforces forward/backward progress through the flow |
| Loading skeleton → loaded content | Instant swap, no cross-fade | N/A | Avoids adding perceived latency; per `14-EMPTY-LOADING-ERROR-STATES.md`, skeletons should disappear the instant data is ready |
| Toast/inline success confirmation (e.g. "Invite sent") | Fade + slight slide in, auto-dismiss after a few seconds | Short, auto-dismiss timing generous enough to read | Confirms an action completed without blocking further work |

## What Never Gets Motion

- Destructive confirmation dialogs (`07-COMPONENT-SPECIFICATION.md`) appear instantly, no entrance animation — the goal there is to interrupt attention immediately, not ease into view.
- Data within tables never animates on sort/filter — re-sorting a table of hundreds of users with animated row reordering would be both slow and disorienting; it updates instantly.

## Ownership

Motion values (durations, easing curves) should be defined as design tokens alongside `05-DESIGN-SYSTEM.md`'s color/spacing tokens once implementation begins, so they stay centrally adjustable rather than hard-coded per component.

Continue to [11 — Micro-interactions](./11-MICRO-INTERACTIONS.md).
