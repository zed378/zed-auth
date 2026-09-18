# Category: PLAN

The engineering plan: what is being built, why, and to what standard. Written before implementation and amended only through the deliberate plan-change process (`AGENTS.md` rule 9, `.github/CODEOWNERS`).

Everything in the newer domain categories (`API/`, `ARCHITECTURE/`, `DATABASE/`, and the rest) **describes what exists in the code**. These documents are the design intent behind it. Where the two differ, the difference is recorded in `MEMORY/DECISIONS.md` (ADRs) or `TASKS/BACKLOG.md` (plan gaps, `PG-xx`), never resolved silently.

## Documents

| Document | Covers |
|---|---|
| [`00-PROJECT-CONTEXT.md`](./00-PROJECT-CONTEXT.md) | The problem, the intended users, the shape of the product |
| [`01-PRODUCT-SCOPE.md`](./01-PRODUCT-SCOPE.md) | In scope, and — as importantly — out of scope |
| [`02-REQUIREMENTS.md`](./02-REQUIREMENTS.md) | Functional (FR-xx) and non-functional requirements |
| [`03-ARCHITECTURE.md`](./03-ARCHITECTURE.md) | System architecture and the major components |
| [`04-DATA-MODEL.md`](./04-DATA-MODEL.md) | The domain model and every table it implies |
| [`05-API-CONTRACT.md`](./05-API-CONTRACT.md) | API shape, conventions, and the endpoint surface |
| [`06-FRONTEND-ARCHITECTURE.md`](./06-FRONTEND-ARCHITECTURE.md) | The management console's architecture |
| [`07-BACKEND-ARCHITECTURE.md`](./07-BACKEND-ARCHITECTURE.md) | The service's internal architecture |
| [`08-AUTHORIZATION.md`](./08-AUTHORIZATION.md) | RBAC, Project Grants and ABAC together — the single source of truth for authorization |
| [`09-SECURITY.md`](./09-SECURITY.md) | Baseline security controls |
| [`10-THREAT-MODEL.md`](./10-THREAT-MODEL.md) | The threat model summary (the detail is in `../SECURITY/`) |
| [`11-TESTING.md`](./11-TESTING.md) | Test strategy and the layers it requires |
| [`12-PERFORMANCE.md`](./12-PERFORMANCE.md) | Latency and throughput targets |
| [`13-OBSERVABILITY.md`](./13-OBSERVABILITY.md) | Logging, metrics, tracing and alerting requirements |
| [`14-DEPLOYMENT.md`](./14-DEPLOYMENT.md) | Environments, releases, migrations (expand/contract) |
| [`15-DISASTER-RECOVERY.md`](./15-DISASTER-RECOVERY.md) | Backup, restore, RPO and RTO |
| [`16-IMPLEMENTATION-ROADMAP.md`](./16-IMPLEMENTATION-ROADMAP.md) | The phase plan the `TASKS/` cards execute |
| [`17-ACCEPTANCE-CRITERIA.md`](./17-ACCEPTANCE-CRITERIA.md) | What "done" means per phase |
| [`18-RISK-REGISTER.md`](./18-RISK-REGISTER.md) | Risks (`R-xx`) and their mitigations |
| [`19-FEATURE-SPECIFICATION-TEMPLATE.md`](./19-FEATURE-SPECIFICATION-TEMPLATE.md) | The template every non-trivial feature is specified against |
| [`20-PUBLIC-SITE-ARCHITECTURE.md`](./20-PUBLIC-SITE-ARCHITECTURE.md) | The public marketing and documentation site |

## Related

- Execution: [`../../TASKS/`](../../TASKS/) — phases, task cards, progress board, backlog.
- What was actually built: [`../../MEMORY/`](../../MEMORY/) — specs, records, decisions.
- The code as documented: [`../ARCHITECTURE/`](../ARCHITECTURE/), [`../API/`](../API/), [`../DATABASE/`](../DATABASE/) and the other domain categories.
