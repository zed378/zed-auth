# 02 - TypeScript Client SDK Specification

> Category: **SDK** (`docs/SDK/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Detail TypeScript client library (`@auth/sdk`) for web browsers and Node.js runtimes.

## Category Mandate

Delivers fully typed API client for interacting with the REST Management API.

## Key Topics To Specify

- Complete TypeScript interface definitions for all request/response models.
- PKCE OAuth 2.1 flow helper methods (`client.loginWithRedirect()`, `client.handleCallback()`).

## Reference Architecture & Specification

TypeScript Usage Example:
```typescript
import { AuthClient } from '@auth/sdk';
const auth = new AuthClient({ domain: 'auth.example.com', clientId: 'app_123' });
await auth.loginWithRedirect();
```

## Acceptance Criteria

- [x] TypeScript client API specified.
- [x] PKCE authentication helper methods documented.

## Open Questions

None.

## Related Documents

- `docs/SDK/00-SDK-ARCHITECTURE.md`
