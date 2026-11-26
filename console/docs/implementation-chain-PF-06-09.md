# PF-06 and PF-09 implementation chain

References: docs/UI-UX/07 Modal / Side Panel and Breadcrumb; 09 Keyboard & Focus; 10 motion; 12 tablet takeover; 03 wayfinding; 13 accessibility; 19 implementation chain.

| Step | PF-06 Modal / Side Panel | PF-09 Breadcrumb |
|---|---|---|
| Design | Focused modal versus detail-heavy side panel. | Real organization, project and section hierarchy. |
| Component | Modal, SidePanel, shared useDialog hook. | Breadcrumb and route-derived ConsoleBreadcrumb. |
| State | Open, closed, acknowledgement required; nested ownership. | Current segment, owned projects, granted projects, long names. |
| Interaction | Close control, Escape, Tab containment and trigger restoration. | Every segment links to its route; final link has aria-current. |
| API dependency | No requests; existing callers supply actions and content. | Active names supplied by callers; destinations derived from current route. |
| Loading | Parent flow supplies content; no invented network state. | Caller supplies resolved labels; ID fallback preserves identity. |
| Error | Destructive confirmation in a panel is rejected. | Unknown routes produce no misleading trail. |
| Empty | Closed dialogs mount no overlay. | Empty item lists produce no navigation landmark. |
| Permission | No authorization changes; callers and server retain their checks. | Navigation links confer no permission. |
| Responsive | Full takeover below 1024px; scrollable content and fixed footer. | Wrapping and visual truncation preserve full accessible labels. |
| Accessibility | Unique names, focus containment, nested Escape ownership, reduced motion. | Named navigation, ordered list, current page, full link names. |
| Test | Component regressions, axe, and Chromium geometry at four widths. | Navigation, route updates, unknown routes, axe; page adoption remains pending. |

Both remain REVIEW. PF-02 is still missing catalog entries; PF-09 is not yet adopted across all screens. Manual assistive-technology sign-off remains pending.
