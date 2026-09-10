# 02 — Requirements

## Functional Requirements

### Authentication
- FR-1: Users can log in with email/username + password.
- FR-2: Users can enable MFA via TOTP; WebAuthn/passkey support added in a later phase.
- FR-3: Users can log in via federated social providers (Google, Microsoft, GitHub) as an alternative to a local password.
- FR-4: A logged-in user accessing a second registered application does not need to authenticate again while their session is valid (SSO).
- FR-5: Users can view and revoke their own active sessions.
- FR-6: Administrators can require MFA for all users in their organization via policy.

### Authorization
- FR-7: Roles are defined per project and assigned to users via grants (RBAC) — see `08-AUTHORIZATION.md`.
- FR-8: A project can be delegated to another organization with a restricted subset of roles (Project Grant) — see `08-AUTHORIZATION.md`.
- FR-9: Fine-grained, attribute-conditional authorization decisions are supported via an optional policy engine (ABAC) — see `08-AUTHORIZATION.md`.
- FR-10: Any service can query a real-time authorization decision via `/v1/authz/check`.

### Multi-Tenancy
- FR-11: A single deployment (instance) can host multiple organizations, each with isolated users, projects, and settings.
- FR-12: Each organization can configure its own password policy, MFA requirement, and session lifetime.

### Management
- FR-13: All entities (organizations, projects, applications, users, roles, grants) support full CRUD via REST API.
- FR-14: All CRUD operations available via the API are also available through the management console.
- FR-15: Administrative actions are recorded in an audit log with actor, timestamp, and details.

## Non-Functional Requirements

| Category | Requirement |
|---|---|
| **Availability** | Auth Service is on the critical path of every consumer application; target availability should match or exceed the strictest consumer app's SLA. |
| **Latency** | `/oauth/token` and `/oauth/authorize` p95 latency target: see `12-PERFORMANCE.md` for specific numbers and load-testing plan. |
| **Scalability** | The service must scale horizontally with no shared in-memory state (see `03-ARCHITECTURE.md`, `07-BACKEND-ARCHITECTURE.md`). |
| **Security** | Full detail in `09-SECURITY.md` and `10-THREAT-MODEL.md`; summarized: short-lived tokens, asymmetric signing, Argon2id hashing, mandatory PKCE, TLS everywhere. |
| **Auditability** | Every identity/permission-changing event must be captured in an append-only log (`04-DATA-MODEL.md`). |
| **Portability** | No hard vendor lock-in — standard OIDC/OAuth/SAML rather than proprietary protocols, so consumer apps aren't tied to this specific implementation. |
| **Maintainability** | API-first design so the UI never contains business logic the API doesn't also expose (`06-FRONTEND-ARCHITECTURE.md`). |
| **Accessibility** | Management console meets WCAG 2.1 AA at minimum — see `UI-UX/13-ACCESSIBILITY.md`. |

## Constraints

- Migration from existing auth systems must be gradual (app by app), not a single cutover — see `01-PRODUCT-SCOPE.md`.
- No third-party dependency may hold the private signing key outside of Auth Service's own infrastructure/secret manager.
- The console must authenticate through the same OIDC flow as every other consumer application ("dogfooding") — see `06-FRONTEND-ARCHITECTURE.md`.

## Assumptions

- Initial deployment is single-tenant (one default organization) for internal use; the schema remains multi-tenant-ready from day one so a second organization can be activated without a migration (`04-DATA-MODEL.md`, `08-AUTHORIZATION.md`).
- Teams integrating with this service are comfortable working with OIDC/OAuth 2.1 concepts (Authorization Code + PKCE).

Continue to [03 — Architecture](./03-ARCHITECTURE.md).
