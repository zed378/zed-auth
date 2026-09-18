# Architecture

The system as it is deployed and as the code is organised: the units that run, the boundaries between them, and the internal structure of the Go service, the console and the public site. `docs/PLAN/03-ARCHITECTURE.md`, `06-FRONTEND-ARCHITECTURE.md`, `07-BACKEND-ARCHITECTURE.md` and `20-PUBLIC-SITE-ARCHITECTURE.md` hold the design intent; these documents describe what exists and cite it.

Operational concerns — environments, images, CI, deployment and recovery — are in [`../DEVOPS/`](../DEVOPS/).

## Documents

| File | Topic | Status |
|---|---|---|
| [`00-SYSTEM-ARCHITECTURE.md`](./00-SYSTEM-ARCHITECTURE.md) | Deployable units, request path, storage | Implemented |
| [`01-SERVICE-BOUNDARIES.md`](./01-SERVICE-BOUNDARIES.md) | What each unit owns, and the gates that enforce it | Implemented |
| [`02-BACKEND-ARCHITECTURE.md`](./02-BACKEND-ARCHITECTURE.md) | Binaries, packages, layering rules | Implemented |
| [`03-FRONTEND-ARCHITECTURE.md`](./03-FRONTEND-ARCHITECTURE.md) | The management console as a client of the contract | Implemented |
| [`04-PUBLIC-SITE-ARCHITECTURE.md`](./04-PUBLIC-SITE-ARCHITECTURE.md) | The public site and its generated API reference | Implemented |

## Related

- [`../API/`](../API/) — the contract the generated router serves.
- [`../DATABASE/`](../DATABASE/) — the schema behind the storage layer.
- [`../MULTI-TENANCY/`](../MULTI-TENANCY/) — how one database serves many tenants safely.
