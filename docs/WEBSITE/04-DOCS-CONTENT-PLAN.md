# 04 - Documentation Content Plan

> Category: **Public Website** (`docs/WEBSITE/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: P1-25, P2-15, P3-13, P4-14 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

What integrator documentation exists, how a page earns its place, and what is owed. The
status is `Partially implemented` because `P4-14` — the delegation, SAML and social-login
guides — has not been written.

## Scope

`public-site/docs/`.

## As Built

### Three kinds of page, and the test each must pass

| Kind | Answers | Test before writing one |
|---|---|---|
| **Concept** | "How should I think about this?" | Would an integrator who skipped it write code that works and models the domain wrongly? |
| **Guide** | "How do I accomplish X?" | Is X a task somebody actually has, phrased the way they would phrase it? |
| **Reference** | "What exactly do I send?" | — it is generated |

### What exists

**Concepts** — `model` (organizations, projects, applications, roles, grants), `authorization`,
`sessions`.

**Guides**, eight of them, each titled as the task rather than the feature:

| Page | Title |
|---|---|
| `define-roles.md` | Define roles and assign them |
| `validate-role-claims.md` | Validate role claims in your service |
| `authorization-checks.md` | Use `/v1/authz/check` for real-time decisions |
| `require-mfa.md` | Require two-step verification for your organization |
| `two-step-verification.md` | Set up two-step verification, and get back in without your phone |
| `step-up-with-amr.md` | Require a stronger sign-in for sensitive actions |
| `refresh-token-rotation.md` | Handle refresh token rotation without logging your users out |

Note the phrasing. *"Handle refresh token rotation without logging your users out"* names
the failure the reader is trying to avoid; *"Refresh tokens"* would name a feature and help
nobody.

Plus `quickstart.md`, the docs home, and one page on the console.

### A guide documents behaviour the service actually has, and the numbers are pinned

`authorization-checks.md` publishes the delegated-role revocation window: **30 seconds**
backstop, re-derived from the grant on every check, and — stated explicitly — delegated
roles do **not** appear in access tokens yet.

That last sentence is the kind a guide usually omits, and omitting it would leave an
integrator building on a claim the service does not make.

The number is not prose. `backend/internal/docsdrift/` reads the published sentence and the
constant and fails if they disagree, so changing `DefaultTTL` without changing the guide
breaks the build.

### A guide is written when the feature ships, in the same phase

`P1-25`, `P2-15` and `P3-13` each wrote the documentation for their phase. `P4-14` owes the
same for Phase 4.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Guide titles name the task | convention | the eight titles above |
| Published numbers are pinned to constants | required where one exists | `backend/internal/docsdrift/` |
| Documentation ships with its phase | required | the phase's docs card |
| No claim beyond the shipped phase | standing | [`02-CONTENT-GOVERNANCE.md`](./02-CONTENT-GOVERNANCE.md) |

## Verification

- `backend/internal/docsdrift/` — the published revocation window matches the code.
- `public-site/scripts/check-claims.mjs` — no page claims an unshipped capability.
- `npm run build` in `public-site/` — every internal link resolves.

## Not Yet Built / Open Questions

- **`P4-14` is not started**: no guide for cross-organization delegation, SAML or social
  login. Phase 4 has shipped `P4-01` through `P4-06` and the public documentation says
  nothing about any of it — the largest current gap on this surface.
- **`/docs/console/` is one page** for twenty-one screens.
- **No troubleshooting or error-code page.** An integrator who gets a `409` from a
  delegated assignment has the status code and no prose about what to do.
- **No migration guide**, because there is nothing to migrate from yet.

## Related Documents

- [`01-INFORMATION-ARCHITECTURE.md`](./01-INFORMATION-ARCHITECTURE.md)
- [`03-GENERATED-API-REFERENCE.md`](./03-GENERATED-API-REFERENCE.md)
- [`../DEVELOPER/`](../DEVELOPER/)
