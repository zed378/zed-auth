# 04 - TypeScript Standards

> Category: **Engineering Practice** (`docs/ENGINEERING/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-17, P0-18, PF-01 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

The TypeScript and React conventions for the console and the public site. The console's
architecture is [`../FRONTEND/`](../FRONTEND/); this is the language-level practice.

## Scope

`console/`, `public-site/`.

## As Built

### Functional components, hooks, no class components

`docs/PLAN/06` § Tech Stack. A class component in this codebase would be the odd one out
and would not get hooks.

### Types come from the contract, not from hand-written interfaces

```ts
type Received = components["schemas"]["ReceivedGrant"];
```

Schemas are read out of `console/src/lib/api/schema.gen.ts`, never re-declared. A
hand-written mirror of an API type drifts silently, and the drift surfaces as a runtime
`undefined` rather than a compile error.

The exception is a **narrow local shape** for something the screen uses structurally:

```ts
type User = { id: string; email: string; display_name?: string | null };
```

That is deliberate — the screen needs three fields, and naming them documents the
dependency.

### `strict` is on, and `any` does not appear

`console/tsconfig.json`. Where a value genuinely is unknown — an error envelope from the
network — the type is `unknown` and it is narrowed at the point of use:

```ts
function messageFor(error: unknown): string {
  const envelope = error as { error?: { message?: string; details?: { issue?: string }[] } };
  ...
}
```

A cast at one boundary, with the shape written out, rather than `any` spreading outward.

### Nullable API fields are checked, not assumed

`openapi-typescript` runs with `--default-non-nullable false`, so an optional field is
optional in the type. `revoked_at` is `string | null | undefined`, and the screens check
both:

```ts
grant.revoked_at !== null && grant.revoked_at !== undefined
```

Verbose, and correct: a `!= null` shorthand works, but the explicit form matches the three
states the API actually has.

### ESLint is configured to error, not warn

`console/eslint.config.js`:

- `js.configs.recommended` and `typescript-eslint`'s recommended set.
- `react-hooks` recommended — the dependency-array rules catch real bugs, and this project
  has had one: a focus effect that took `onClose` as a dependency re-ran on every keystroke.
- **`jsx-a11y` recommended as errors.** A warning in a lint run of a hundred files is a
  line people scroll past.
- Three local rules banning raw colours, arbitrary Tailwind values and inline styles.
- `no-unused-vars` as an error, with `^_` ignored: an unused variable in a component is
  usually a half-finished edit.

`src/lib/api/schema.gen.ts` is excluded — linting it would report on code nobody can edit,
and the fix for anything wrong there is in the spec.

The Playwright directory gets its own block: React's rules misfire badly there, because
Playwright's fixture API takes a `use` callback that has nothing to do with React's `use`
hook, and `async ({}, use) =>` is its idiomatic way of declaring a fixture that depends on
nothing.

### Comments follow the Go side's house style

Long, explaining why. A component's doc comment names the specification it implements and
the rule it enforces:

> **A role the grant does not delegate is never rendered** — not shown disabled, not shown
> greyed, not in the DOM. […] a disabled control invites a support ticket asking to have
> it enabled.

### The public site shares no code with the console

`scripts/check.sh` asserts it. `docs/PLAN/20` requires the two surfaces to be independent
projects, and running them as one would quietly couple what the plan says to keep apart.
Brand values are kept in step by a check that compares them, not by a shared module.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| `strict` | on | `console/tsconfig.json` |
| API types | from `schema.gen.ts` | review |
| `jsx-a11y` recommended | error | `console/eslint.config.js` |
| `react-hooks` recommended | error | `console/eslint.config.js` |
| Raw colours / arbitrary values / inline styles | error | `console/eslint-local-rules.js` |
| Unused variables | error, `^_` exempt | `console/eslint.config.js` |
| Typecheck before build | `tsc --noEmit && vite build` | `console/package.json` |
| Shared code with the public site | none | `scripts/check.sh` |

## Verification

- `npm run check` in `console/` — lint, typecheck, 290 tests.
- `scripts/check.sh` § Console and § Public site.

## Not Yet Built / Open Questions

- **No Prettier.** Formatting is by convention and review. ESLint does not enforce layout.
- **The public site has a lighter rule set** than the console; it does not carry the local
  token rules, because its styling is Docusaurus's.

## Related Documents

- [`../FRONTEND/`](../FRONTEND/)
- [`../WEBSITE/`](../WEBSITE/)
- [`10-TOOLING-LINT-AND-FORMAT.md`](./10-TOOLING-LINT-AND-FORMAT.md)
