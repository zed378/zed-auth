# Site Content, and Auditing a Rule Instead of Reading It

**Date**: 2026-09-08
**Task**: `P0-19`
**Branch**: `feat/P0-19-site-content`

---

## What Was Left

Most of `P0-19`'s steps landed with `P0-18`, because a skeleton with no words in it is not a skeleton of anything: the About page, `/contact` with its disclosure channel, the three concepts pages, the quickstart placeholder, and the deliberate absence of `/security` were all written then.

What remained were the two things that are not content: the landing page's conformance to `UI-UX/20`'s detailed spec, and the capability audit.

---

## Two Gaps Against the Spec

**No primary CTA in the navigation.** `UI-UX/20` § Above-the-fold lists it alongside the logo and the Docs and About links. Added.

**Three labels for one action.** The hero said "Read the docs", the closing section said "Read the concepts". `UI-UX/20` § Interaction asks for "exactly **one** primary CTA style used consistently across the whole page", and its landing spec says the final section restates the primary action rather than introducing a new one. Three different labels is the five-competing-CTAs failure that document warns about, in slower motion — each one individually reasonable.

Now three instances of "Read the docs", all pointing at `/docs`, all the same weight. Verified in the browser rather than by reading the source.

---

## The Audit, and Why It Is Not a Read

`P0-19`'s Definition of Done asks that "a capability audit confirms every claim on every published page maps to something either shipped or explicitly labelled as planned".

A read confirms it today. The rule it is confirming — `UI-UX/21` § Content Governance, repeated in `CLAUDE.md` — has to hold in six months, when somebody tightens a sentence and a hedge disappears. That is how a roadmap item becomes a claim without anyone deciding to make one, and it is invisible in review because the diff looks like an improvement.

So the audit is three things rather than one.

**`CLAIMS.md`** — the document the DoD asks for. Every claim on every page, what it maps to, and whether it is shipped, planned-and-labelled, positioning, or a principle. It says plainly what it cannot do and when to re-run it.

**`check-claims.mjs`** — every capability on the landing page carries a phase label, and no label contradicts the roadmap board. The second half matters more than it looks: a card labelled "Phase 4" whose task is now `DONE` is exactly as inaccurate as an unlabelled one, and it is the version nobody notices, because the label is *there*.

**`check-no-internal-leak.mjs`** — `PLAN/20` § What Never Gets Published names three documents that must never reach the public site. This scans the built pages for **verbatim phrases** from them, eight words or longer.

The phrase approach is the point. A keyword check on a site that legitimately discusses authorization, sessions and tokens produces constant false positives, and a check that cries wolf gets disabled — this project watched that happen days ago with a PEM scanner that flagged `node_modules`. An eight-word run appearing in both an internal document and a public page is not a coincidence; it is a paste, which is the realistic failure. Nobody writing a security page from memory reproduces a sentence.

A short denylist covers what is dangerous even paraphrased: private IP addresses, risk-register identifiers, deviation identifiers.

All three checks were verified by breaking them: a sentence from `SECURITY/02` pasted into `/about`, a private IP on `/contact`, and a capability card stripped of its phase label. Each was caught, named, and located.

---

## What the Landing Page Says, and Does Not

`UI-UX/21`'s own blueprint writes four capabilities in the present tense — "Log in once, access every registered application", "Everything the console can do, your scripts and CI/CD can do too". None of the four exists. The project is in Phase 0: the service builds, deploys, isolates tenants in the database, writes an audit log, and serves two probes.

Copied as written, the site would have claimed four things that are not true, from a document whose own governance section forbids exactly that. The blueprint is a structural template with placeholder copy — it says so — and reading it as ready-to-ship text was the trap.

They are on the page as design, each labelled with the phase that delivers it, under a heading saying none is finished, below a status statement above the fold.

Two smaller calls followed from the same rule. **No social-proof section**, because `UI-UX/20` says include it "only once genuinely available" and an empty one "is worse than omitting it entirely". And **the hero CTA points at the docs**, not at a quickstart, because a "Get Started" button leading to a page that opens "This guide does not exist yet" costs more trust than the click is worth.

---

## Verified

| Check | Result |
|---|---|
| Capability audit | 4 claims, each labelled, none contradicting the board |
| Internal leak scan | 35 pages against 3,240 phrases from 3 documents — clean |
| Audits catch violations | Pasted phrase, private IP, missing label — all caught |
| Primary CTA consistency | 3 instances, one label, one destination |
| Hero above the fold at 1440×900 | Bottom at 740px |
| `scripts/check.sh` | 33 gates |

---

## Outstanding

- **`OQ-10` is still open.** `P0-18`'s DoD asks for Lighthouse scores against a bar `UI-UX/20` never sets. Not resolved here, because inventing a threshold to satisfy a checkbox is the same failure as inventing a capability to fill a section.
- **Analytics is not wired.** `PLAN/20` asks for privacy-respecting page analytics. Every option is a third party to trust or a service to run, which is a decision rather than a default, and there is no traffic to measure yet.
- **`CLAIMS.md` lags by design.** It is accurate at the moment it was written and will be wrong after the first phase completes. It says so, and names when to re-run it.
