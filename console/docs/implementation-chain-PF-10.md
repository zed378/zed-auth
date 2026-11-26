# PF-10 — Search Input

References: `docs/UI-UX/07` Search Input, `docs/UI-UX/11` Search-as-You-Type, `docs/UI-UX/03` Search. This implements reusable primitives; shell installation and real cross-resource results belong to PF-14.

| Chain step | Implementation |
|---|---|
| Design | Token colors, spacing, typography and focus outline; visible label and global shortcut hint. |
| Component | Controlled `SearchInput` with global/scoped variants; router-aware `ScopedSearchInput`. |
| State | Draft query, IME composition, delayed loading feedback, query-associated result count. |
| Interaction | Debounce 250ms; Ctrl+K / Command+K focuses/selects the mounted global input. Unmount removes shortcut and timers. |
| API dependency | `onSearch(query)` delegates fetching to the consumer; consumer cancels obsolete requests and supplies loading/result state. No endpoint is invented. |
| Loading | Feedback appears after 200ms of continuous loading and resets when query changes. Fast requests show no flash. |
| Error | API errors and retry belong to the surrounding results view; no false count is emitted. |
| Empty | A completed current query announces “0 results”; surrounding results view provides recovery actions. |
| Permission | Component grants no access; caller restricts queried resources and server authorizes every request. |
| Responsive | Full-width input, flexible row and nonshrinking shortcut hint; no animation. |
| Accessibility | Visible label, native searchbox, keyboard shortcut metadata, polite atomic live region; stale counts withheld and IME supported. |
| Test | Debounce/unmount, IME, delayed loading, stale-count suppression, shortcut cleanup, URL restoration/filter preservation, axe. |

Mount one global input in the persistent shell to make its shortcut available on every screen (PF-14 integration). Scoped queries use `q` by default; `queryParam` supports multiple independently scoped searches. URL replacement preserves other parameters without creating one history entry per keypress. `results.query` must identify the request that produced `results.count`.

Verification (2026-11-25): shared console lint, typecheck and production build passed. Initial focused Vitest runs using both forks and threads failed before collecting tests because the worker did not respond within 60 seconds; final runtime results are recorded in `MEMORY/records/2026-11-25-PF-08-10-11-catalog.md`. Persistent shell adoption, browser layout and manual assistive-technology behavior remain acceptance work.
