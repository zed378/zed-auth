# PF-17 — Project Grant form adoption

References: `docs/UI-UX/15-FORM-UX.md`, `docs/UI-UX/04-USER-FLOWS.md` Flow 2, `docs/UI-UX/13-ACCESSIBILITY.md`, `docs/PLAN/05-API-CONTRACT.md` standard error format.

| Step | Implementation |
|---|---|
| Design | Two steps retain the existing consequence preview, named shared and withheld roles, and partner organization ID privacy boundary. |
| Component | `FormSystem`, `useExplicitForm`, `TextField` and `FormSummary` own explicit saving and partner validation; the existing checkbox group remains. |
| State | Blur and Review validate; typing does not. Back and Cancel preserve drafts. A successful save clears the draft. Organization or project navigation remounts the scope and clears its dialogs. |
| Interaction | Review validates before showing the full summary. Final confirmation submits once while controls and dismissal are blocked. A server failure returns to editable fields. |
| API dependency | Existing create-grant endpoint and `granted_org_id` / `role_keys` payload. The handler reports role validation details as `role_keys`; `granted_role_keys` is the response field. Server authorization and subset validation remain authoritative. |
| Loading | Existing role loading and failure guidance remain; pending roles cannot pass Review. |
| Error | Standard server details map to partner or role group; invalid controls receive focus with a live summary. Network failures retain drafts. Cache refresh failure cannot report a committed creation as failed. |
| Empty | A project without roles cannot create a grant; an untouched form reports required choices on Review. |
| Permission | Existing collection denial hides creation and cached contents. No API policy changes. |
| Responsive | Shared token-based field styling and existing wrapping checkbox/summary layout. |
| Accessibility | Persistent step text, visible labels, group legend and error description, first-invalid focus, live error summary. |
| Test | `projectgrants.forms.test.tsx` covers validation timing, field detail mapping/focus, retained drafts, invalidation failure after commit, and project navigation with late completion. Existing grant tests retain consequence and role-subset assertions. |

PF-17 remains WIP. Focused Project Grant suites pass 27/27; follow-up integration passes lint/typecheck, all 631 console tests across 56 files, production build and 14 Chromium workbench checks. Live API and manual accessibility acceptance remain pending.
