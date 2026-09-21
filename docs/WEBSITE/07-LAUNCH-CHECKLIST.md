# 07 - Launch Checklist

> Category: **Public Website** (`docs/WEBSITE/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: P0-19, P5-13 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

What has to be true before the site is a **public** launch rather than a staging preview.
The status is `Partially implemented` because the site is deployed and several items below
are open.

## Scope

`public-site/`, and the decisions a launch forces.

## As Built

### Done

- [x] Every page exists and every internal link resolves — the build fails otherwise.
- [x] Header and footer are shared across marketing and docs (ADR-014), so the surfaces do
      not read as two products.
- [x] Contrast meets WCAG 2.1 AA in both themes, computed from the stylesheet.
- [x] Brand values match the console's, by a check.
- [x] No code is shared with the console, by a check.
- [x] Canonical URLs, the sitemap and `robots.txt` all derive from one `SITE_URL`.
- [x] The API reference is generated, and a stale one fails the build.
- [x] The spec documents no endpoint the service does not serve.
- [x] No verbatim material from the three never-publish documents appears in the output.
- [x] Every landing-page capability carries a phase label, and no label contradicts the
      roadmap board.
- [x] A dated capability audit exists (`public-site/CLAIMS.md`), including a record of the
      audit that missed something.
- [x] A responsible-disclosure path is in the footer.
- [x] A changelog entry exists for each completed phase.

### Open

- [ ] **Phase 4 documentation.** `P4-01`…`P4-06` have shipped and the site says nothing
      about cross-organization delegation. `P4-14` owes it. This is the largest gap.
- [ ] **A performance measurement.** No Lighthouse run, no budget, no Core Web Vitals.
- [ ] **An accessibility pass beyond contrast.** No axe run over the built pages, no
      screen-reader pass.
- [ ] **Structured data and an Open Graph audit.** Neither exists.
- [ ] **A decision about analytics.** There is none, so there is no evidence about which
      pages are read — and adding one is a privacy decision, not a technical one.
- [ ] **Console documentation beyond one page**, for twenty-one screens.
- [ ] **An error-code page** for integrators.
- [ ] **A hosted-offering decision.** The site states plainly that there is none and that
      standing up a deployment needs database access for the first organization and
      administrator (`PG-26`). A public launch either keeps saying that clearly or changes
      it.

### The item that is not a checkbox

Before a launch, `public-site/CLAIMS.md` is re-audited against the board on that day. The
mechanical checks cannot read prose, and the audit is the only thing standing between a
tightened sentence and a claim nobody decided to make.

`P2-15` is the standing reminder: it audited the docs pages but not `CLAIMS.md`, and the
landing status went on saying roles were "not started" for an entire phase.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Launch gated on a fresh claims audit | required | [`02-CONTENT-GOVERNANCE.md`](./02-CONTENT-GOVERNANCE.md) |
| Phase documentation ships with its phase | required | the phase's docs card |
| No claim beyond the shipped phase | standing | `check-claims.mjs` + `CLAIMS.md` |

## Verification

- `CHECK_FULL=1 bash scripts/check.sh` — the full public-site section.
- `public-site/CLAIMS.md` — the audit date against `TASKS/PROGRESS.md`.

## Related Documents

- [`02-CONTENT-GOVERNANCE.md`](./02-CONTENT-GOVERNANCE.md)
- [`04-DOCS-CONTENT-PLAN.md`](./04-DOCS-CONTENT-PLAN.md)
- [`05-SEO-PERFORMANCE-AND-ACCESSIBILITY.md`](./05-SEO-PERFORMANCE-AND-ACCESSIBILITY.md)
