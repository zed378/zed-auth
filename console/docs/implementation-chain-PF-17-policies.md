# PF-17 — Policy form validation and recovery

References: `docs/UI-UX/15-FORM-UX.md`, `docs/UI-UX/13-ACCESSIBILITY.md`,
`docs/UI-UX/08-PAGE-SPECIFICATIONS.md` Policies — Access tab,
`docs/PLAN/05-API-CONTRACT.md` Standard Error Format, and the existing
[P2-14 implementation chain](./implementation-chain-P2-14.md).

| Step | Implementation |
|---|---|
| Design | Keep the four existing policy sections, explicit save behavior, generated bounds/defaults and access-reduction confirmation. Validation waits for blur and submit. |
| Component | Shared `FormSystem` and `FormSummary` wrap the existing typed policy controls. A local adapter preserves optional numbers, booleans and the whole settings document; the string-valued shared form hook is not used for this document. |
| State | Field errors, form summary and drafts stay with one organization. The keyed scope remounts on organization changes, preventing late mutation responses from changing another organization's draft or feedback. Existing permission-denial handling still suppresses cached policies and drafts. |
| Interaction | Blur validates the affected control. Submit reads numeric DOM values to include browser autofill, validates every field and focuses the first invalid control in visual order. An invalid changed form remains submittable so the summary can explain what to fix. |
| API dependency | Existing `PATCH /v1/organizations/{org_id}` receives the complete typed settings document. No validation bound, authorization rule or policy enforcement changed. API details matching local field names, nested password-policy paths or `settings.*` paths map to their controls. Unknown details remain in the summary. |
| Loading | Pending saves disable controls, Discard, confirmation dismissal and duplicate submission. Access-reducing changes retain a fixed submitted snapshot, shown in the existing consequence dialog before the request. |
| Error | Field errors replace helper text and receive focus after controls unlock and confirmation closes. Network failures give retry guidance and retain the draft. A rejected cache invalidation does not report an already-committed save as failed. |
| Empty | Blank number fields retain the existing service-default behavior. Password expiry of zero remains valid. At least one sign-in method remains required. |
| Permission | Server authorization is authoritative. This change is form presentation over the shipped API and does not add an endpoint or permission. |
| Responsive | Existing form width, field groups and token classes are preserved. |
| Accessibility | Field names identify error targets; labels, `aria-invalid` and error descriptions remain connected. The shared live summary announces errors; the first invalid control receives focus. |
| Test | `policies.forms.test.tsx` exercises blur/submit timing, untouched DOM values, confirmation-before-write, nested numeric/boolean/group field errors, draft recovery, pending locks, rejected invalidation and late response isolation through the generated client and mocked fetch. Existing policy tests retain consequence and accessibility coverage with updated blur/submit expectations. |

PF-17 remains WIP. Fetch-backed component tests do not constitute live API or
manual screen-reader acceptance. Execution results are recorded by the integrating
verification agent.
