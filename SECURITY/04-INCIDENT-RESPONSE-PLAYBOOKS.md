# 04 — Incident Response Playbooks

Concrete, per-scenario response steps, expanding the "Response" column from `02-ATTACK-SURFACE-AND-SCENARIOS.md` into runnable playbooks. The general outage/data-loss runbook is in `PLAN/15-DISASTER-RECOVERY.md`; this document covers security-incident-specific playbooks.

## Playbook: Suspected Signing Key Compromise

1. **Immediately** generate a new signing key and add it to the JWKS alongside the (now suspect) old key.
2. Begin rejecting new token issuance signed with the old key while allowing a short grace window for in-flight token verification, per the rotation mechanics in `PLAN/09-SECURITY.md`.
3. Force-expire all refresh tokens issued under the suspect key's validity window.
4. Notify all consumer application teams that a forced token refresh is required.
5. Investigate how the key was exposed (`02-ATTACK-SURFACE-AND-SCENARIOS.md` §16) and close that vector before considering the incident closed.
6. Post-incident review, update `PLAN/18-RISK-REGISTER.md`.

## Playbook: Confirmed Privilege Escalation / Delegation Abuse

1. Immediately revoke the specific role/grant that was escalated (`PLAN/08-AUTHORIZATION.md`).
2. Pull the full audit trail (`PLAN/04-DATA-MODEL.md` `events` table) for everything the escalated privilege touched, from the moment of escalation to detection.
3. Assess whether any data was exfiltrated or modified using the escalated access; notify affected organizations per data-protection obligations if PII was exposed (`PLAN/09-SECURITY.md` Compliance section).
4. Patch the specific validation gap that allowed escalation (cross-reference `PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`'s "Abuse Cases" section for the affected feature — if this section was empty or inadequate for that feature, that's itself a process gap to fix).
5. Add a regression test reproducing the exact escalation path before closing the incident (`PLAN/11-TESTING.md`).

## Playbook: Audit Log Tampering Detected

1. Treat as **Critical** regardless of what else is found — tampering implies significant prior access (`02-ATTACK-SURFACE-AND-SCENARIOS.md` §19).
2. Immediately cross-reference the external SIEM copy (which the application itself cannot modify) to reconstruct the true event history.
3. Identify and revoke the credential/access path that allowed the tampering.
4. Full audit of all activity by the compromised credential/account, not just the tampered entries.
5. This incident category always warrants an external security review before considering it closed, given the severity.

## Playbook: Credential Stuffing Attack Detected

1. Confirm scope via the detection alert (`03-DETECTION-AND-MONITORING.md`) — how many accounts, what pattern.
2. Apply broader temporary rate limiting/CAPTCHA beyond normal thresholds for the duration of the attack.
3. Force password reset + session revocation for any account with a confirmed successful login from the attack pattern.
4. Notify affected users.
5. No code change is typically required (this is an expected, ongoing internet-wide threat) — but review whether existing rate limits (`PLAN/09-SECURITY.md`) held up as designed, and tune if not.

## Playbook: Supply-Chain / Dependency Compromise Detected

1. Immediately identify all services built with the compromised dependency version.
2. Roll back to the last known-good dependency set and redeploy (`PLAN/14-DEPLOYMENT.md` rollback strategy).
3. Rotate any credentials the compromised build had access to during its deployment window — treat this the same as a secret-exposure incident (`02-ATTACK-SURFACE-AND-SCENARIOS.md` §16).
4. Full review of what the compromised code path could have accessed or exfiltrated.
5. Add the compromised dependency/version to a permanent deny-list in the dependency scanning configuration.

## General Incident Response Principles (Apply to Every Playbook)

- **Contain before investigating exhaustively** — stopping ongoing harm takes priority over fully understanding root cause first, consistent with the DR runbook philosophy in `PLAN/15-DISASTER-RECOVERY.md`.
- **Every incident gets a post-incident review**, feeding `PLAN/18-RISK-REGISTER.md` with any newly identified systemic gap.
- **Communication**: affected consumer-app teams and, where required, affected end users/organizations are notified proportionate to the incident's actual impact — never silently patch a security issue that affected real user data without disclosure.

Continue to [05 — Verification & Red Team Plan](./05-VERIFICATION-AND-REDTEAM-PLAN.md).
