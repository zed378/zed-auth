# 04 - Public Site Architecture

> Category: **Architecture** (`docs/ARCHITECTURE/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-18, P0-19, P1-25 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describe the public marketing and documentation site: what it contains, how the API reference is produced, and the rules that stop it describing things the service cannot do.

## Scope

`public-site/`. The console is `03-FRONTEND-ARCHITECTURE.md`; copy strategy is `docs/UI-UX/21-CONTENT-AND-COPY-STRATEGY.md`.

## As Built

- A statically generated site (Docusaurus-based) deployed separately from the console, on its own origin.
- **The API reference is generated from the contract**, never written by hand: `openapi/openapi.yaml` → `public-site/docs/api-reference/`. Regenerating and diffing is a CI gate, so an endpoint change that has not been re-published fails the build.
- Guides and concept pages are hand-written, and constrained by a **capability audit**: the built site is scanned for claims about capabilities that have not shipped, against `public-site/CLAIMS.md`. The audit and a link check run in CI (`scripts/check.sh`, and `CHECK_FULL=1` locally).
- A drift test in the backend (`backend/internal/docsdrift/`) asserts that specific published statements still match the implementation — for example the sign-in methods described and the phase status line.
- Shares no code with the console; a CI gate asserts it.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| API reference | Generated; staleness fails CI | `public-site/` generation step in `scripts/check.sh` |
| Capability claims | Only shipped capabilities | capability audit + `public-site/CLAIMS.md` |
| Published statements about behaviour | Pinned by tests | `backend/internal/docsdrift/phase3_docs_test.go` |
| Shared code with the console | None | dedicated gate |
| Accessibility and contrast | Checked for both themes | `scripts/check.sh` public-site section |

## Interfaces

- Reads `openapi/openapi.yaml` at build time.
- Deployed as static files behind nginx (`deploy/public-site`), reachable at the project's public documentation hostname.

## Security Considerations

- The site is unauthenticated and must never embed tokens, internal hostnames or environment values.
- Because it is the public promise of the product, an untrue capability claim is a security-relevant defect: it invites integrators to rely on a control that does not exist. That is why the audit is a gate rather than a review habit (`docs/UI-UX/21` § Content Governance).

## Verification

- `scripts/check.sh` (full mode) — build, capability audit, link check, contrast.
- `backend/internal/docsdrift/` — published statements versus implementation.

## Not Yet Built / Open Questions

- Pages for SAML, social login and webhooks must not appear until those features ship; the audit is what keeps that true.

## Related Documents

- `docs/PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`, `docs/UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md`, `docs/UI-UX/21-CONTENT-AND-COPY-STRATEGY.md`.
