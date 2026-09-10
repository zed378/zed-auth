# 02 — Attack Surface & Scenarios

Every category below follows the same pipeline: **Asset → Trust Boundary → Threat Actor → Attack Surface → Attack Scenario → Impact → Likelihood → Mitigation → Detection → Response**. This is the core document of `SECURITY/` — it turns the generic "use HTTPS/JWT" level of thinking in `PLAN/09-SECURITY.md` into concrete, scenario-by-scenario reasoning.

> **Scope note**: this document models attack scenarios *against this system* for defensive planning and verification purposes. It is not an exploitation guide against third-party systems, and the verification plan in `05-VERIFICATION-AND-REDTEAM-PLAN.md` is scoped exclusively to authorized testing of this system's own infrastructure.

## 1. Authentication Attacks

| Field | Detail |
|---|---|
| Asset | User password hashes, session cookies |
| Trust Boundary | TB-1, TB-7 (`00-ASSET-AND-TRUST-BOUNDARY-INVENTORY.md`) |
| Threat Actor | Anonymous External Attacker |
| Attack Surface | `/oauth/token`, login form |
| Scenario | Credential stuffing using breached password lists against many accounts |
| Impact | Account takeover, potentially organization-wide if an admin account is hit |
| Likelihood | High (automated, low-cost for attacker) |
| Mitigation | Per-account and per-IP rate limiting, breached-password rejection, MFA (`PLAN/09-SECURITY.md`) |
| Detection | Spike in failed logins from distributed IPs against many distinct accounts (`PLAN/13-OBSERVABILITY.md` alerting) |
| Response | Automatic temporary lockout/cooldown; force password reset + session revocation for confirmed-compromised accounts |

## 2. Authorization Bypass / IDOR

| Field | Detail |
|---|---|
| Asset | Organization/user data, Project Grant records |
| Trust Boundary | TB-3, TB-4, TB-5 |
| Threat Actor | Authenticated End User, Malicious Org Admin, Malicious Granted Organization |
| Attack Surface | Any endpoint accepting a resource ID (`/v1/organizations/{org_id}/users/{user_id}`, etc.) |
| Scenario | A user substitutes another organization's or user's ID in a request, hoping authorization checks are missing or scoped incorrectly |
| Impact | Cross-tenant data leak, unauthorized access modification |
| Likelihood | Medium — depends entirely on implementation discipline, since this is a coding-correctness risk, not just a config one |
| Mitigation | Every endpoint validates the resource belongs to the caller's authorized scope, not just that the caller is "logged in"; row-level security as a defense-in-depth backstop (`PLAN/08-AUTHORIZATION.md`) |
| Detection | Anomalous cross-org access patterns in audit logs; automated tests specifically probing ID substitution (`PLAN/11-TESTING.md`) |
| Response | Immediate access revocation for the exploited path, audit log review to scope the actual data accessed |

## 3. Privilege Escalation (Including Delegation Abuse)

| Field | Detail |
|---|---|
| Asset | `manager_roles`, `user_grants`, `project_grants` |
| Trust Boundary | TB-4 |
| Threat Actor | Malicious Granted Organization, Authenticated End User |
| Attack Surface | Grant/role assignment endpoints |
| Scenario | A receiving organization assigns itself a role outside `granted_role_keys`, or a user finds a path to assign themselves a `manager_role` they shouldn't have |
| Impact | Access far beyond intended scope, potentially instance-wide |
| Likelihood | Medium |
| Mitigation | Server-side subset validation on every grant/role assignment (`PLAN/08-AUTHORIZATION.md` Part C, `PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md` worked example) |
| Detection | Automated tests attempting exactly this (`PLAN/11-TESTING.md`); audit log alerting on `manager_roles` changes, which should be rare events |
| Response | Immediate role/grant revocation, full audit trail review of everything the escalated privilege touched |

## 4. Session Attacks (Fixation, Hijacking)

| Field | Detail |
|---|---|
| Asset | Session cookies |
| Trust Boundary | TB-7 |
| Threat Actor | Anonymous External Attacker, Authenticated End User |
| Attack Surface | Login flow, session cookie handling |
| Scenario | Session fixation (attacker sets a known session ID before victim logs in) or hijacking (stolen cookie via XSS/network) |
| Impact | Full account takeover without needing credentials |
| Likelihood | Low if mitigations are correctly implemented |
| Mitigation | Session ID regenerated on login, `HttpOnly`/`Secure`/`SameSite` cookies, TLS everywhere (`PLAN/09-SECURITY.md`) |
| Detection | Session used from a sudden new IP/device inconsistent with prior pattern (`PLAN/09-SECURITY.md` anomaly detection) |
| Response | Force session invalidation, notify the affected user |

