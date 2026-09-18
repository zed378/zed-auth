# Developer

Two audiences, deliberately in one place: people working **on** this service, and people integrating an application **with** it. Both need the same facts about how it behaves; only the starting point differs.

For the design intent behind any of it, follow the links into [`../PLAN/`](../PLAN/). For the endpoint-level contract, [`../API/`](../API/).

## Documents

| File | Topic | Status |
|---|---|---|
| [`00-GETTING-STARTED.md`](./00-GETTING-STARTED.md) | The five concepts, the repository layout, what to read first | Implemented |
| [`01-LOCAL-DEVELOPMENT-SETUP.md`](./01-LOCAL-DEVELOPMENT-SETUP.md) | Running the stack, the test commands, regenerating generated code | Implemented |
| [`02-TASK-CONVENTIONS-AND-WORKFLOW.md`](./02-TASK-CONVENTIONS-AND-WORKFLOW.md) | Card to branch to gate to staging to record | Implemented |
| [`03-INTEGRATION-GUIDE.md`](./03-INTEGRATION-GUIDE.md) | Registering an application, signing users in, validating tokens, checking permissions | Implemented |

## Related

- `CLAUDE.md` — the documentation map for AI agents.
- `TASKS/` — the execution plan; `MEMORY/` — decisions, specs and records.
- [`../SDK/`](../SDK/) — what client-side support exists today.
