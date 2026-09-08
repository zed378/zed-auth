# The Console Shell, and What "The Token Exists" Does Not Mean

**Date**: 2026-09-08
**Task**: `P0-17` (also closes `P0-16` step 3)
**Branch**: `feat/P0-17-console-skeleton`

---

## What Was Built

A React 19 + TypeScript SPA on Vite 8 and Tailwind 4: design tokens, routing, the navigation tree from `PLAN/06`, an error boundary, TanStack Query, and the typed API client generated from `openapi/openapi.yaml`.

No screens. Every nav destination renders a placeholder that names the phase it arrives in. `PLAN/16` forbids building a Phase N+1 feature while Phase N is incomplete, and the information architecture is a decision already made — encoding it now means Phase 1 adds page bodies rather than renegotiating structure.

Four things carry the weight:

**Tokens are the only way to name a value.** Every token `UI-UX/05` lists exists in `tokens.css` under that document's name. Three local ESLint rules — `no-raw-color`, `no-arbitrary-value`, `no-inline-style` — make a raw hex or a one-off `p-[13px]` a build failure. `UI-UX/05` § Governance asks for this; a rule people are asked to remember holds until the week someone is in a hurry.

**`color-danger` is un-overridable structurally.** The brandable set is a union of literal token names, so `applyBranding` cannot be called with `--color-danger` even by a caller holding one in a variable — plus a runtime filter, because branding arrives as JSON from an API where the type system has already ended. A custom accent is contrast-checked against the surface it will sit on before it is applied, which `UI-UX/13` requires at the point an org admin sets it.

**Contrast is computed, not claimed.** `tokens.test.ts` parses `tokens.css` and computes WCAG ratios from the values actually in it, so changing a colour runs the check against the new one.

**Accessibility is in the shell, not on a list.** Landmarks, a skip link whose target is genuinely focusable, 44px targets at compact density, a focus ring using `color-accent` that `outline: none` cannot be added anywhere near, and a `prefers-reduced-motion` fallback. All tested, plus an axe pass restricted to the WCAG 2.1 A/AA rules the project targets.

---

## The Bug That Justifies This Whole Record

Every token was defined. Named exactly as `UI-UX/05` names them. Contrast verified. Lint clean, typecheck clean, 72 tests passing, build succeeding.

And `text-body`, `text-heading-1`, `text-heading-2`, `text-heading-3`, `text-small` generated **no CSS at all**. Every piece of text in the console rendered at the browser's default size. So did `duration-quick`, `ease-standard`, and `w-nav` — the navigation had no width.

Tailwind v4 reads font sizes from `--text-*`. `--font-size-*` is not a namespace it knows, so those declarations sat in the stylesheet as inert custom properties. Same for `--line-height-*` (it wants `--leading-*`), `--easing-*` (`--ease-*`), and `--duration-*`, which is not a namespace at all.

Nothing failed. **The tests passed because they read the source file**, and the source file contained exactly what it was supposed to contain. The gap between "the token is defined" and "the class works" is invisible from the input.

> A test that reads the input to a compiler cannot tell you what the compiler did.

The fix keeps both vocabularies: `UI-UX/05`'s names hold the values (satisfying the DoD's "every token exists, by name"), and Tailwind's namespaces reference them, so there is one place to change a size. `@theme static` rather than a bare `@theme`, so a token exists whether or not a utility happens to use it yet — `--color-danger` has no user in the shell and must be there for the first destructive action that needs it.

`utilities.test.ts` now compiles Tailwind against the real source tree and asserts the classes the components use produce rules. Verified the only way worth trusting: the bug was deliberately reintroduced. The new test failed on all five sizes; the old source-reading test passed 48 of 48.

---

## Three More Found by Looking Rather Than Assuming

**The lint rule caught a bug, not just a style violation.** Its first run rejected `tablet:w-[--spacing-nav]` in `SideNav` — and `--spacing-nav` was a token I had never defined, so the navigation would have had no width. The rule was written to enforce a convention and paid for itself immediately by finding a defect.