## 5. CSRF

| Field | Detail |
|---|---|
| Asset | Any state-changing action reachable via a browser session |
| Trust Boundary | TB-7 |
| Threat Actor | Anonymous External Attacker |
| Attack Surface | Any cookie-authenticated POST/PUT/DELETE endpoint |
| Scenario | A malicious page tricks a logged-in admin's browser into submitting a state-changing request |
| Impact | Unauthorized action performed as the victim (e.g. an unwanted role grant) |
| Likelihood | Low with mitigations in place |
| Mitigation | `state` parameter in OAuth flows, `SameSite` cookies, CSRF tokens on console form submissions (`PLAN/09-SECURITY.md`) |
| Detection | Difficult to detect after the fact; prevention is the primary control |
| Response | Same as Authorization Bypass response above, since the effect is equivalent |

## 6. XSS

| Field | Detail |
|---|---|
| Asset | Session cookies (if not `HttpOnly`), any rendered user-controlled data in the console |
| Trust Boundary | TB-7 |
| Threat Actor | Anonymous External Attacker, Authenticated End User (stored XSS via a field like display name) |
| Attack Surface | Any console screen rendering user-supplied text (org name, display name, application redirect URI shown in a list) |
| Scenario | Stored or reflected script injection via a form field that's later rendered unsanitized |
| Impact | Session/token theft, arbitrary action as the victim |
| Likelihood | Medium if output encoding discipline lapses anywhere in the console |
| Mitigation | Framework-level auto-escaping (React's default JSX escaping) treated as the baseline, not the sole control; explicit review for any `dangerouslySetInnerHTML`-equivalent usage; Content-Security-Policy headers |
| Detection | SAST/dependency scanning (`PLAN/11-TESTING.md`), CSP violation reports |
| Response | Patch the specific injection point, force-revoke sessions if exploitation is confirmed |

## 7. SSRF

| Field | Detail |
|---|---|
| Asset | Internal network reachability from Auth Service |
| Trust Boundary | TB-2, TB-5 |
| Threat Actor | Authenticated End User (via any feature that fetches a URL server-side) |
| Attack Surface | SAML metadata URL fetching, any future feature that fetches a URL supplied by an admin (e.g. webhook target validation, `PLAN/05-API-CONTRACT.md`) |
| Scenario | An admin-supplied URL is used to make the server fetch an internal-only address (cloud metadata endpoint, internal admin panel) |
| Impact | Internal network reconnaissance, potential credential theft from cloud metadata services |
| Likelihood | Low if URL-fetching features are deliberately restricted |
| Mitigation | Allow-list validation on any server-side URL fetch (SAML metadata, webhooks), block requests to private/link-local IP ranges by default |
| Detection | Outbound connection monitoring from Auth Service to unexpected internal addresses |
| Response | Disable the affected feature/endpoint immediately, audit what was reachable |

## 8. SQL/NoSQL Injection

| Field | Detail |
|---|---|
| Asset | Entire PostgreSQL database |
| Trust Boundary | TB-5 |
| Threat Actor | Anonymous External Attacker, Authenticated End User |
| Attack Surface | Any query built from user input (search filters, sort parameters) |
| Scenario | Unparameterized query construction allows arbitrary SQL execution |
| Impact | Full database compromise, including bypassing row-level security if achieved with sufficient privilege |
| Likelihood | Low if parameterized queries/ORM usage is enforced project-wide |
| Mitigation | Exclusive use of parameterized queries, SAST rules specifically flagging string-concatenated queries (`PLAN/11-TESTING.md`) |
| Detection | Unusual query patterns/errors in DB logs, WAF rules as a secondary layer |
| Response | Immediate patch, credential rotation for the DB user if compromise is confirmed, restore-from-backup evaluation (`PLAN/15-DISASTER-RECOVERY.md`) |

## 9. File Upload Abuse

| Field | Detail |
|---|---|
| Asset | Server filesystem/storage, other users via served malicious content |
| Trust Boundary | TB-2 |
| Threat Actor | Authenticated End User |
| Attack Surface | Organization logo upload (branding, `PLAN/01-PRODUCT-SCOPE.md`) — currently the only file-upload surface in scope |
| Scenario | Uploading a malicious file disguised as an image (polyglot file, embedded script) |
| Impact | Stored XSS if served without correct content-type enforcement, or storage abuse |
| Likelihood | Low, narrow surface |
| Mitigation | Strict content-type and file-signature validation (not just extension), re-encoding uploaded images server-side, serving from a separate cookieless domain/CDN |
| Detection | File-type validation failures logged and monitored for repeated attempts |
| Response | Remove the offending file, review what else the account did |

## 10. API Abuse & Rate-Limit Bypass

| Field | Detail |
|---|---|
| Asset | Service availability, per-`PLAN/12-PERFORMANCE.md` latency budget |
| Trust Boundary | TB-1, TB-2 |
| Threat Actor | Anonymous External Attacker, Compromised Consumer Application |
| Attack Surface | All public API endpoints |
| Scenario | Distributing requests across many IPs/accounts to bypass per-IP/per-account rate limits |
| Impact | Degraded availability for legitimate users, resource exhaustion |
| Likelihood | Medium |
| Mitigation | Rate limits scoped per `client_id`/API key in addition to per-IP (`PLAN/05-API-CONTRACT.md`), anomaly-based throttling |
| Detection | Aggregate request-rate metrics and alerting (`PLAN/13-OBSERVABILITY.md`) |
| Response | Temporary broader throttling, `client_id` suspension for confirmed abuse |

## 11. Business-Logic Abuse

| Field | Detail |
|---|---|
| Asset | Project Grant model, invitation flow |
| Trust Boundary | TB-4 |
| Threat Actor | Malicious Granted Organization, Authenticated End User |
| Attack Surface | Multi-step flows (`UI-UX/04-USER-FLOWS.md`) where intermediate states might be exploitable |
| Scenario | Exploiting a race condition between grant revocation and in-flight role assignment (`PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md` §13 edge case) |
| Impact | Access retained briefly past intended revocation |
| Likelihood | Low, but consequential given the access-control context |
| Mitigation | Transactional consistency on grant-dependent operations, short-TTL cache invalidation (`PLAN/08-AUTHORIZATION.md`) |
| Detection | Automated tests specifically targeting this race condition (`PLAN/11-TESTING.md`) |
| Response | Manual review and revocation of any access retained past the intended window |

## 12. Enumeration

| Field | Detail |
|---|---|
| Asset | User/organization existence information |
| Trust Boundary | TB-1 |
| Threat Actor | Anonymous External Attacker |
| Attack Surface | Login form, invite flow, password reset |
| Scenario | Differing error messages/timing reveal whether an email/username exists |
| Impact | Enables targeted phishing or credential-stuffing against confirmed-valid accounts |
| Likelihood | Medium if not deliberately mitigated |
| Mitigation | Generic error messages and constant-time responses for login/reset flows regardless of whether the account exists |
| Detection | Difficult; primarily a prevention-focused control |
| Response | N/A beyond standard monitoring |

## 13. Credential Stuffing & Token Leakage

Covered jointly with Authentication Attacks (§1) and Session Attacks (§4) above; token leakage specifically also covers: tokens accidentally logged (`PLAN/13-OBSERVABILITY.md`'s explicit prohibition), tokens cached insecurely by a consumer app, and tokens exposed via overly permissive CORS configuration.

## 14. Insecure Direct Object Access (Client-Side Trust)

| Field | Detail |
|---|---|
| Asset | Any data a resource server serves based on client-supplied identifiers |
| Trust Boundary | TB-3 |
| Threat Actor | Compromised Consumer Application, Authenticated End User |
| Attack Surface | Consumer applications trusting client-supplied role/org context instead of verified token claims |
| Scenario | A consumer app's frontend sends an `org_id` parameter that its backend trusts without cross-checking against the verified token claim |
| Impact | Cross-tenant access at the consumer-app level — technically outside Auth Service's own codebase, but a direct consequence of how it documents its contract |
| Likelihood | Medium — this is a common integration mistake by teams consuming this service |
| Mitigation | Clear integration documentation (`PLAN/05-API-CONTRACT.md`) stressing that authorization context must always come from verified token claims, never client-supplied parameters; provide SDK/reference implementations that enforce this correctly by default |
| Detection | Not directly observable by Auth Service; requires consumer-app-side auditing |
| Response | Update integration guidance, notify affected consumer app teams |

## 15. Supply-Chain / Dependency Risks

| Field | Detail |
|---|---|
| Asset | Source code, build pipeline, production binaries |
| Trust Boundary | TB-6 |
| Threat Actor | Supply-Chain Attacker |
| Attack Surface | Go module dependencies, npm dependencies for the console, base container images, GitHub Actions used in CI |
| Scenario | A compromised or malicious dependency version is pulled into a build |
| Impact | Arbitrary code execution in production, potentially the most severe category since it can bypass every other control simultaneously |
| Likelihood | Low per individual dependency, but the aggregate surface (many dependencies) makes it a persistent background risk |
| Mitigation | Automated dependency scanning (`PLAN/09-SECURITY.md`), pinned versions with deliberate update review, minimal base images, verified/pinned GitHub Actions (not `@main`/`@latest`) |
| Detection | Dependency scanning alerts, SBOM (Software Bill of Materials) diffing between releases |
| Response | Immediate rollback to the last known-good dependency set, incident review of what the compromised dependency had access to |

## 16. Secret Exposure

| Field | Detail |
|---|---|
| Asset | Signing keys, DB credentials, client secrets |
| Trust Boundary | TB-6 |
| Threat Actor | Malicious Insider, Supply-Chain Attacker |
| Attack Surface | Source repositories, CI logs, environment configuration |
| Scenario | A secret accidentally committed to a repository or printed in a CI log |
| Impact | Depends on the secret — signing key exposure is Critical (see Asset Inventory, `00-ASSET-AND-TRUST-BOUNDARY-INVENTORY.md`) |
| Likelihood | Medium — one of the most common real-world incident categories industry-wide |
| Mitigation | Secret scanning in CI (pre-commit and pipeline-level), secrets never in code/env plaintext (`PLAN/09-SECURITY.md`), least-privilege secret access |
| Detection | Automated secret-scanning tools, monitoring for unexpected usage of a given credential |
| Response | Immediate credential rotation, audit of everything accessible with the exposed secret during its exposure window |

## 17. Container / Runtime Security

| Field | Detail |
|---|---|
| Asset | Running containers, host infrastructure |
| Trust Boundary | TB-2, TB-6 |
| Threat Actor | Malicious Insider, an attacker who has achieved code execution via another vector |
| Attack Surface | Container images, Kubernetes configuration (`PLAN/14-DEPLOYMENT.md`) |
| Scenario | A container running with excessive privileges is used to escape to the host or access other workloads |
| Impact | Lateral movement beyond the initially compromised service |
| Likelihood | Low with correct hardening |
| Mitigation | Non-root container users, read-only filesystems where possible, minimal base images, Kubernetes network policies restricting pod-to-pod traffic |
| Detection | Runtime security monitoring (unexpected process execution, privilege escalation attempts within a container) |
| Response | Isolate/kill the affected pod, rotate any credentials it had access to |

## 18. CI/CD Attack Surface

| Field | Detail |
|---|---|
| Asset | Build pipeline, deployment credentials |
| Trust Boundary | TB-6 |
| Threat Actor | Malicious Insider, Supply-Chain Attacker |
| Attack Surface | GitHub Actions workflows, deployment credentials stored in CI |
| Scenario | A malicious pull request modifies a workflow file to exfiltrate secrets during CI execution |
| Impact | Full production deployment compromise |
| Likelihood | Low with correct branch protection and workflow review requirements |
| Mitigation | Required review for workflow file changes, minimal-scope CI credentials, no secrets exposed to workflows triggered by forked-repo pull requests |
| Detection | CI audit logs, alerting on workflow file changes |
| Response | Immediate credential rotation for anything the pipeline had access to, review of recent deployments |

## 19. Logging / Audit Integrity

| Field | Detail |
|---|---|
| Asset | The `events` audit log itself |
| Trust Boundary | TB-5, TB-6 |
| Threat Actor | Malicious Insider, an attacker who has achieved database access via another vector |
| Attack Surface | Direct database access, application code paths that write to `events` |
| Scenario | An attacker with sufficient access deletes or modifies audit log entries to cover their tracks |
| Impact | Undermines every other control's accountability (this is exactly the Repudiation category in `PLAN/10-THREAT-MODEL.md`) |
| Likelihood | Low but high-impact — a determined insider or an attacker who has already achieved significant access |
| Mitigation | Append-only enforcement at the database level (no `UPDATE`/`DELETE` grants on the `events` table for the application's normal DB role), forwarding to an external SIEM the application itself cannot modify |
| Detection | Integrity checks comparing local audit log against the external SIEM copy; alerting on any attempted write outside the expected insert path |
| Response | Treat as a critical incident regardless of what else was found — audit tampering implies the attacker had significant access and time |

Continue to [03 — Detection & Monitoring](./03-DETECTION-AND-MONITORING.md).
