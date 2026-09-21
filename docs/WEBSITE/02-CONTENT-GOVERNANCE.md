# 02 - Content Governance

> Category: **Public Website** (`docs/WEBSITE/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-19, P1-25, P2-15, P3-13 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

The rule that governs every word on the public site, the two checks that enforce its
mechanical half, and the honest statement of what those checks cannot do.

## Scope

`public-site/CLAIMS.md`, `public-site/scripts/check-claims.mjs`,
`public-site/scripts/check-no-internal-leak.mjs`.

## As Built

### The rule

> Marketing and documentation copy never describes a capability that is not actually
> shipped in the current roadmap phase.

`docs/UI-UX/21` § Content Governance, `CLAUDE.md`, `AGENTS.md` rule 8. It is a standing
rule, not a launch task: a read confirms it today, and the rule has to hold in six months
when somebody tightens a sentence and a hedge disappears — which is how a roadmap item
becomes a claim without anybody deciding to make one.

### `check-claims.mjs` — two mechanical properties

1. **Every capability described on the landing page carries a phase label.** A capability
   card without one reads as available.
2. **No label contradicts the roadmap board.** Nothing labelled with a future phase is a
   task `TASKS/PROGRESS.md` already marks `DONE`, and nothing described as available is a
   task that is not.

The second direction is the one this project actually got wrong: `P2-15` audited the docs
pages but not `CLAIMS.md`, which is how the landing status went on saying roles were "not
started" for a whole phase. **A stale label is the same lie as a missing one**, and the
check now fails for either.

It runs against the **built** site, because that is what a visitor sees — a check over the
source would miss anything a component interpolates.

### What it cannot check, stated in the script itself

> What it cannot check is prose. "Zed Auth provides SSO" in a paragraph will pass this and
> be wrong. That is what `CLAIMS.md` and human review are for, and saying so plainly here
> is better than implying the script is the whole control.

### `public-site/CLAIMS.md` — the part a person keeps true

A table of every claim on every published page, its status (`Positioning`, `Shipped —
Phase N`, `Planned — Phase N`), and the basis for it. Dated on every audit, with the
history of previous audits kept, including the one that missed something.

### `check-no-internal-leak.mjs` — verbatim phrases, not keywords

Three documents must never be published: `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`,
`docs/PLAN/14-DEPLOYMENT.md`, `docs/PLAN/18-RISK-REGISTER.md`.

The check looks for **runs of eight or more words** appearing in both an internal document
and the built site. The reasoning is worth keeping:

> Keywords produce false positives on words a public site legitimately uses —
> "authorization", "session", "token" — and a check that cries wolf gets disabled, which
> this project has already watched happen once with a PEM scanner.

A run of eight words appearing in both places is not a coincidence; it is a paste, which is
the realistic failure mode.

### The status sentence is part of the design, not a disclaimer

The landing page's positioning line is immediately followed by what is actually shipped.
That adjacency is the site's editorial stance, and removing it to "tighten" the hero is
exactly the change the governance rule exists to catch.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| No claim beyond the shipped phase | standing | `check-claims.mjs` + `CLAIMS.md` |
| Every capability labelled | required | `check-claims.mjs` |
| A stale label fails | required | `check-claims.mjs` |
| Three internal documents never published | verbatim-phrase check | `check-no-internal-leak.mjs` |
| Claims audit | dated, with history | `public-site/CLAIMS.md` |
| Checks run against the build | required | both scripts |

## Verification

- `npm run check` in `public-site/` — `check:claims` and `check:leak` run after the build.
- `scripts/check.sh` — the public-site section, with the slow parts behind `CHECK_FULL=1`.

## Not Yet Built / Open Questions

- **Prose is unchecked.** By construction. `CLAIMS.md` is the compensating control and it
  is only as current as its last audit.
- **The audit is manual and periodic**, tied to a phase card rather than to every content
  change.

## Related Documents

- [`00-SITE-PURPOSE-AND-AUDIENCE.md`](./00-SITE-PURPOSE-AND-AUDIENCE.md)
- [`07-LAUNCH-CHECKLIST.md`](./07-LAUNCH-CHECKLIST.md)
- [`../UI-UX/21-CONTENT-AND-COPY-STRATEGY.md`](../UI-UX/21-CONTENT-AND-COPY-STRATEGY.md)
