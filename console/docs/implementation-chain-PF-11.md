# PF-11 — Supporting components

Implements `TASKS/PHASE-F-FRONTEND-IMPLEMENTATION.md` PF-11 against `docs/UI-UX/07`, `09`, `10`, `11`, `13`, `14`, `15` and `18`.

| Chain step | Implementation |
|---|---|
| Design | Existing surface, border, typography, accent, success and spacing tokens; static KPI cards have no interactive styling. |
| Component | `Supporting.tsx`: Tabs, StepIndicator, Toast/InlineSuccess, CopyButton, KpiCard, PaginationFooter, InlineErrorBanner. |
| State | URL-backed tabs; controlled progress; paused/dismissed toast; copy pending/success/failure; static/linked KPI; paging/first/last; error/retrying. |
| Interaction | Tabs support arrow/Home/End keys, skip disabled options and retain mounted panel drafts. Progress only offers earlier steps. Copy awaits the clipboard operation. |
| API dependency | No endpoints added. Pass real KPI values, server cursors and the failed query's refetch callback. The flow owns draft state, validation, final summary and URL step state; the workbench demonstrates details/review/back retention. |
| Loading | Copy disables itself pending completion; pagination blocks concurrent requests; retry button exposes busy state. |
| Error | Clipboard rejection gets explicit manual recovery and retry; inline query failure supplies targeted retry. No copied value appears in feedback. |
| Empty | No cursors disables navigation; zero count is explicit. Unknown/disabled tab query falls back to first enabled tab. |
| Permission | Components confer no authority. Callers supply only API-permitted actions and independently enforce route/API permissions. |
| Responsive | Tabs and cards retain token geometry; step and pagination controls wrap. |
| Accessibility | Roving tab focus, linked tab/panel semantics, labelled progress and pagination, current-step state, success live regions, explicit clipboard errors. Toast defaults to ten seconds and pauses while hovered or focused; duration zero keeps it persistent. |
| Test | Component regressions cover tab URL/history and draft retention, progress, failed/stale copy, paused toast, empty-string and opaque cursors, static KPI, targeted retry and axe. |

These are shared primitives and workbench examples; existing page adoption remains in the corresponding page tasks. Step linkability is deliberately controlled by each form owner because navigation must respect that form's validation and draft lifecycle. Toasts suit routine reversible feedback; access changes use persistent explicit success.
