# 07 - Forms and Validation

> Category: **Frontend Engineering** (`docs/FRONTEND/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-22, P2-11, P2-14, P3-10, P4-05, P4-06 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

Describe how the console validates input, reports what the server refused, and confirms an
action that cannot be undone. The governing rule is short: **the console never invents a
validation rule.**

## Scope

`console/src/lib/api/patterns.gen.ts`, `console/src/lib/api/settings.gen.ts`, and the
forms inside `console/src/pages/`. `docs/UI-UX/15-FORM-UX.md` is the design intent.

## As Built

### Client-side rules are generated from the same schema the server enforces

`console/scripts/gen-patterns.mjs` reads `openapi/openapi.yaml` and emits:

- `console/src/lib/api/patterns.gen.ts` — `ROLE_KEY_PATTERN`, `PERMISSION_KEY_PATTERN`,
  their maximum lengths, and compiled `isValidRoleKey` / `isValidPermissionKey` helpers.
- `console/src/lib/api/settings.gen.ts` — `SETTINGS_BOUNDS` and `SETTINGS_DEFAULTS` for
  organization policy: password minimum length, maximum age, session lifetime, allowed
  login methods.

`scripts/check.sh` regenerates both and fails on a diff.

A hand-written regex in the console is a rule that will drift from the one the API
enforces, and the drift presents as a form that accepts what the server refuses — or, more
insidiously, one that refuses what the server would accept, which nobody reports as a bug.
`console/src/pages/RolesPage.tsx` is the worked example; its test asserts that an
upper-case role key is rejected *because the generated pattern rejects it*, not because
the page has an opinion.

`SETTINGS_DEFAULTS` does double duty: each field shows what the service applies when the
organization's setting says nothing, so an administrator can see what they are changing
from.

### The client check is convenience; the server check is the control

Every form still sends the request and still renders what the server said. Client
validation exists so a user learns about a typo before a round trip, not so the console
can decide what is acceptable. This is the same rule as
[`06-ROUTING-AND-PERMISSIONS.md`](./06-ROUTING-AND-PERMISSIONS.md)'s, one layer down.

For Project Grants it is explicit in `AGENTS.md` hard rule 3: a delegated role assignment
is validated server-side as a subset of `granted_role_keys` **on every request**, and the
console's role list is a rendering convenience on top of that.

### Server errors are shown as the server wrote them

```ts
envelope?.error?.details?.[0]?.issue ?? envelope?.error?.message ?? "…"
```

Each form repeats this shape. The service answers an unknown, a suspended and a
self-referential organization with one sentence (`P4-01` A-5), and the console repeats it
rather than guessing which case occurred — guessing is how a console tells a user
something the server did not say.

Field-level problems use `aria-invalid` and `aria-describedby`, and the error **replaces**
the help text rather than appearing alongside it, so a screen reader announces one
statement about the field instead of two.

### Choices survive a refusal

A refused create renders the problem on the step where it can be fixed, and going back
returns to the form with the values intact. Losing a half-filled form to a server error is
the failure `docs/UI-UX/14` is about: the user did nothing wrong, and their work should
not vanish.

### Two-step confirmation for the high-consequence flows

`docs/UI-UX/04` Flow 2 asks for choose → read back → confirm. `ProjectGrantsPage`
implements it inside one modal: the summary sentence is rewritten live as roles are ticked,
and the second step repeats it as the thing being confirmed. Both directions of a
delegation get a consequence preview before the final action.

### Destructive actions state the consequence, not the rule

`ConfirmDialog` takes a `consequence` node, not a warning string. "This will not be shown
again" is a fact about the system; "keep it somewhere safe" is advice. The dialog says the
first.

`typeToConfirm` adds friction proportional to the blast radius: revoking a Project Grant
asks for the partner organization's exact name **only when somebody holds a role through
it**, and the count comes from the server (`holder_count`) because the console cannot see
the partner's users.

Two defects in that component were found by its first typed-confirmation caller and fixed
in the component: typed text survived a cancel, so reopening arrived already unlocked —
the friction spent before the new consequence was read — and the typed input never took
focus.

### Where a picker would leak, there is no picker

Creating a Project Grant asks for the partner's **organization ID**, typed (ADR-026). A
search box would let any administrator enumerate every organization on the instance. The
name appears once the grant exists, resolved by a bounded server-side lookup.

The same reasoning, inverted, applies on the receiving side: the person picker in
`GrantedProjectsPage` lists this organization's own users, which the caller may already
list, and the server refuses a user outside it regardless.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Validation patterns | generated from the spec | `console/src/lib/api/patterns.gen.ts`, `scripts/check.sh` |
| Settings bounds and defaults | generated from the spec | `console/src/lib/api/settings.gen.ts` |
| Server error text | shown verbatim from `details[0].issue` or `message` | every form |
| Field error | `aria-invalid` + `aria-describedby`, replacing the help text | `console/src/pages/PoliciesPage.tsx`, `AccountPage.tsx`, `MfaTab.tsx` |
| Destructive confirmation | consequence, not rule; typed name when holders > 0 | `console/src/components/ConfirmDialog.tsx` |
| Organization selection | typed ID, never a search | ADR-026 |

## Verification

- `console/src/pages/roles.test.tsx` — an upper-case role key is refused by the generated
  pattern; a permission key outside `resource:action` form is refused.
- `console/src/pages/policies.test.tsx` — bounds and defaults come from the generated file.
- `console/src/pages/projectgrants.test.tsx` — the typed confirmation, its reset on cancel,
  and its focus behaviour.
- `console/src/pages/grantedprojects.test.tsx` — the assign button stays disabled until
  both a person and a role are chosen; a server refusal is rendered as the server wrote it.

## Not Yet Built / Open Questions

- **No shared form abstraction.** Each screen wires its own fields. With a dozen forms
  this is still cheaper than an abstraction that has to cover all of them, but the
  `aria-invalid`/`aria-describedby` pairing is repeated by hand and could be a component.
- **No cross-field validation helper.** Nothing needs one yet.

## Related Documents

- [`08-LOADING-ERROR-EMPTY-STATES.md`](./08-LOADING-ERROR-EMPTY-STATES.md)
- [`09-ACCESSIBILITY-PRACTICE.md`](./09-ACCESSIBILITY-PRACTICE.md)
- [`../UI-UX/15-FORM-UX.md`](../UI-UX/15-FORM-UX.md)