**Two `<h1>` elements.** The narrow-width message painted a full-screen overlay *on top of* the layout and left the layout in the DOM. Visually right, wrong twice: the document had two top-level headings, and below tablet width a screen-reader user would have walked past the "this screen is too narrow" message straight into the application it says is unusable — a visual overlay hides nothing from assistive technology. Now the subtrees are swapped rather than stacked, so exactly one exists at any width.

**A focus ring that looked broken and was not.** Measuring the ring after `element.focus()` showed the accent colour on two links and `#15181d` on the rest. The implementation was correct: `:focus-visible` is a user-agent heuristic that programmatic focus does not satisfy for links, so the ring was not applying at all and `getComputedStyle` reported `currentColor`. Real `Tab` presses show `2px solid rgb(29, 78, 216)` on every link. Worth recording because the wrong conclusion — "the token is not resolving" — would have led to a fix for a bug that did not exist.

---

## Deployment, and the Refresh Bug

The build is served from the staging VM behind `console.zedth.my.id`, bound to `127.0.0.1:10920` so only `cloudflared` reaches it.

**Refreshing on `/projects` returned 404**, reported while the preview was already public. The classic history-mode failure: a static host looks up `/projects` as a file, does not find one, and answers 404 — so the application that would have routed it never loads. It works while you navigate inside the app and breaks on every refresh, bookmark, and link from a ticket, which is most of an admin console's traffic.

`deploy/console/nginx.conf` fixes it with `try_files $uri $uri/ /index.html`, and carries two more things worth having:

- **`index.html` is never cached; fingerprinted assets are cached forever.** The asymmetry is the part people get wrong — `index.html` is the file that *names* the current bundle, so caching it means a returning admin keeps loading the previous release's entry point, pointing at the previous release's assets, and the deploy silently never reaches them.
- **Security headers**, because nothing the backend sets (`P0-10`) reaches a response served by this nginx.

The first version of that config put the security headers at server level and the cache headers per location. In nginx `add_header` does **not** accumulate across contexts: a `location` block with any `add_header` of its own discards every server-level header. The security headers vanished from exactly the responses people load. `curl -I` against the deployed site found it; nothing in the config looked wrong. Both now come from one context, with the cache policy in a `map`.

The healthcheck probes `/projects`, not `/` — a route with no file behind it, so the probe fails if the SPA fallback is ever dropped. A probe on `/` would stay green while every refresh 404s.

It also had to be told `127.0.0.1` rather than `localhost`: nginx's `listen 80` is IPv4-only while `localhost` resolves to `::1` first, so the probe got "connection refused" from a server that was serving perfectly.

---

## Verified

Against `https://console.zedth.my.id` in a real browser, not jsdom.

| Width | Nav | Main | Narrow message | Visible `<h1>` | Overflow |
|---|---|---|---|---|---|
| 1440 | 240px | 1200px | hidden | 1 — "Overview" | none |
| 1024 | 240px | 784px | hidden | 1 | none |
| 768 | 240px | 528px | hidden | 1 | none |
| 600 | hidden | hidden | shown | 1 — "too narrow" | none |

Keyboard: `Tab` reaches the skip link first, then every nav link, each matching `:focus-visible` with `outline: 2px solid rgb(29,78,216)` — the accent token — and a 44px target height.

Deep links, headers and caching all confirmed on the public URL. `scripts/check.sh`: 29 gates, 0 failures.

---

## Outstanding

- **`PG-12`**: `color-border` serves both input borders (WCAG 1.4.11 wants 3:1) and table dividers (which want a hairline). One token cannot do both well; it is set to the accessible value, so tables will read heavier than `UI-UX/00`'s density principle wants until `UI-UX/05` splits it.
- **`VITE_API_BASE_URL` is empty**, meaning same-origin. Nothing calls the API yet, so nothing is broken — but on `console.zedth.my.id` a Phase 1 call would go to that host rather than `auth.zedth.my.id`. Setting it is a build-time change plus a CORS decision on the backend, and belongs with `P1-03` when the console first authenticates.
- The manual screen-reader pass `UI-UX/13` requires before Phase 5 has not happened. axe catches mechanical failures, not the ones needing judgement.
- The console preview has no access control. It exposes the roadmap and the navigation structure and no data at all, which is a judgement the owner made deliberately in order to look at it.
