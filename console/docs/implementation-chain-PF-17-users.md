# PF-17 — Users invitation form implementation chain

References: `docs/UI-UX/04-USER-FLOWS.md` Flow 1, `docs/UI-UX/15-FORM-UX.md`, the Users List specification in `docs/UI-UX/18-DETAILED-PAGE-SPECIFICATIONS.md`, and task PF-17.

| Step | Decision |
|---|---|
| Design | Keep Flow 1 as a two-step side-panel flow. Validate the email before moving to access review, and show the invitee and optional display name together before confirmation. |
| Component | The invite panel uses `FormSystem`, `useExplicitForm`, `FormField`, `FormSummary`, and `FormReview`. The shared hook owns values, blur/submit validation, field error mapping, focus, saving state, and draft retention. |
| State | The form hook stays mounted while the step changes, so Back preserves its values. Closing or successfully sending resets the draft. The no-access acknowledgement remains explicit step state and gates the send button with an accessible disabled reason. |
| Interaction | Next validates only the email and advances to review. Back returns to editable fields. The final submit validates again, reads the DOM for autofill, blocks duplicate requests, and keeps the draft on failure. |
| API Dependency | The existing organization users endpoint remains authoritative. Request fields are `email`, optional `display_name`, and `send_invite_email`; server `error.details[].field` values map directly to the matching form field. No endpoint or authorization behavior changed. |
| Error | Client email errors appear beside the email field. Server field details use the same field display and focus path; unmapped server errors remain in the live form summary. |
| Accessibility | Shared labels and descriptions connect helper/error text, the summary announces form status, and the first invalid field receives focus. The access acknowledgement is a labelled checkbox. |
| Test | `users.forms.test.tsx` covers required/format validation and focus, two-step review/back retention, normalized API values, and API field-error mapping with retained drafts. |

Validation: `rtk npm run typecheck`, file-scoped ESLint, and the focused `users.forms.test.tsx` suite passed on 2026-11-26. The repository's broader console checks remain with the integrating agent.
