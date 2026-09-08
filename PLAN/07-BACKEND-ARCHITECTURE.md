# 07 — Backend Architecture

> The stack choices in this section are **initial recommendations**, not final decisions. Adjust based on your team's existing skill set.

## Core Language & Framework

| Component | Choice | Reason |
|---|---|---|
| Primary language | **Go** | High performance, native concurrency (goroutines) well suited for I/O-bound auth workloads, single binary is easy to deploy, mature OIDC/OAuth library ecosystem (`go-oidc`, `ory/fosite`, etc.) |
| HTTP framework | `chi` or plain `net/http` + middleware | Lightweight, not "magic," easy to reason about for security-critical code |
| OAuth2/OIDC library | `ory/fosite` (OAuth2 server framework) or `zitadel/oidc` | Battle-tested for implementing a *provider* (not just a client) |
| Authorization/policy engine | Open Policy Agent (OPA), embedded as a Go library | Avoids a network hop on every authz check — see `08-AUTHORIZATION.md` |

**Alternatives** if the team is more familiar with other languages:
- **TypeScript/Node.js** (NestJS) + `node-oidc-provider`.
- **Java/Kotlin** (Spring Authorization Server).

Guiding principle: **don't reinvent cryptography/OAuth protocols from scratch** — use well-tested libraries and focus effort on business logic (multi-tenancy, RBAC/ABAC, etc.).

## Database & Storage

| Need | Choice | Reason |
|---|---|---|
| Primary store | **PostgreSQL** | ACID, JSONB for flexible data (custom claims, metadata), mature for multi-tenancy |
| Cache & session | **Redis** | Fast session lookups, good fit for rate limiting & token blacklisting |
| Audit log / event store | Append-only `events` table in PostgreSQL to start; evaluate a separate event store later if volume grows | Simpler than full event sourcing to begin with |

## Cryptography

- **Password hashing**: Argon2id (current OWASP recommendation), bcrypt as fallback if compatibility is needed.
- **Token signing**: JWT using RS256/ES256 (asymmetric), not HS256 — so consumer services can verify tokens with a public key without a shared secret.
- **Key rotation**: multiple active signing keys via the JWKS endpoint, rotated periodically.

## Service Layout

The backend is a **modular monolith** at MVP: a single Go binary with clearly separated internal packages (authn, authz, management-api, oidc-provider) so it can be split into microservices later without a rewrite, if scaling truly requires it (see `12-PERFORMANCE.md`).

## Infrastructure

| Layer | Choice |
|---|---|
| Containerization | Docker |
| Orchestration | Kubernetes (or Docker Compose for MVP/small staging) |
| Ingress/Gateway | NGINX / Envoy / cloud load balancer, TLS termination here |
| Secret management | Vault / cloud secret manager |
| CI/CD | GitHub Actions |

Full deployment topology and environment strategy are in `14-DEPLOYMENT.md`.

## Decision Summary

| Area | Default Decision |
|---|---|
| Language | Go |
| DB | PostgreSQL |
| Cache | Redis |
| Tokens | JWT RS256, key rotation via JWKS |
| Password hashing | Argon2id |
| Policy engine | OPA (embedded) |
| Deployment | Docker + Kubernetes |

Continue to [08 — Authorization](./08-AUTHORIZATION.md).
