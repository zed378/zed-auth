# Category: SECURITY

Asset inventory, trust boundaries, threat actor profiles, attack surface & scenarios, security baselines, detection, incident playbooks, and red-team plans.

## Category Mandate

The `SECURITY/` directory defines the platform's **threat model and security posture**. It details asset classification, trust boundaries, vulnerability attack scenarios, defense-in-depth controls, security detection rules, and incident response playbooks.

## Documents in Category

| Document | Title | Description |
|---|---|---|
| `00-ASSET-AND-TRUST-BOUNDARY-INVENTORY.md` | Asset Inventory | Classification of sensitive data, keys, and trust zones. |
| `01-THREAT-ACTOR-PROFILES.md` | Threat Actor Profiles | Attacker profiles (External attacker, Malicious tenant, Insider). |
| `02-ATTACK-SURFACE-AND-SCENARIOS.md` | Attack Surface & Abuse Catalog | Catalog of abuse cases (SQLi, IDOR, Privilege Escalation, JWT forgery). |
| `03-SECURITY-CONTROLS-BASELINE.md` | Security Baseline Controls | Mandatory security controls (TLS 1.3, AES-256, bcrypt/argon2, CSP). |
| `04-DETECTION-AND-MONITORING.md` | Detection & Alerting | SIEM rules, anomaly detection (brute force, credential stuffing). |
| `05-INCIDENT-RESPONSE-PLAYBOOKS.md` | Incident Response Playbooks | Incident response workflows for key leakage, tenant breach, token theft. |
| `06-VERIFICATION-AND-REDTEAM-PLAN.md` | Red-Team Verification Plan | SAST (`gosec`), DAST, penetration testing, automated abuse tests. |
