# public-site/

The public marketing and documentation site: landing page, about, docs, changelog, contact.

**Governing documents**: `PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`, `UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md`, `UI-UX/21-CONTENT-AND-COPY-STRATEGY.md`.

**Status**: `P0-18`. The structure, docs versioning, search and the generated API reference all work. Real content for the landing and About pages is `P0-19`.

---

## One project, not two

`PLAN/20` suggests a marketing static-site generator alongside a separate docs framework. This is a single Docusaurus project instead, recorded as a deliberate deviation in [ADR-014](../MEMORY/DECISIONS.md).

The reason is `UI-UX/20` § Cross-Page Requirements: every page shares the same header and footer, "so moving between Landing → Docs → About never feels like a different product". Two projects make that navigation a duplicated component in two codebases — and duplicated navigation is exactly what drifts, because a link added to one is a link somebody has to remember to add to the other.

The marketing pages are ordinary React and MDX under `src/pages/`. If the landing page's bundle weight ever becomes a real problem rather than a hypothetical one, they move to a separate Astro build and the docs stay put.

---

## Deliberately separate from the console

Different audience, different tech, different deploy cadence. The two share **only** the brand-level visual language (`UI-UX/06`) — never a codebase, never a component library, never a deploy pipeline.

Both halves of that are enforced rather than trusted:

| Script | What it protects |
|---|---|
| `check:tokens` | The shared colour values still match `console/src/styles/tokens.css`. Duplication on purpose and duplication by accident look identical six months later. |
| `check:boundary` | No file here imports anything from `console/`. This rule erodes by convenience, not by decision — someone imports one useful component because it is right there. |
| `check:contrast` | Every colour token meets WCAG 2.1 AA, in **both** themes. |

---

## Commands

```bash
npm install

npm start            # dev server on :3000
npm run build        # static build to build/
npm run serve        # serve the built site

npm run check        # tokens + contrast + boundary + typecheck + build
npm run api:generate # regenerate the API reference from openapi/openapi.yaml
npm run docs:version -- 1.1   # snapshot the current docs as a new version
```

---

## Content rules that are not negotiable

**Nothing describes a capability that has not shipped.** `UI-UX/21` § Content Governance and `CLAUDE.md` both say so. The project is in Phase 0 — the service runs and serves two operational probes — so the landing page describes the four headline capabilities as *design*, each labelled with the roadmap phase that delivers it, rather than as things a visitor can use.

`src/components/PhaseNotice.tsx` exists so this is structural rather than a matter of careful phrasing. Careful phrasing is the first thing to erode: a sentence gets tightened, a hedge disappears, and a roadmap item is now a claim.

**No social-proof section.** `UI-UX/20` is explicit — include it "only once genuinely available", because "an empty or fabricated social-proof section is worse than omitting it entirely".

**`/docs/api-reference` is generated**, never hand-written (`CLAUDE.md`). The same `openapi/openapi.yaml` generates the backend's server interface (ADR-013) and the console's typed client, so all three cannot disagree.

**`/security` is not published yet.** `PLAN/20` § Deployment & Roadmap Placement puts the trust page alongside Phase 5. `/contact` carries the responsible-disclosure path in the meantime, which `PLAN/20` requires to exist — a researcher with nowhere to report goes public instead.

**`/pricing` does not exist.** `PLAN/20`: omit entirely unless a commercial tier exists.

---

## Docs versioning

Enabled from the first commit. `PLAN/20` § Versioning Strategy requires old-version docs to stay reachable through a version's deprecation window, and retrofitting versioning once v1 docs exist means reorganising every file at the moment there is most content to break.

Two versions exist:

- **`current`** — the live, in-development documentation, served at `/docs`.
- **`1.0`** — a snapshot at `/docs/1.0`, labelled *placeholder* and carrying an "unmaintained" banner. It proves the pipeline works before there is a release to version, for the same reason the API reference is generated now from a spec with two endpoints in it.

It is labelled a placeholder rather than presented as a shipped release: everything else here is careful not to claim something exists that does not, and a version dropdown implying a 1.0 release would be the same untruth in a different control. Delete it when a real release replaces it.

---

## Search

Local, indexed at build time (`@easyops-cn/docusaurus-search-local`), covering docs, changelog and pages — 123 documents at last build.

`PLAN/20` names Algolia DocSearch or built-in local search. Local, because Algolia means an external crawler and an API key for a site with a dozen pages. `UI-UX/20` § Docs Home requires fuzzy matching because "a developer often doesn't know the exact terminology this project uses yet", which local search does.

---

## Deployment

Static output in `build/`, deployable to any static host or CDN, through a pipeline entirely separate from the console's and the backend's — `PLAN/20`: "a docs typo fix shouldn't require a backend deploy pipeline".

`SITE_URL` and `SITE_BASE_URL` set the canonical origin at build time. Getting them wrong is not cosmetic: they produce canonical URLs and sitemap entries pointing at somewhere that does not exist, on the one surface whose entire job is being discovered.
