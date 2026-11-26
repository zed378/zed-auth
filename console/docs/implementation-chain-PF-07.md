# PF-07 — Badge / Tag

References: `docs/UI-UX/07-COMPONENT-SPECIFICATION.md` § Badge / Tag, `docs/UI-UX/06-VISUAL-LANGUAGE.md` § Status Encoding and Role-Source Visual Treatment, and `docs/UI-UX/13-ACCESSIBILITY.md` § Color & Contrast.

| Step | Implementation |
|---|---|
| Design | Compact token-based labels; active uses a filled dot, invited and deactivated outline dots, revoked a slash. Locked uses warning and draft neutral. |
| Component | `Badge`, `StatusBadge`, `Tag`, `RoleSourceBadge`; workbench entry `badgeEntry`. |
| State | Six statuses; generic tones; direct/delegated role sources; source disclosure closed/open; metadata tag. |
| Interaction | Delegated source appears through the hover title and through tap or keyboard activation; direct roles and metadata are non-interactive. |
| API dependency | None; callers supply the server's status, role key and source organization. |
| Loading | No fetching; badges render only supplied data, without inventing an interim status. |
| Error | Empty labels and empty delegated origins are rejected; API error handling belongs to the containing screen. |
| Empty | The collection decides its empty state; a badge never substitutes a blank status or source. |
| Permission | Presentation only; authorization is enforced by the containing route and API. |
| Responsive | Inline layout wraps in the containing collection; source disclosure works without hover. |
| Accessibility | Visible text is required, marks are decorative, disclosure is keyboard operable and reports expanded state. Actual token contrasts are checked against both backgrounds; allowed branding changes only accent. |
| Test | Dedicated status, blank-label, disclosure, contrast and axe tests; existing component and authorization screen tests remain passing. Manual browser and assistive-technology verification remain review work. |
