# PF-16 — Applications, Authorizations and user security states

Date: 2026-11-26. Status: WIP; parent acceptance remains open.

The user requested parallel continuation. Three agents owned separate console
pages and their regression/implementation-chain files; root integrated and
validated the result. Existing uncommitted work was preserved.

References: `docs/UI-UX/14-EMPTY-LOADING-ERROR-STATES.md`,
`docs/UI-UX/09-INTERACTION-DESIGN.md` (Error Recovery),
`docs/UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md`, and task PF-16.

Applications now keeps stale rows mounted during refresh failures and suppresses
cached project/application information and registration, SAML and issued-secret
dialogs after permission refusal. Its local drafts remount per organization and
project. Authorizations applies the same recovery to user search, selected-user
grants and role choices, with request-local retry. Denial from any of its four
read dependencies suppresses resource context and open dialogs.

Sessions and MFA retain stale tables and meaningful metadata on refresh failure,
hide cache, actions and confirmations on denial, and reset local state when
scope changes. Initial testing exposed an eager member read while AuthProvider
was still restoring claims. MFA now waits for claims and Sessions enables its
query only once claims exist, so self views choose only their self endpoint.
The parent UserDetailPage header/detail query is outside this bounded slice.

Added 29 fetch-backed regressions: Applications 8, Authorizations 9, user
security 12. They exercise the generated client against response fixtures;
they do not establish live API or manual screen-reader acceptance. The initial
user-security run had one failure, which exposed the eager request above; its
12-case focused rerun passes after the fix. Typecheck also caught an unsupported
Testing Library role-query option in a new test; it was removed.

Validation: lint, typecheck/production build, all 572 component tests across
46 files and the Chromium workbench suite (13/13) pass. Documentation check
passes across 188 documents, and diff
whitespace checks pass. No backend/API contract or authorization policy changes;
no commit or deployment. PF-16 remains WIP for other screens and live/manual
acceptance. Individual chain tables live under `console/docs/`.
