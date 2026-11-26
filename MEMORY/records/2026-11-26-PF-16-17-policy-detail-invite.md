# PF-16/17 — Policy, user detail, and invitation continuation

Date: 2026-11-26. Status: WIP; page and manual acceptance remain open.

The user asked to continue implementation in parallel. Three agents owned disjoint
console pages and their focused tests/chains; the root integrated, adjusted one
existing test to match the new form validation boundary, and ran the full console
checks. Existing uncommitted work was preserved. No commit or deployment.

Policies now uses the shared loading announcement and retains last-known settings
and unsaved edits across a transient refresh failure. A permission refusal hides
cached settings and drafts and closes confirmation. User Detail normalizes HTTP
403, announces initial profile loading, shows a recoverable stale-profile banner
on transient failure, and hides cached user details and confirmations on denial.
Their fetch-backed regressions cover refresh/retry and permission behavior.

The two-step user invitation uses `FormSystem` and `useExplicitForm`, retains values
through Back, includes a review summary, maps API field details to the email field,
and requires explicit acknowledgement when no access is assigned. The existing
screen regression was updated to use a format-valid address so it reaches its
mocked server validation response; the test also scopes its assertion to the
visible field helper because the shared form announces the same error to assistive
technology.

Validation: full console `npm run check` passes lint, typecheck, and 584 tests across
49 files. Production build passes; the Chromium workbench passes 13/13 checks.
Documentation citation validation passes across 188 documents and `git diff
--check` passes. No backend/API contract or authorization-policy changes. PF-16,
PF-17, PF-24, and PF-25 remain WIP for further page adoption, live API validation,
end-to-end and manual accessibility acceptance.
