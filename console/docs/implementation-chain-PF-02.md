# PF-02 — Library scaffolding and component workbench

This slice establishes the public export surface and a development-only preview.
PF-02 remains WIP pending final browser and manual acceptance. All component
families in `docs/UI-UX/07` now have entries, alongside supporting components,
collection states and forms. Component behavior gaps belong to their own tasks;
previews do not imply those tasks are done.

Run `npm run workbench` and open `/workbench.html`. The regular production build
uses `index.html` and excludes this entry. Fixtures make no API requests and do
not require an authenticated session. Import primitives from `src/components`;
keep domain forms in their own modules.

| Chain step | Implementation |
|---|---|
| Design | Existing design tokens and UI-UX/07 usage rules; no new tokens. |
| Component | Public barrel; Button, Table, Form Field, Modal/Side Panel, Badge/Tag, Confirmation, Breadcrumb, Search, supporting components, collection states and forms. |
| State | Catalog records required states and matching render examples. |
| Interaction | Real keyboard focus and pointer hover; accent editor updates only the branded column. |
| API dependency | None: explicit example fixtures only. |
| Loading | Button loading and Table skeleton previews. |
| Error | Table error example; rejected branding reports its validation reason and retains default accent. |
| Empty | Genuine empty and filtered-empty Table examples. |
| Permission | Development entry only; fixtures perform no access-changing actions. |
| Responsive | Columns stack below the existing desktop breakpoint. |
| Accessibility | Headings, explicit labels, unique disabled-reason IDs; use Tab for actual focus-visible states. Branding uses existing WCAG contrast validator. |
| Test | Vitest inventory/completeness/render checks, component axe tests and real ESLint primitive-redeclaration tests run with the console check commands. CI also installs Chromium and runs the standalone development workbench browser suite. Execution belongs to the sole testing agent. |

The page lint guard rejects local declarations using registered primitive names.
It does not classify arbitrary domain-specific components by appearance, and it
does not forbid domain forms or dialogs that compose library primitives. Existing
token rules also apply to the component library and workbench.

When adding an entry, include its usage rule, required states, and a render
example for each state. Tests fail when a registered required state has no example.
Keep missing component families visible in `pendingEntries` until implemented.
