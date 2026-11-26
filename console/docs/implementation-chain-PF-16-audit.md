# PF-16 — Audit log collection states

References: `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` (Audit Log and cross-screen requirements), `docs/UI-UX/14-EMPTY-LOADING-ERROR-STATES.md`, and PF-16.

| Step | Implementation |
|---|---|
| Design | Keeps the existing event filters, cursor pagination, actor wording, timestamps and server-redacted payload view. Uses the shared factual state messages and recovery controls. |
| Component | `AuditLogPage` wraps its table and expanded event detail in `CollectionStateBoundary`; `Table` supplies matching loading skeletons. |
| State | Central priority suppresses empty content while pending. Definitive failure takes precedence; permission denial suppresses cached rows, expanded payload, and pagination. |
| Interaction | Retry invokes only `events.refetch()` with the current organization, filters and cursor. No navigation, reload or scroll call occurs. Pagination remains available after an empty or failed later page so Previous can recover without changing filters. |
| API Dependency | Existing generated `GET /v1/organizations/{org_id}/events` client and query key. Existing `asFailure` normalizes API envelopes; HTTP 403 is explicitly treated as denial. No endpoint or authorization rule changes. |
| Loading | Announced busy region with event-table skeleton; neither initial nor filtered empty appears before the response. |
| Error | Server/network failure offers a request-local retry. Cached rows and an expanded payload remain mounted on a failed refresh. Native fetch `TypeError` uses connection recovery copy. |
| Empty | Initial empty offers no create action: audit events are produced by service activity. Filtered empty offers Clear the search, clearing event type, both dates and the cursor stack. |
| Permission | The generic access guidance exposes neither cached event fields nor server error detail. API authorization remains authoritative. |
| Responsive | Retains existing wrapping filters, secondary table columns and horizontally scrollable payload. No breakpoint or screen-scope changes. |
| Accessibility | Loading is announced; request/access failures use the shared alert. Existing labelled date/type filters and expanded-detail controls remain. Retry preserves mounted table/detail and does not reset scrolling. |
| Test | `auditlog.states.test.tsx` covers deferred loading, filter recovery and date query bounds, stale table/detail retention and request-local retry, cached-content denial, and retry/back-navigation with an active cursor and filter. Existing audit tests in `screens.test.tsx` continue to cover actor, UTC and payload behavior. |

Validation on 2026-11-26: all five new audit regressions passed within the full console suite (516 tests); production build and lint pass. This is bounded page adoption, not full PF-16 completion. No live API or manual accessibility acceptance is claimed. Export, actor-filter enhancements and other later-phase screen work remain outside this change.
