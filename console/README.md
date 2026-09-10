# console/

The management console: a React + TypeScript SPA that authenticates as an ordinary OIDC client (`type: spa`, Authorization Code + PKCE) — the dogfooding constraint from `docs/PLAN/02-REQUIREMENTS.md` and `docs/PLAN/06-FRONTEND-ARCHITECTURE.md`.

**Governing documents**: `docs/PLAN/06-FRONTEND-ARCHITECTURE.md` (engineering), the whole `docs/UI-UX/` folder (design), starting at `docs/UI-UX/00-DESIGN-DIRECTION.md`. Task breakdown: `TASKS/PHASE-F-FRONTEND-IMPLEMENTATION.md`.

**Status**: `P0-17`. The shell exists — tokens, routing, navigation, error boundary, API client. The screens themselves are Phase 1; every nav destination renders a placeholder that says so.

---

## Rules That Are Not Negotiable

- **Tokens only.** No raw hex colors, no arbitrary spacing. New tokens and components go into the design system before a page uses them (`docs/UI-UX/05-DESIGN-SYSTEM.md` § Governance). Enforced by lint, not by memory.
- **`color-danger` is reserved** for destructive and irreversible actions (`docs/UI-UX/06-VISUAL-LANGUAGE.md`), and is not overridable by per-organization branding. Enforced by a type and a runtime guard, and tested.
- **Permission-gated routes are genuinely unreachable, not hidden.** The API enforces independently regardless (`docs/UI-UX/08-PAGE-SPECIFICATIONS.md`, `docs/PLAN/08`).
- **Every screen runs through the twelve-step chain** in `docs/UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md` before it is considered implementable.
- The API client is **generated** from `openapi/openapi.yaml`; never hand-written.

---

## Commands

```bash
npm install

npm run dev          # vite dev server on :5173
npm run build        # typecheck, then static SPA build to dist/
npm run check        # lint + typecheck + tests — what CI runs

npm run lint
npm run typecheck
npm test
npm run api:generate # regenerate the typed client from openapi/openapi.yaml
```

---

## Design Tokens

`src/styles/tokens.css` holds every token `docs/UI-UX/05` names, under the name that document gives it.

`docs/UI-UX/05` names the tokens and describes their intent — "neutral, low-saturation", "must meet WCAG AA against both bg tokens" — without fixing values. The values here are this implementation's, and `tokens.test.ts` computes the contrast ratios **from the stylesheet** rather than trusting the comment beside them. Change a colour and the check runs against the new value.

### The one place the token vocabulary is doubled

Tailwind v4 generates utilities from its own theme namespaces, which do not all match `docs/UI-UX/05`'s names:

| `docs/UI-UX/05` | Tailwind namespace | Utility |
|---|---|---|
| `--font-size-body` | `--text-body` | `text-body` |
| `--line-height-body` | `--leading-body` | `leading-body` |
| *(none)* | `--ease-standard` | `ease-standard` |

So both exist: the design-system name holds the value, and the Tailwind name references it. There is one place to change a size and no pair to keep in step.

This is not incidental tidiness. Writing only the `docs/UI-UX/05` names produced a stylesheet that looked complete, passed every test, built without warning — and generated no `text-body` class at all, so every piece of text rendered at the browser default. `utilities.test.ts` exists because of that bug: it compiles Tailwind and asserts the classes the components use actually produce CSS. **A test that reads the input to a compiler cannot tell you what the compiler did.**

### `color-border` is heavier than it looks

3:1 against both backgrounds, because WCAG 2.1 AA (1.4.11) requires that for the visual information identifying a UI component, and an input's border is exactly that. `docs/UI-UX/05` gives one token for both input borders and table dividers; a divider-weight value would be prettier and would fail every text input. Raised as `BACKLOG` `PG-12` rather than split unilaterally.

---

## Branding

`src/branding/` implements the rule that an organization may override the accent colour and the logo, and nothing else.

The overridable set is a **union of literal token names**, so `applyBranding` cannot be called with `--color-danger` even by a caller holding one in a variable — and a runtime filter backs it up, because branding arrives as JSON from an API where the type system has already ended.

A custom accent is contrast-checked before it is applied, against the surface it will sit on. `docs/UI-UX/13` requires validation at the point an org admin sets it "rather than allowing an inaccessible combination to ship silently" — and the accent is the focus ring, so an unreadable one is an accessibility failure on every screen at once.

---

## Testing

```
src/styles/tokens.test.ts      the tokens exist, by name, and meet contrast
src/styles/utilities.test.ts   the classes components use actually compile
src/branding/branding.test.ts  danger is not overridable; accent is checked
src/app/shell/AppShell.test.ts landmarks, skip link, keyboard order, axe
```

`expectNoAxeViolations` (`src/test/axe.ts`) runs axe-core restricted to the WCAG 2.1 A/AA rule set the project targets. It catches mechanical failures, not the ones needing judgement — `docs/UI-UX/13` also requires a manual screen reader pass before Phase 5, and that is still outstanding.

---

## Deployment

A static SPA build, served from a CDN or any static host, calling the Management API over HTTPS. Deliberately decoupled from the backend deploy: a console fix must not require shipping the auth service (`docs/PLAN/06`).

`VITE_API_BASE_URL` is a **build-time** variable, empty meaning same-origin. Runtime-configurable in a static SPA would mean an attacker who can influence that value redirects every bearer token the console holds.

A preview of the current build runs on the staging VM behind `console.zedth.my.id`, bound to `127.0.0.1:10920` so only `cloudflared` reaches it.
