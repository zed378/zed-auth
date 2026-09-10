# The Public Site, and a Plan That Contradicted Itself

**Date**: 2026-09-08
**Task**: `P0-18` (also closes `P0-16` step 4)
**Branch**: `feat/P0-18-public-site-skeleton`
**Decision**: [ADR-014](../DECISIONS.md)

---

## The Conflict Worth Naming First

`docs/PLAN/20` § Recommended Stack lists a marketing static-site generator and a docs framework as separate rows, and `P0-01` step 5 restates that as two things to choose. Read as an instruction, it is two projects.

`docs/UI-UX/20` § Cross-Page Requirements pulls the other way: every page shares the same header and footer, "so moving between Landing → Docs → About never feels like a different product."

Two projects make that navigation a duplicated component across two codebases with two build systems. Duplicated navigation is not a theoretical risk — it is the specific thing that drifts, because a link added to one is a link somebody has to remember to add to the other, and the failure only shows on the path nobody tested.

Neither document is wrong; they optimise for different things. `CLAUDE.md` says a deviation should be a visible decision rather than a silent one, so it is [ADR-014](../DECISIONS.md): **one Docusaurus project**, with the marketing pages as ordinary React and MDX that could move to a separate Astro build later without touching the docs.

The deciding argument was not convenience. It was that `docs/PLAN/20` § Versioning Strategy makes docs versioning a requirement and `P0-18`'s Definition of Done asks for it demonstrated — and the obvious marketing-side alternative, Astro with Starlight, has no native versioning. Choosing a stack that needs a third-party plugin for a stated requirement is the wrong trade.

---

## What Was Built

The full route structure from `docs/PLAN/20`: `/`, `/about`, `/contact`, `/docs` with quickstart, concepts, guides, console and a generated API reference, and `/changelog`. Thirty-four pages.

`/pricing` does not exist — `docs/PLAN/20` says omit entirely unless a commercial tier exists. `/security` does not exist either, because `docs/PLAN/20` § Deployment & Roadmap Placement puts the trust page alongside Phase 5; `/contact` carries the responsible-disclosure path in the meantime, which `docs/PLAN/20` requires to exist. A researcher with nowhere to report goes public instead.

**The API reference is generated** from `openapi/openapi.yaml` — the same file that generates the backend's server interface (ADR-013) and the console's typed client. Three consumers, one source, so none of them can disagree. This closes `P0-16` step 4, the last open piece of that task.

**Docs versioning is live** with two versions: `current` (in development, at `/docs`) and a `1.0` snapshot at `/docs/1.0`, labelled *placeholder* and carrying an unmaintained banner. It proves the pipeline before there is a release to version, for the same reason the API reference is generated now from a spec with two endpoints in it. It is labelled rather than presented as a release: everything else here is careful not to claim something exists that does not, and a version dropdown implying a shipped 1.0 would be that same untruth in a different control.

**Search** is local and indexed at build time — 123 documents. `docs/UI-UX/20` requires fuzzy matching because "a developer often doesn't know the exact terminology this project uses yet".

---

## The Content Rule, Made Structural

`docs/UI-UX/21` § Content Governance and `CLAUDE.md` both forbid present-tense copy for an unshipped capability. `docs/UI-UX/21`'s own landing-page blueprint has four capability cards — SSO, REST API, RBAC, policies — written in the present tense, and none of those four exists. The project is in Phase 0: the service runs and serves two operational probes.

So the landing page describes them as the design, each labelled with the roadmap phase that delivers it, under a heading that says so. The hero keeps `docs/UI-UX/21`'s headline verbatim, because it describes what the product is *for* rather than what you can do with it today.

Two smaller judgements followed from the same rule:

- **No social-proof section.** `docs/UI-UX/20` is explicit — include it "only once genuinely available", because "an empty or fabricated social-proof section is worse than omitting it entirely". There is nobody to quote.
- **The hero CTA points at the docs, not a quickstart.** `docs/UI-UX/21`'s blueprint says "Get Started". A "Get Started" button leading to a page that says "not available yet" costs more trust than it wins.

