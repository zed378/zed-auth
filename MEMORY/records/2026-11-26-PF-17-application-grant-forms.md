# PF-17 — Application and Project Grant forms

Date: 2026-11-26. Status: implementation slice verified; PF-17 remains WIP.

The user requested parallel implementation. Two agents own application registration
and Project Grant creation respectively; one dedicated agent executes verification.
The root integrates and records the result. Existing uncommitted work is preserved.

Scope follows `docs/UI-UX/15-FORM-UX.md`, the standard error format in
`docs/PLAN/05-API-CONTRACT.md`, and the existing PF-17 task. The changes adopt the
existing shared explicit form system over shipped Management API operations.
Backend authorization and validation remain authoritative.

Application registration now uses shared fields for name, type and redirect URIs.
Errors map to those fields with focus and draft retention; failed cache refreshes
cannot hide an issued secret or report a committed registration as failed.
Pending registration disables controls and dismissal. Six fetch-backed regressions
cover validation, retry/reset, draft retention, nested SAML summary, secret delivery
and late completion after project navigation. The SAML metadata/manual controls and
SAML settings edit modal were outside this initial slice; the
[SAML/policy continuation](./2026-11-26-PF-17-saml-policy-forms.md) subsequently
adds their field-level adoption.

Project Grant creation now validates the partner organization and selected roles
through the shared form hook, keeps the two-step review with a visible step label,
and retains drafts through Back, Cancel/reopen and request failures. API field
errors return to the editable step and focus the invalid control. Organization and
project changes remount the scope; saving prevents dismissal and duplicate saves.
Committed creation closes successfully even if cache invalidation rejects.

Follow-up integration verification on 2026-11-26 passes lint/typecheck and all
631 console tests across 56 files, production build and 14 Chromium workbench
checks. The focused Project Grant suites pass 27/27 after confirmation-stage
queries were scoped to the dialog, distinguishing the same-named page action.
See the SAML/policy continuation record for the intermediate failures and recovery.
No commit or deployment is part of this continuation. PF-17 remains WIP pending
remaining screen adoption, live API field-error acceptance and manual accessibility.
