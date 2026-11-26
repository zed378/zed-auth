# PF-18 responsive grid preparation

PF-18 depends on PF-13. This bounded foundation slice prepares shared components and fixtures; shell/page adoption and the complete responsive audit remain gated.

| Step | Decision |
|---|---|
| Design | `docs/UI-UX/12` defines 4 / 8 / 12 / 12 columns at mobile / tablet / desktop / wide widths. |
| Component | `ResponsiveGrid` defines the grid; `GridRegion` requires an explicit span for all four breakpoints. |
| State | CSS responds to viewport width; no resize listeners or hydration-dependent branch is introduced. |
| Interaction | Source/tab order stays stable when regions stack. `min-w-0` and wrapping prevent long content from pushing tracks wider. |
| API Dependency | None. |
| Loading | The component preserves the same tracks for loading content supplied by the caller. |
| Error | Invalid runtime spans fail explicitly rather than silently create overflow columns. |
| Empty | Empty regions retain their declared occupancy; callers own empty-state content. |
| Permission | No permission or authorization decision is made by this layout. |
| Responsive | 768 / 1024 / 1440px use existing breakpoint tokens. Reduced gutters use existing `spacing-4` (16px); wide full gutters use `spacing-5` (24px). These sizes are implementation choices on the existing token scale; the reference specifies relative gutter sizes. |
| Accessibility | Layout preserves content order and does not use CSS reordering; the existing admin boundary hides the unsupported content subtree from display/focus/assistive technology. |
| Test | Component tests cover explicit occupancy and invalid spans; the workbench fixture supports browser checks for computed tracks, gutters, spans, and boundary behavior. |

The 768px admin boundary remains authoritative: `docs/UI-UX/16` explicitly excludes admin screens below tablet width, while the roughly 600px sample in `docs/UI-UX/12` is a required testing width. Personal account settings use `AccountShell` outside that boundary; hosted authentication is a separate surface. No new 600px breakpoint is introduced.

The fixture's primary/supporting spans are examples only. No page occupancy, Rego screen audit, actual device acceptance, or full PF-18 completion is claimed.

Validation on 2026-11-26: the responsive entry is registered in the workbench catalog and its required-family contract. All four Chromium viewport checks pass for track counts, gutter sizes and both region spans; the 767/768px boundary check passes. The complete workbench suite passes 13 tests. Browser assertions inspect grid-column start/end independently of shorthand serialization. PF-18 remains gated preparation until navigation and screen adoption are complete.

Follow-up verification found that `wide:gap-6` used 32px on the project's custom
spacing scale. The grid now uses `wide:gap-5`, matching the documented 24px
full gutter and the browser assertion. Both grid component tests and all 14
current Chromium workbench checks pass after the correction.
