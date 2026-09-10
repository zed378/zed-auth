# 05 — Verification & Red Team Plan

This document defines how the threats catalogued in `02-ATTACK-SURFACE-AND-SCENARIOS.md` get **verified** against the actual system, before and after each release. It focuses on test scenarios and what "verified" looks like — it does not contain exploitation instructions against any third-party system, service, or infrastructure outside this project's own authorized environments.

## Scope

All verification activity described here targets **this system's own staging/pre-production environment**, under explicit authorization, following `PLAN/14-DEPLOYMENT.md`'s environment strategy. Nothing here is intended for, or should be applied to, systems the team does not own or have explicit written authorization to test.

## Verification Approach Per Threat Category

Rather than open-ended "penetration testing," each category in `02-ATTACK-SURFACE-AND-SCENARIOS.md` maps to a **specific, repeatable verification scenario** that can be automated where possible (feeding `PLAN/11-TESTING.md`'s security testing layer) and manually reviewed where automation isn't practical.

| Category | Verification scenario | Automatable? |
|---|---|---|
| Authentication attacks | Simulate credential-stuffing traffic against a staging environment, confirm rate limiting engages at the expected threshold | Yes — CI/scheduled job |
| Authorization bypass / IDOR | Automated test suite attempting resource-ID substitution across every endpoint accepting an ID parameter | Yes |
| Privilege escalation / delegation abuse | Automated test attempting to assign a role outside a Project Grant's `granted_role_keys`, and attempting to self-assign a `manager_role` without authorization | Yes |
| Session attacks | Automated test confirming session ID regenerates on login, cookie flags are correctly set | Yes |
| CSRF | Automated test confirming state-changing requests without a valid `state`/CSRF token are rejected | Yes |
| XSS | SAST + automated test injecting script payloads into every user-controlled text field, confirming safe rendering | Mostly yes |
| SSRF | Automated test attempting to point a server-side URL fetch (SAML metadata) at an internal/private address, confirming rejection | Yes |
| SQL/NoSQL injection | SAST rule enforcement + automated test injecting SQL metacharacters into filter/search parameters | Yes |
| File upload abuse | Automated test uploading a polyglot/mismatched-content-type file to the branding upload endpoint | Yes |
| API abuse / rate-limit bypass | Load test confirming rate limits hold under simulated distributed load (`PLAN/12-PERFORMANCE.md`) | Yes |
| Business-logic abuse | Manual review + targeted test of the specific race conditions identified per-feature (`PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md` §13/§14) | Partially |
| Enumeration | Automated test confirming identical response timing/content for valid vs. invalid accounts on login/reset flows | Yes |
| Credential stuffing / token leakage | Log-scanning audit confirming no tokens/passwords appear in application logs across a full test suite run | Yes |
| IDOR via client-side trust | Documentation review + reference-implementation test confirming the provided SDK/example never trusts client-supplied org context | Manual (documentation-focused) |
| Supply-chain / dependency risks | Automated dependency scanning on every build, SBOM diff review on every release | Yes |
| Secret exposure | Automated secret-scanning on every commit and CI log | Yes |
| Container/runtime security | Automated container image scanning, manual review of Kubernetes security policies | Mostly yes |
| CI/CD attack surface | Manual review of workflow permissions and branch protection rules, at least quarterly | Manual |
| Logging/audit integrity | Automated test confirming the application's DB role cannot `UPDATE`/`DELETE` on the `events` table | Yes |

## External Penetration Testing

- An external, professional pentest (`PLAN/09-SECURITY.md`, `PLAN/11-TESTING.md`) is required before Phase 5 go-live and periodically afterward — this is a distinct activity from the internal verification scenarios above, performed by a qualified third party under a signed engagement scope covering exclusively this system's owned environments.
- Findings from external pentests feed directly into `02-ATTACK-SURFACE-AND-SCENARIOS.md` (new scenarios discovered) and `PLAN/18-RISK-REGISTER.md`.

## Cadence

| Activity | Frequency |
|---|---|
| Automated verification scenarios (table above) | Every CI run / every release, where automatable |
| Manual verification scenarios | At least quarterly |
| External pentest | Before Phase 5 go-live, then at least annually |
| Full `SECURITY/` document review (all files) | At the start of every roadmap phase (`PLAN/16-IMPLEMENTATION-ROADMAP.md`), and whenever a new trust boundary is introduced |

## Definition of "Verified"

A threat scenario in `02-ATTACK-SURFACE-AND-SCENARIOS.md` is considered verified, not just documented, when it has a passing automated test (where automatable) or a completed, dated manual review (where not) — a threat model entry with no corresponding verification is a gap, and should be tracked as such in `PLAN/18-RISK-REGISTER.md`.

---

This concludes the `SECURITY/` folder. It complements, rather than replaces, `PLAN/09-SECURITY.md` (baseline controls) and `PLAN/10-THREAT-MODEL.md` (STRIDE summary) — those two documents now point here for the full detail.