`PhaseNotice` exists so this survives contact with future edits. Careful phrasing is the first thing to erode — a sentence gets tightened, a hedge disappears, and a roadmap item is a claim. A component is visible on the page, greppable in the source, and awkward to remove by accident.

---

## Three Checks Instead of Three Conventions

`docs/PLAN/20` § Why a Separate Surface says these two surfaces share the visual language and nothing else. That is two rules, and both erode quietly rather than by decision.

**`check-brand-tokens.mjs`** — the nine shared colour values still match `console/src/styles/tokens.css`. Duplication on purpose and duplication by accident look identical six months later, and the failure is silent: two slightly different blues that nobody notices until a visitor crosses from the site into the console.

**`check-no-shared-code.mjs`** — nothing here imports from `console/`. Nobody ever proposes coupling the two; someone imports one useful component because it is right there and works, and the boundary is gone.

**`check-contrast.mjs`** — every token meets WCAG 2.1 AA in both themes. This one earned its place immediately.

---

## The Dark Mode That Would Have Shipped Unreadable

The console has no dark mode. This site does, and the obvious move was to carry the console's palette across.

Measured against the dark surface, that palette gives: accent **2.4:1**, danger **2.7:1**, warning **3.3:1**, success **2.8:1** — every one below the 4.5:1 `docs/UI-UX/13` requires — and the border at **1.8:1**, below the 3:1 WCAG 1.4.11 wants of a component boundary.

It would have looked deliberate and been unreadable, which is worse than not offering a dark mode at all. Each token was re-tuned: same *meaning*, different value for a different surface. Danger is still the colour reserved for irreversible things; it is a different red because what sits behind it changed.

None of that was visible by looking. It came from computing the ratios, which is why the check is a script rather than a comment beside the values — and the script was verified by reverting the dark accent to the light value and watching it fail.

---

## Two Deployment Bugs

**A soft 404.** The nginx config used `try_files $uri $uri/ /404.html`, which serves the styled page with a **200** status. The visitor sees the right thing and every crawler is told the missing URL exists — on the one surface whose entire job is discovery, where `docs/PLAN/20` names SEO as critical. Fixed to `=404` with `error_page`, which returns the real status and still renders the page with its header, footer and search box.

**A security check that cried wolf.** `scripts/check.sh` failed on "a PEM private key block is committed". The three matches were inside `public-site/node_modules` — a certificate library's own fixtures, gitignored, committed by no definition. The check walked the working tree while its message said "committed". Now it scans `git ls-files`, like the credential-token check beside it always did. A security check that fires the first time someone installs dependencies in a new directory is a check that gets commented out.

Both were found by running the thing rather than reading it.

---

## Verified

On the VM, served by the same nginx config that would serve it in production.

| Check | Result |
|---|---|
| Pages built | 34, both doc versions |
| `/`, `/docs/*`, `/docs/1.0/*`, `/changelog/` | 200 |
| `/nope` | 404 — real status, styled page, 10KB with header and search |
| Security headers on HTML and assets | present |
| Cache: HTML / hashed assets | `max-age=300` / `immutable` |
| `/pricing`, `/security` | absent, per `docs/PLAN/20` |
| Search index | 123 documents |
| Sitemap, RSS, Atom, robots.txt | present |
| `scripts/check.sh` | 33 gates |

---

## Outstanding

- **Real landing and About copy is `P0-19`**, along with the capability audit that task requires. What is there now is structurally right and reads as a skeleton.
- **The Lighthouse bar in `P0-18`'s Definition of Done does not exist.** It points at `docs/UI-UX/20` § Cross-Page Requirements, which sets an accessibility bar and no numeric performance target. Nothing has been measured against a number, because there is no number. Worth settling in `P0-19` or deleting from the DoD.
- **Analytics is not wired.** `docs/PLAN/20` asks for privacy-respecting page analytics; every option is a third party or a service to run, which is a decision rather than a default. Deferred to `P0-19` with the content it would measure.
- The site is deployed on the VM at `127.0.0.1:10930`, not yet behind a hostname.
