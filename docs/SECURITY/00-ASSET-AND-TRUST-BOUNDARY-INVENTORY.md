# 00 — Asset & Trust Boundary Inventory

This is the foundation of everything else in `SECURITY/`: you cannot reason about threats, mitigations, or detection without first being explicit about what you're protecting and where the trust boundaries actually sit. This expands on the trust-boundary diagram in `PLAN/10-THREAT-MODEL.md` into a full asset inventory.

## Asset Inventory

| Asset | Sensitivity | Where it lives | Why it matters |
|---|---|---|---|
| JWT signing private key | Critical | Secret manager, loaded into Auth Service memory only | Compromise means an attacker can forge tokens for *any* user in *any* organization — the single highest-value target in the whole system |
| User password hashes | Critical | `users.password_hash`, PostgreSQL | A leak enables offline cracking; even hashed, this is a severe breach for affected users |
| Refresh tokens (hashed) | High | `refresh_tokens.token_hash`, PostgreSQL | A leaked *raw* refresh token (not the hash) gives an attacker persistent access until revoked |
| Session cookies | High | Client browser, `sessions` table | A stolen session cookie gives immediate access without needing credentials |
| OIDC/SAML client secrets | High | `applications.client_secret_hash`, PostgreSQL | Compromise lets an attacker impersonate a legitimate application |
| Organization/user data (PII) | High | PostgreSQL, various tables | Regulatory exposure (GDPR/local data protection law, `PLAN/09-SECURITY.md`) plus direct harm to affected individuals |
| Audit log (`events` table) | High (integrity), Medium (confidentiality) | PostgreSQL, forwarded to SIEM | Tampering undermines every other control's accountability — see Repudiation in `PLAN/10-THREAT-MODEL.md` |
| Project Grant records | High | `project_grants`, `user_grants` | Governs cross-organization access; a manipulated grant can leak access between unrelated organizations |
| ABAC policy source (Rego) | High | `policies.rego_source` | A malicious/buggy policy change can silently over-grant or lock out access at scale |
| Management API access tokens (service accounts) | High | Not stored server-side beyond hashed refresh tokens; live only in the calling service | Compromise of a CI/CD or automation credential gives programmatic control over org/user management |
| Source code & CI/CD pipeline | High | GitHub, CI runners | A supply-chain compromise here can inject malicious code before it ever reaches production |

## Trust Boundaries (Expanded from `PLAN/10-THREAT-MODEL.md`)

| Boundary | Between | Why it's a boundary |
|---|---|---|
| TB-1 | Public internet ↔ Load balancer / TLS termination | Anyone on the internet can reach this edge; everything past it must assume hostile input |
| TB-2 | Load balancer ↔ Auth Service internals | Internal services trust requests differently once past TLS termination and basic edge filtering — but must still authenticate/authorize every request |
| TB-3 | Auth Service ↔ Consumer applications (resource servers) | Consumer apps trust tokens issued by Auth Service; a forged or over-privileged token crosses this boundary with severe consequence |
| TB-4 | Owning organization ↔ Granted (delegated) organization | Introduced by Project Grants (`PLAN/08-AUTHORIZATION.md` Part C) — a delegated organization is *not* fully trusted with the whole project, only the specifically granted subset |
| TB-5 | Auth Service application layer ↔ Data layer (PostgreSQL, Redis) | Row-level security and parameterized queries are the actual enforcement point for tenant isolation — application-code correctness alone is not sufficient |
| TB-6 | Developer/CI environment ↔ Production | Code, secrets, and dependencies cross this boundary; supply-chain and CI/CD attack surface live here |
| TB-7 | End-user device ↔ Auth Service (browser-based flows) | The browser itself is untrusted infrastructure (extensions, malware, shared devices) — this is where session/token theft from the client side originates |

## How to Use This Document

Every entry in `02-ATTACK-SURFACE-AND-SCENARIOS.md` should reference which asset and which trust boundary it concerns. Every new feature spec (`PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md` §14 "Abuse Cases") should identify which row(s) above it touches before deciding "not applicable."

Continue to [01 — Threat Actor Profiles](./01-THREAT-ACTOR-PROFILES.md).
