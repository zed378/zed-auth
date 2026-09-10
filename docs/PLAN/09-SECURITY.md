# 09 — Security

> **Note**: this document covers baseline security controls. For the full asset inventory, threat-actor profiles, per-scenario attack analysis (Asset → Trust Boundary → Threat Actor → Attack Surface → Scenario → Impact → Likelihood → Mitigation → Detection → Response), incident playbooks, and verification/red-team plan, see the dedicated **`SECURITY/`** folder.

The Auth Service is the most sensitive component in the entire system — a compromise here means a compromise of *every* application that depends on it. This section summarizes the minimum security controls that must be in place. Structured threat analysis (attacker-by-attacker) is in `10-THREAT-MODEL.md`, and the full scenario-by-scenario breakdown is in `SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`.

## Tokens & Keys

- **JWT signing key**: asymmetric (RS256/ES256); the private key **never** leaves the Auth Service.
- **Periodic key rotation** (e.g., every 90 days), with an overlap period so older tokens can still be verified.
- **Short-lived access tokens** (5–15 minutes), **refresh tokens** with a longer lifetime but revocable, stored as a hash (not plaintext).
- **Audience (`aud`) & issuer (`iss`) claims** strictly validated by the resource server.

## Passwords & Credentials

- Hashed with **Argon2id**, parameters tuned to server capacity.
- Check new passwords against a breached-password list (e.g. via a k-anonymity API) — reject known-leaked passwords.
- **Never** log passwords, even failed attempts.

## Transport & Storage

- **TLS mandatory** everywhere, no HTTP fallback.
- Session cookies: `HttpOnly`, `Secure`, `SameSite=Lax` (or `Strict` where UX allows).
- Sensitive DB data (e.g. OIDC client secrets) encrypted at rest.

## Protection Against Common Attacks

| Threat | Mitigation |
|---|---|
| Credential stuffing / brute force | Rate limiting per account & per IP, temporary lockout, CAPTCHA after N attempts |
| Token replay | Short-lived access tokens, refresh token rotation |
| CSRF | Mandatory `state` parameter, SameSite cookies |
| Open redirect | `redirect_uri` must be an exact match, never a prefix match |
| Authorization code interception | PKCE mandatory for all clients |
| Session fixation | Regenerate session ID after successful login |
| Privilege escalation via the API | Every management API endpoint validates the caller's permission against the scope of the resource being accessed |
| Delegation abuse | `role_keys` assigned by a receiving org must be validated as a subset of `granted_role_keys` (`08-AUTHORIZATION.md`) |

## Audit & Anomaly Detection

- Every sensitive event logged in `events` (`04-DATA-MODEL.md`): logins, role changes, user creation/deletion, policy changes.
- Audit log is **append-only**, ideally forwarded to an external SIEM.
- Automatic notifications for suspicious activity (new location/device, repeated failures).

## Secure Development Practices

- **Threat modeling** before implementing any major new phase (see `10-THREAT-MODEL.md`).
- **Automated dependency scanning** in CI (crypto/OAuth libraries especially).
- **External security review/pentest** before go-live, and periodically afterward.
- **Secure-by-default**: new features must be safe by default; an admin should consciously relax a setting, not the other way around.

## Compliance (If Relevant Later)

If this eventually serves enterprise/regulated clients, prepare documentation for SOC 2 (access controls, audit trail) and GDPR/local data protection law (right to erasure, portability). The audit log & data model already support this direction.

Continue to [10 — Threat Model](./10-THREAT-MODEL.md).
