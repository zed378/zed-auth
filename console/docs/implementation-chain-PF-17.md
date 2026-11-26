# PF-17 Form System Implementation Chain

| Step | Decision |
|---|---|
| Design | Labels stay visible; helper text becomes the field error. Validation waits for blur and submit. |
| Component | `FormSystem`, `useExplicitForm`, `FormSummary`, and `FormReview` provide the shared behavior. Existing `FormField` primitives render the controls. |
| State | Draft values, touched errors, submit errors, async saving, and a live summary are retained in the hook. |
| Interaction | Blur validates one field. Submit reads `FormData` to include browser autofill, validates every field, focuses the first invalid control, and blocks duplicate submits. |
| API Dependency | Server envelopes with `error.details[].field` and `issue` map to matching fields. Project creation and role creation/editing adopt the hook with the existing typed Management API client. Role fields use the API names `key`, `display_name` and `permission_keys`, so validation details reach the correct control. Generated role and permission patterns are reused for client validation. |
| Loading | The save button enters its existing loading state while the request runs; controls are disabled. |
| Error | Field details render on their fields; unmapped details render in the live summary. Draft values remain after failure. |
| Empty | Required fields report their own missing value; review shows `Not provided` for an optional blank. |
| Permission | The API remains authoritative. A denied response is retained as a form-level failure for the owning screen to render with its permission state. |
| Responsive | The components use existing token classes and wrap review values; no fixed-width control is introduced. |
| Accessibility | Summary uses `role=status` and `aria-live`; the first invalid field receives focus. Inputs retain the existing labelled/described-by contract. |
| Test | Workbench fixtures cover blur/submit, server field mapping, draft retention, and multi-step review. `projects.forms.test.tsx` and `roles.forms.test.tsx` keep the generated API client and fetch transport in the path to exercise field envelopes, trimmed payloads, successful reset and network draft recovery. Role regressions additionally cover editable-only PATCH payloads, failed cache invalidation after a committed create, and late completion after scope navigation. Full component and browser suites remain the validation gate. |

`ToggleField` checks its immediate-save mode against `FormSystem` during development. Custom immediate controls must call `useSavePattern("immediate")`; raw HTML controls cannot be automatically classified. `FormSystem` excludes an explicit submit handler in immediate mode, but custom save actions also need the corresponding guard.

Project creation now uses the shared form system. Successful mutations reset the draft and close the modal; failed cache refreshes do not fail an already-committed creation. Drafts are scoped to the active organization, and a late response from the previous organization cannot close a newer organization's dialog. Cancel/reopen within the same organization retains an unsaved draft.

Role creation and editing now use the same form system. Validation waits for blur or submit; server permission errors map to the Permissions textarea, replacing its helper and moving focus there. Save controls validate on submit rather than silently blocking submission. Read-only role keys remain focusable when editing. Closing and reopening a create form or the same role retains its draft; successful saves clear it. Changing organizations or projects remounts the role page, clearing its dialogs and drafts and isolating late mutation completion. Selecting another role starts that role's draft. Cache invalidation failure cannot report an already-committed role save as failed. This remains presentation over the existing API's independent authorization and validation.

`FormSystem.test.tsx` covers blur versus submit, browser autofill, server details and focus, malformed/network failures, retry, wizard back-navigation and the actual ToggleField mixed-pattern guard. Project regression tests exercise an API-backed screen through a mocked fetch response; this is not evidence of a live API validation response. PF-17 remains WIP pending live API acceptance, further screen adoption, and a shared redirect-URI validation definition (the server remains authoritative).

The user invitation flow also uses the shared form system. Its two-step panel retains values through Back, maps server field errors to the email field, and requires explicit acknowledgement when no access is assigned; see [Users invitation chain](./implementation-chain-PF-17-users.md). Existing server authorization and validation remain authoritative.

Common application registration fields now adopt the shared form system; see
[Application registration chain](./implementation-chain-PF-17-applications.md).
Nested SAML registration fields and the SAML edit modal now use the same
field-error and focus behavior; metadata-derived errors return to the document.
Redirect URI and SAML protocol validation continue to use the server as authority.

Project Grant creation also uses the shared form system for partner and role errors,
with retained two-step review, draft recovery and scope isolation; see
[Project Grant form chain](./implementation-chain-PF-17-projectgrants.md).

Organization policies use a typed adapter around `FormSystem` and `FormSummary`,
retaining the whole-settings payload and consequence confirmation. Validation
waits for blur/submit; nested API details map to controls, and confirmation saves
the reviewed snapshot. See [Policy form chain](./implementation-chain-PF-17-policies.md).

Validation on 2026-11-26: the full console component suite passed 631 tests across 56 files, including project, role, user invitation, application/SAML, Project Grant and policy regressions. Lint, typecheck, production build, and 14 Chromium workbench checks pass. A final isolated grid token correction additionally passes both focused component tests and the rebuilt bundle/browser checks. Live API validation, manual accessibility acceptance and remaining page adoption stay pending.
