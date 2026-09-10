# Reference documentation moved under `docs/`

**Date**: 2026-09-10
**Branch**: `feat/P1-08-userinfo`
**Reason**: the owner asked for the AI-execution folder structure used in [`zed378/wedding-saas`](https://github.com/zed378/wedding-saas).

---

## What changed

```
PLAN/       →  docs/PLAN/       (21 files)
UI-UX/      →  docs/UI-UX/      (22 files)
SECURITY/   →  docs/SECURITY/   ( 6 files)
+ docs/README.md
```

`TASKS/` and `MEMORY/` did not move. They were already identical in shape to the reference repository, down to having their own `README.md` — which is the part of the structure that actually governs AI execution, and it was already right.

226 files were rewritten to follow the paths.

## What was NOT done, and why

`wedding-saas` splits its documentation into ten topical folders: `PLAN/`, `ARCHITECTURE/`, `API/`, `DATABASE/`, `BACKEND/`, `FRONTEND/`, `DEVOPS/`, `TESTING/`, `SECURITY/`, `UI-UX/`. This repository has three, because it keeps **one document per topic** where that one keeps a folder per topic — `docs/PLAN/07-BACKEND-ARCHITECTURE.md` is what a `BACKEND/` folder would hold, `docs/PLAN/04-DATA-MODEL.md` is what a `DATABASE/` folder would hold.

Matching the ten-folder taxonomy would mean splitting single documents into eight-to-twelve files each. That is **authoring new content**, not moving folders, and the request was about folder structure. It would also rewrite the plan documents themselves, which `AGENTS.md` rule 9 and `.github/CODEOWNERS` deliberately put behind a review gate.

`docs/README.md` states the difference and says what the natural move is if a topic ever outgrows its file.

## How the rewrite was done, and why it was safe

The three folders turned out to be **self-contained**: 44 markdown links between them, every one of the form `./sibling.md` within the same folder, and not a single outbound relative link to `TASKS/`, `MEMORY/`, or any code directory. So moving them together required **zero edits inside them** — the internal links stayed valid because everything moved at once.

That is worth recording as a property rather than luck. It is what made a 226-file mechanical rewrite a one-pass script instead of a per-file judgement call.

Everything outside was one substitution, `(PLAN|UI-UX|SECURITY)/` → `docs/\1/`, with two guards:

- A negative lookbehind on `[A-Za-z0-9_]`, so `EXPAND/CONTRACT` and any word ending in `PLAN` are untouched. A leading `/` is deliberately allowed, because `../PLAN/` is exactly the case that has to be rewritten.
- Already-prefixed references are replaced with a sentinel first and restored afterwards, so the script is **safe to run twice**. Without that, a second run produces `docs/docs/PLAN/`.

Verified by grepping the whole tracked tree for `docs/docs/` (none) and for surviving bare references (none).

**Only one reference in the repository was a functional path**: `.github/CODEOWNERS`, which is what makes a change under these folders require a review. Everything else — all 225 other files — was prose in a comment or a markdown link. That is a pleasant thing to discover and it is also why the change is low-risk: a mistake would have been a broken link, not a broken build.

## What the move surfaced

**A public link that the move broke, and the rewrite fixed.** `public-site/changelog/2026-09-08-phase-0-foundation.md` links to the roadmap on GitHub by absolute URL. After the move that URL was a 404; the rewrite corrected it. Nothing in CI checks external links, so this would have been found by a reader rather than by a test — the sort of thing a purely mechanical rewrite gets right for free and a hand-edited one forgets.

**`BL-04`**: the generated public API reference cites internal plan documents by bare path — `(docs/PLAN/02 FR-14)` and seventeen more, from `description` fields in `openapi/openapi.yaml`. Not a leak (the repository is public, and the leak checker is about three specific documents' content), but poor public documentation: a bare path with no link tells an integrator nothing. It was already there; regenerating the reference put it in a diff, which is where it got noticed.

## Records were rewritten too, and that is a decision

`MEMORY/records/` are historical documents — they say what was true on a date. Twenty-three of them contain paths that no longer resolve.

The paths were rewritten. What a record **claims** is unchanged; only where a cited document now lives is. A record whose links 404 is a record nobody follows, and the alternative — leaving them stale and adding a note explaining that pre-2026-09-10 records use the old paths — is a footnote every future reader has to carry.

The same reasoning applies to `MEMORY/specs/` and to the task cards.

## Verification

`CHECK_FULL=1 scripts/check.sh` — all 40 gates, including the public site build's broken-link check, the capability audit, and the leak check whose `FORBIDDEN_SOURCES` are resolved as real filesystem paths and therefore had to be updated correctly or it would have found nothing.

Two things broke and were fixed:

- `internal/oauth/token`'s **integration** test called `signing.Verifier.Verify` with the old single argument. `go vet ./...` does not compile files behind a build tag, so it passed while the tagged build did not. Caught by `check.sh`, which runs the integration suite. **`go vet -tags=integration ./...` after any exported-signature change** — the plain vet is not enough in this repository.
- The generated API reference was stale, because the paths inside `openapi.yaml`'s descriptions changed. Regenerated rather than hand-edited, which is the rule (`CLAUDE.md`: never hand-write API reference documentation).
