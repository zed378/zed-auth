# SDK

This category documents client-side integration tooling for the Auth Service: official SDKs, a React auth provider, and HTTP middleware. Almost none of it is built. The generated TypeScript client the console uses internally is the one exception, and it is not a published package. Where a document specifies unbuilt work, it states what an integrator should do today instead — generate a client from `openapi/openapi.yaml`, use a standard OIDC library, and follow `docs/DEVELOPER/03-INTEGRATION-GUIDE.md` — before specifying testable acceptance criteria for the future SDK. This category does not restate the API contract (`docs/API/`) or the authentication/authorization model (`docs/PLAN/08-AUTHORIZATION.md`, `docs/AUTHORIZATION/`); it links to both.

## Documents

| File | Topic | Status |
|---|---|---|
| [`00-SDK-ARCHITECTURE.md`](./00-SDK-ARCHITECTURE.md) | What exists today vs. a future SDK family; constraints and acceptance criteria | Draft specification |
| [`01-GO-SDK.md`](./01-GO-SDK.md) | Go client: none published; what to do today; future spec | Draft specification |
| [`02-TYPESCRIPT-SDK.md`](./02-TYPESCRIPT-SDK.md) | The console's generated TypeScript client (internal, unpublished) and how it is built | Partially implemented |
| [`03-REACT-AUTH-PROVIDER-AND-HOOKS.md`](./03-REACT-AUTH-PROVIDER-AND-HOOKS.md) | React auth provider/hooks: none published; the console's own implementation as reference; future spec | Draft specification |
| [`04-HTTP-MIDDLEWARE-SPECIFICATION.md`](./04-HTTP-MIDDLEWARE-SPECIFICATION.md) | Backend middleware for token validation: none published; what to do today; future spec | Draft specification |

## Related Documents

- `docs/DEVELOPER/03-INTEGRATION-GUIDE.md` — the actual, implemented integration path.
- `docs/API/` — the REST contract any SDK would wrap.
- `openapi/openapi.yaml`, `openapi/README.md` — the single source of truth for the contract.
- `docs/PLAN/06-FRONTEND-ARCHITECTURE.md` — the console's own generation pipeline.
- `demo/` — the two reference consumer applications integrators can read (not import).
