# PF-08 — Confirmation Dialog

References: `docs/UI-UX/07` § Confirmation Dialog, `docs/UI-UX/09` § Consequence Before Confirmation and Feedback Timing, `docs/UI-UX/04`, `docs/UI-UX/10`.

| Chain step | Implementation |
|---|---|
| Design | Destructive action verb, required consequence, secondary Cancel; full modal interruption. |
| Component | `ConfirmDialog` composes shared `Modal` and `Button`. |
| State | Default, exact typed gate, consequence loading/error, mutation busy, explicit success acknowledgement. |
| Interaction | Type the exact resource name; Cancel resets typing on close. Pending mutations block duplicate submission and dismissal. Done acknowledges completed actions. |
| API dependency | Caller supplies current consequence, lookup loading/error, retry callback, mutation busy state and success text after verified completion. The primitive performs no requests. |
| Loading | Hide cached consequence content while `consequenceLoading` is true; use a skeleton and announce lookup progress. Never enable confirmation before the current consequence is available. |
| Error | Hide untrusted consequence content; explain failure, block confirmation and retry only the lookup through the supplied callback. Preserve typed input. |
| Empty | Null/undefined/boolean/empty-string consequences are rejected; zero affected users is a valid explicit consequence. |
| Permission | Caller and server enforce authorization; the dialog grants no permission. |
| Responsive | Inherits viewport-constrained, scrollable Modal geometry. |
| Accessibility | Unique typed-field IDs, visible disabled explanations, status/error announcements, shared focus trap and restoration. No entrance motion. |
| Test | Component regressions cover exact matching, reopen reset, stale impact suppression, failed lookup/retry, duplicate action and dismissal blocking, success acknowledgement, unique labels and axe. Existing dialog tests cover the no-motion contract. |

Typed confirmation is reserved for Project Grant revocation with dependents and organization deletion. `OK` and `Confirm` are rejected as action labels. Pages that require asynchronous consequences must pass `consequenceLoading` for every refresh (including background refreshes), or `consequenceError` after failure. They must keep the dialog open and supply `success` after high-risk completion; existing page adoption is tracked separately.

Workbench counts are explicit demonstration fixtures, not production estimates. Real mutations remain in their existing caller/server paths.
