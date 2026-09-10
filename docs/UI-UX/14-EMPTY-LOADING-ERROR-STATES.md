# 14 — Empty, Loading & Error States

Every screen listed in `08-PAGE-SPECIFICATIONS.md` must define all three states below before being considered spec-complete — a page spec that only describes the "happy path with data" is incomplete.

## Empty States

Every list/table screen (Organizations, Projects, Users, Project Grants, Audit Log, etc.) needs a distinct empty state — not just a bare table with a header row and nothing below it.

**Anatomy**: a short, factual message describing what's missing (not a generic "No data"), and, where a clear next action exists, a primary button to take it (e.g. Users list empty state: "No users yet" + "Invite your first user" button).

**Rule**: distinguish **genuinely empty** ("no projects exist yet") from **filtered-to-empty** ("no users match your search") — the latter needs a "clear filters" action instead of a create action, since offering to "create a user" when the real issue is an overly narrow search is actively unhelpful.

## Loading States

Per `07-COMPONENT-SPECIFICATION.md`'s Table entry and `10-MOTION-DESIGN.md`: use skeleton placeholders shaped like the eventual content (skeleton rows matching table row height), not a full-screen blocking spinner — this preserves layout stability and lets the admin see the page structure immediately.

**Rule**: for actions (not initial page load) — e.g. submitting the Invite User flow (`04-USER-FLOWS.md` Flow 1) — the triggering button shows its own loading state (`07-COMPONENT-SPECIFICATION.md` Button spec: spinner replaces label, same width) rather than blocking the entire screen, so the admin retains context.

## Error States

Three distinct error scenarios, each handled differently:

| Scenario | Treatment |
|---|---|
| A list/table fails to load | Inline error banner above the table area; if stale cached data exists, show it with a "showing last-known data, refresh to retry" note rather than an empty error screen |
| A form submission fails (validation error from the API) | Errors mapped back to the specific fields per `05-API-CONTRACT.md`'s error format (`details[].field`), shown inline per `15-FORM-UX.md` — never just a generic top-of-form error banner when field-level detail is available |
| A form submission fails (server/network error, not validation) | Top-of-form banner with a factual message and a retry action; per `09-INTERACTION-DESIGN.md`'s Error Recovery principle, the user's already-entered input must be preserved |

**Rule, consistent with `00-DESIGN-DIRECTION.md`'s tone guidance**: error copy states what happened and what to do next, in plain language, without blaming the user or using vague technical jargon ("Something went wrong" alone is insufficient — pair it with a next step: "Something went wrong saving this role. Try again, or contact support if this keeps happening.").

## State Priority When Multiple Could Apply

Loading takes priority over empty (don't flash an empty state before data has actually loaded); error takes priority over both loading and empty once a request has definitively failed. A screen should never show "No results" while a request is still in flight.

Continue to [15 — Form UX](./15-FORM-UX.md).
