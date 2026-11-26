# PF-17 — Application registration forms

References: `docs/UI-UX/15-FORM-UX.md`, `docs/UI-UX/13-ACCESSIBILITY.md`, `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` Applications tab, `docs/PLAN/05-API-CONTRACT.md` Standard Error Format.

| Step | Implementation |
|---|---|
| Design | Registration uses explicit saving with persistent labels, blur/submit validation and retained drafts. |
| Component | Registration and SAML settings editing use `FormSystem`, `useExplicitForm`, `FormField`, shared text/choice primitives and `FormSummary`. Metadata/PEM textareas retain monospace text. The SAML editor binds its nested API names to the same form machinery. |
| State | Name, application type, redirect URIs, both SAML source drafts and SAML choices persist through errors and same-project Cancel/reopen. Successful registration resets them. An unfinished SAML edit survives cancellation/reopening for the same application; selecting another application starts its own registration. Organization/project changes remount the owning scope, and management permission loss clears the SAML edit scope. |
| Interaction | Name and active SAML presence fields validate on blur and submit; submit focuses the first invalid field. Submit stays available for incomplete drafts so it can explain missing metadata or manual entity/ACS values. Saving disables controls and dismissal and prevents duplicate submission. Browser autofill is read from the DOM before payload creation. |
| API dependency | Existing application POST; trimmed names and nonempty trimmed redirect lines retain the existing request shape. SAML registration still sends only SAML input; other types send only OIDC redirects. |
| Loading | Register and Save display their loading state. SAML controls, Cancel and dismissal are disabled until the request finishes. |
| Error | API details for common and nested `saml.*` fields map to the corresponding control. Indexed attribute-release fields map to the release choices with one group error; metadata-derived entity/ACS/certificate errors map to the editable document. Other details reach the summary. Network failures retain input and offer retry guidance. |
| Empty | Untouched blank names and incomplete SAML inputs remain submittable so validation can explain the missing value. Only the selected metadata/manual source is required. |
| Permission | Existing page permission handling and independently enforced API authorization remain authoritative. |
| Responsive | Shared field layouts and existing token classes remain fluid. |
| Accessibility | Labelled fields expose error descriptions; submission announces the summary and focuses the first invalid field. |
| Test | `applications.forms.test.tsx` covers common registration behavior and one-time secret delivery. `samlapplications.forms.test.tsx` adds active-source validation, retained metadata/manual drafts, nested API mapping and focus, indexed release errors, autofill payloads, SAML edit draft isolation, pending dismissal/duplicate locks, committed edit success after invalidation rejection, and late completion after project navigation. `samlapplications.test.tsx` retains metadata/manual payload-exclusion, switch-consequence, certificate-status and accessibility coverage. |

Cache invalidation rejection cannot turn committed registration into a failed save or discard its one-time secret. The response still opens the existing secret dialog for confidential clients; public clients receive no empty secret dialog.

SAML settings editing also treats a committed PATCH as successful even if cache invalidation rejects. It sends the whole manual registration, including attribute choices and both switches. Metadata registration still sends only metadata and the shared release/IdP-initiated choices; manual registration sends only individual fields. Switching source never sends the hidden alternative. Protocol formats and certificate parsing remain authoritative on the server; client presence checks preserve the previous minimum-completeness rule. Redirect URI validation also remains authoritative on the server until a shared definition exists. PF-17 remains WIP pending other page adoption and live API/manual accessibility acceptance. Test execution is recorded by the integrating agent.
