# 11 — Testing

Because this is a critical security component, testing isn't just about "making sure the feature works" — it's also about "making sure there are no security gaps." Abuse scenarios here are cross-referenced from `10-THREAT-MODEL.md`.

## Testing Pyramid

```
        ┌─────────────────────┐
        │   Security Testing   │  (pentest, token fuzzing, etc.)
        ├─────────────────────┤
        │  E2E / Flow Testing  │  (full login flow via automated browser)
        ├─────────────────────┤
        │  Integration Testing │  (real API + DB + Redis)
        ├─────────────────────┤
        │     Unit Testing      │  (pure logic: hashing, JWT, validation)
        └─────────────────────┘
```

## Unit Testing
- Password hashing & verification logic.
- JWT creation & verification logic (correct claims, correct expiry, correct signature).
- Input validation (email format, redirect_uri matching, role key format).
- RBAC/ABAC decision logic (pure permission checks, no DB) — including Project Grant scoping (`role_keys` ⊆ `granted_role_keys`).

## Integration Testing
- API endpoints tested with a real database & Redis (via testcontainers or similar).
- Full flow: create organization → create project → create application → create user → assign role → log in → verify token claims are correct.
- Full delegation flow: create Project Grant → receiving org assigns an allowed role → verify a disallowed role is rejected.

## End-to-End (E2E) Testing
- Simulate the full login flow via automated browser (Playwright), including:
  - Successful login → correct redirect with a code.
  - SSO: log in on App A, open App B, no re-login required.
  - MFA: correct password but wrong TOTP → rejected.
  - Logout: session is truly gone, subsequent access requires logging in again.
- Console flows (shared setup with `06-FRONTEND-ARCHITECTURE.md`): invite + first role assignment, Project Grant creation/revocation, session revocation.

## Security Testing

Directly test the abuse scenarios identified in `10-THREAT-MODEL.md`:
- Token with wrong `aud` rejected by the resource server.
- Non-exact-match `redirect_uri` rejected.
- Used (rotated) refresh token cannot be reused (replay detection).
- Rate limiting actually triggers under simulated brute force.
- A receiving organization cannot assign a role outside its Project Grant's `granted_role_keys`.
- A revoked Project Grant immediately invalidates access.
- Row-level security actually prevents cross-org data leaks, independent of application-layer filtering.

Plus general practices:
- **SAST** in CI for Go code (e.g. `gosec`).
- **Dependency scanning** for CVEs, especially crypto/OAuth libraries.
- **Fuzz testing** for parsers accepting external input (SAML assertions, JWTs).
- **External pentest** before go-live and periodically after (`16-IMPLEMENTATION-ROADMAP.md` Phase 5).

## Load & Performance Testing

Covered in detail in `12-PERFORMANCE.md`; summarized here: load-test `/oauth/token` and `/oauth/authorize` (highest-traffic, critical-path endpoints), and test graceful degradation when DB/Redis is under stress.

## "Production-Ready" Criteria

- [ ] Every flow in Phase 1 & 2 (`16-IMPLEMENTATION-ROADMAP.md`) has an integration test & E2E test passing in CI.
- [ ] SAST & dependency scans have no unaddressed critical/high findings.
- [ ] Load testing meets the latency targets in `12-PERFORMANCE.md`.
- [ ] External pentest results reviewed and critical findings remediated.
- [ ] All abuse scenarios in `10-THREAT-MODEL.md` §"High-Priority Abuse Scenarios" have a corresponding automated test.

Full sign-off criteria per module are in `17-ACCEPTANCE-CRITERIA.md`.

Continue to [12 — Performance](./12-PERFORMANCE.md).
