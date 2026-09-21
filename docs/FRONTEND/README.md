# Frontend Engineering

How the management console is actually built: the module layout, the data layer, the
component library, and the conventions that stop twenty-one screens from becoming
twenty-one inventions.

This category is the **engineering** counterpart to [`../UI-UX/`](../UI-UX/). The
distinction is load-bearing and worth stating plainly:

| | |
|---|---|
| [`../UI-UX/`](../UI-UX/) | **Design intent.** What the console should look like and how it should behave. Written before the code, frozen under `.github/CODEOWNERS`, amended only by a deliberate plan change. |
| [`../../TASKS/PHASE-F-FRONTEND-IMPLEMENTATION.md`](../../TASKS/PHASE-F-FRONTEND-IMPLEMENTATION.md) | **The work breakdown.** 53 task cards, each with a gate naming the backend task that must land first. |
| **This category** | **The system as built.** Where the code lives, which decisions are already made, and which are still open — with every claim citing a file. |

If you are about to add a screen, read `01`, `04`, `07` and `09`, then run
[`../UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md`](../UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md)
and commit the resulting table beside your code in `console/docs/`.

The **public site** is a separate application with separate rules — it shares no code
with the console, by a CI check. It is [`../WEBSITE/`](../WEBSITE/).

## Documents

| File | Topic | Status |
|---|---|---|
| [`00-FRONTEND-CONTEXT.md`](./00-FRONTEND-CONTEXT.md) | What the console is, what it is not, and the constraints every other document inherits | Implemented |
| [`01-APPLICATION-STRUCTURE.md`](./01-APPLICATION-STRUCTURE.md) | Directory layout, module boundaries, what may import what | Implemented |
| [`02-DESIGN-TOKENS-AND-STYLING.md`](./02-DESIGN-TOKENS-AND-STYLING.md) | The token layer, Tailwind v4 mapping, and the lint rules that keep raw values out | Implemented |
| [`03-COMPONENT-LIBRARY.md`](./03-COMPONENT-LIBRARY.md) | Every shared component, its contract, and when a screen may not invent its own | Implemented |
| [`04-STATE-AND-DATA-LAYER.md`](./04-STATE-AND-DATA-LAYER.md) | Generated client, query keys, pagination, cache invalidation, tenancy scoping | Implemented |
| [`05-AUTHENTICATION-AND-SESSION.md`](./05-AUTHENTICATION-AND-SESSION.md) | PKCE login, in-memory tokens, silent renewal, and the 401 recovery path | Implemented |
| [`06-ROUTING-AND-PERMISSIONS.md`](./06-ROUTING-AND-PERMISSIONS.md) | The route table, guards, and why a guard is never a control | Implemented |
| [`07-FORMS-AND-VALIDATION.md`](./07-FORMS-AND-VALIDATION.md) | Generated validation patterns, field errors, destructive-action confirmation | Implemented |
| [`08-LOADING-ERROR-EMPTY-STATES.md`](./08-LOADING-ERROR-EMPTY-STATES.md) | The four states as a set, and the shared components that supply them | Implemented |
| [`09-ACCESSIBILITY-PRACTICE.md`](./09-ACCESSIBILITY-PRACTICE.md) | What is automated, what is not, and what is still owed | Partially implemented |
| [`10-FRONTEND-TESTING.md`](./10-FRONTEND-TESTING.md) | The three layers, the stubbing rule, mutation discipline | Implemented |
| [`11-BUILD-AND-DELIVERY.md`](./11-BUILD-AND-DELIVERY.md) | Vite build, environment injection, how the bundle reaches staging | Implemented |
| [`12-SCREEN-INVENTORY.md`](./12-SCREEN-INVENTORY.md) | Every route that exists today, its gate, and its implementation chain | Implemented |

## Related

- [`../UI-UX/`](../UI-UX/) — the design intent these documents implement.
- [`../ARCHITECTURE/03-FRONTEND-ARCHITECTURE.md`](../ARCHITECTURE/03-FRONTEND-ARCHITECTURE.md) — the one-page summary; this category is the detail behind it.
- [`../API/`](../API/) — the contract the console is generated from.
- [`../WEBSITE/`](../WEBSITE/) — the public site, which shares nothing with the console.
- [`../TESTING/`](../TESTING/) — the whole-repository test strategy.
