# 01 — Threat Actor Profiles

Different actors have different capabilities, motivations, and access starting points. Modeling them explicitly prevents the common mistake of designing controls only against "a generic hacker" and missing insider or partner-originated risk — both of which are especially relevant given this system's multi-tenant delegation model (`PLAN/08-AUTHORIZATION.md` Part C).

## Actor 1: Anonymous External Attacker

- **Starting access**: none — reaches only public endpoints (`/oauth/*`, public marketing/login pages).
- **Motivation**: credential theft, account takeover, opportunistic exploitation of known CVEs.
- **Relevant attack surface**: `02-ATTACK-SURFACE-AND-SCENARIOS.md` — authentication attacks, credential stuffing, enumeration, SSRF/injection against any public input.

## Actor 2: Authenticated End User (Malicious or Compromised Account)

- **Starting access**: a valid session/token for their own account within their own organization.
- **Motivation**: privilege escalation, accessing other users' data (IDOR), abusing business logic (e.g. exploiting a Project Grant edge case).
- **Relevant attack surface**: authorization bypass/IDOR, business-logic abuse, session attacks.

## Actor 3: Malicious Organization Admin (Insider Within an Org)

- **Starting access**: legitimate `ORG_ADMIN`/`ORG_OWNER` privileges for their own organization.
- **Motivation**: exceeding their organization's intended boundary — e.g. attempting to access another organization's data, or escalating to instance-level access.
- **Relevant attack surface**: cross-tenant data leak (`SECURITY/00-...` TB-5), privilege escalation, API abuse using legitimate credentials in unintended ways.

## Actor 4: Malicious or Compromised Granted (Delegated) Organization

- **Starting access**: a legitimate `PROJECT_GRANT_OWNER` role at their own organization, scoped to a specific delegated project and role subset (`PLAN/08-AUTHORIZATION.md` Part C).
- **Motivation**: assigning themselves or their users roles beyond what was granted; using their limited legitimate access as a foothold to probe for broader access.
- **Relevant attack surface**: this is the actor type most specific to this system's design (TB-4 in `00-ASSET-AND-TRUST-BOUNDARY-INVENTORY.md`) — every Project Grant feature must be threat-modeled against this actor specifically, not just generic "authorized user."

## Actor 5: Compromised Consumer Application / Resource Server

- **Starting access**: a legitimate client's `client_id`/secret or a valid access token obtained from a legitimate user flow.
- **Motivation**: token replay, using a token beyond its intended `aud`, exfiltrating tokens from logs/storage.
- **Relevant attack surface**: token leakage, client-side trust issues, insecure direct object access (if the resource server itself trusts client input over verified token claims).

## Actor 6: Malicious Insider (Auth Service Operator/Developer)

- **Starting access**: production infrastructure access, database access, or CI/CD pipeline access.
- **Motivation**: direct data exfiltration, planting a backdoor, disabling audit logging before an abuse.
- **Relevant attack surface**: secret exposure, CI/CD attack surface, logging/audit integrity, container/runtime security.

## Actor 7: Supply-Chain Attacker

- **Starting access**: none directly — targets a dependency (npm/Go package), base container image, or CI action used in the build pipeline.
- **Motivation**: inject malicious code that ships to production undetected.
- **Relevant attack surface**: supply-chain/dependency risks, CI/CD attack surface.

## How to Use This Document

Every scenario in `02-ATTACK-SURFACE-AND-SCENARIOS.md` is tagged with the actor(s) capable of executing it — a mitigation effective against Actor 1 (e.g. rate limiting) may do nothing against Actor 3 or 4, who already hold legitimate credentials.

Continue to [02 — Attack Surface & Scenarios](./02-ATTACK-SURFACE-AND-SCENARIOS.md).
