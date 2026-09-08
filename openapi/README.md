# openapi/

`openapi.yaml` is the **single contract artifact** for the Management REST API.

Two things are generated from it, and neither is ever hand-maintained:

1. The console's typed API client (`console/`) — so a backend change not reflected here breaks the console's build, not its runtime.
2. The public API reference at `/docs/api-reference` (`public-site/`) — `CLAUDE.md` hard rule: never hand-write API reference documentation.

**Governing documents**: `PLAN/05-API-CONTRACT.md` (the contract itself), `PLAN/20-PUBLIC-SITE-ARCHITECTURE.md` § API Reference Generation.

CI validates the spec and fails if a committed generated client is stale relative to it (`P0-16`).
