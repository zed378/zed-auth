# Phase 5 — Hardening & Production Scale

**Goal**: establish that the system is genuinely ready to be the single point of failure for every application that depends on it — verified by external parties and by drills, not by internal confidence.

**Why this phase is not optional**: every prior phase proved that features work. This phase proves the system holds under attack, under load, and under failure. `PLAN/09-SECURITY.md` opens with the reason: "a compromise here means a compromise of *every* application that depends on it."

**Prerequisite**: Phase 4 exit checklist satisfied. Phase 4b either completed or explicitly declined in `MEMORY/DECISIONS.md`.

**Roadmap reference**: `PLAN/16-IMPLEMENTATION-ROADMAP.md` § Phase 5.

---

## Task Summary

| ID | Task | Surface | Size | Depends on |
|---|---|---|---|---|
| P5-01 | Internal security verification sweep | backend | L | Phase 4 exit |
| P5-02 | External penetration test | all | L | P5-01 |
| P5-03 | Pentest remediation and re-test | all | L | P5-02 |
| P5-04 | Load testing against `PLAN/12` targets | backend, infra | L | Phase 4 exit |
| P5-05 | Degraded-dependency and graceful-degradation testing | backend, infra | L | P5-04 |
| P5-06 | Horizontal scale-out verification | infra | M | P5-04 |
| P5-07 | Disaster recovery drill | infra | L | P0-20 |
| P5-08 | Detection and monitoring completion | backend, infra | L | P0-11, P1-14 |
| P5-09 | Incident response playbook rehearsal | all | M | P5-08 |
| P5-10 | Console — audit log filtering and export polish | console | M | P1-24 |
| P5-11 | Console — accessibility audit and remediation | console | L | all console tasks |
| P5-12 | Console — performance pass | console | M | all console tasks |
| P5-13 | Public security and trust page | public-site | M | P5-03 |
| P5-14 | Integrator documentation completion | docs | L | Phase 4 exit |
| P5-15 | Risk register reconciliation | docs | M | P5-03 |
| P5-16 | Production launch readiness review | all | M | all above |

---

## P5-01 — Internal Security Verification Sweep

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | Phase 4 exit |
| **Plan refs** | `SECURITY/05-VERIFICATION-AND-REDTEAM-PLAN.md`, `SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` (all 19 categories), `PLAN/10-THREAT-MODEL.md` |
| **Spec required** | No |
| **Surface** | backend |

**Goal** — Walk every one of `SECURITY/02`'s nineteen attack categories against the built system and confirm each has both a mitigation and a test — before paying an external firm to find what an afternoon of internal review would have caught.

**Steps**
1. Work through `SECURITY/02` category by category: authentication attacks, authorization bypass/IDOR, privilege escalation including delegation abuse, session attacks, CSRF, XSS, SSRF, injection, file upload abuse, API abuse and rate-limit bypass, business-logic abuse, enumeration, credential stuffing and token leakage, insecure direct object access, supply-chain, secret exposure, container/runtime, CI/CD, and audit integrity.
2. For each, record: the implemented mitigation, the test that proves it, and the detection that would catch an attempt.
3. Follow `SECURITY/05`'s verification approach per threat category and its "Definition of Verified."
4. Confirm every high-priority abuse scenario in `PLAN/10` has an automated test — this is a `PLAN/11` production-ready criterion.
5. Run a full dependency audit across backend, console, and public site.
6. Re-run SAST with the strictest available ruleset and triage every finding, including those below the CI failure threshold.
7. Review secret handling end to end: rotation runbooks exercised, no secret in any image, log, or repository.
8. Review the container and Kubernetes configuration against `SECURITY/02` §17.

**Definition of Done**
- [ ] All nineteen `SECURITY/02` categories have a documented mitigation, test, and detection.
- [ ] Every `PLAN/10` high-priority abuse scenario has a passing automated test.
- [ ] Dependency audit is clean of critical and high findings across all three surfaces.
- [ ] Every secret rotation runbook has been executed at least once.
- [ ] Container and orchestration configuration is reviewed against `SECURITY/02` §17.
- [ ] Findings are logged in `PLAN/18-RISK-REGISTER.md` — via the deliberate-edit process, since it is a plan document.

---

## P5-02 — External Penetration Test

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P5-01 |
| **Plan refs** | `PLAN/09-SECURITY.md` § Secure Development Practices, `SECURITY/05-VERIFICATION-AND-REDTEAM-PLAN.md` § External Penetration Testing, `PLAN/16-IMPLEMENTATION-ROADMAP.md` § Phase 5 |
| **Spec required** | No |
| **Surface** | all |

**Goal** — Independent adversarial testing by people who did not build the system and do not share its authors' assumptions.

**Steps**
1. Scope per `SECURITY/05`: the OIDC and SAML surfaces, the Management API, the console, multi-tenant isolation, and the delegation model.
2. Give testers the threat model and the architecture. A grey-box test finds more in the same time than a blind one, and the goal is finding problems rather than proving they are hard to find.
3. Provide a dedicated, production-like environment with representative data. Never point a pentest at production, and never populate a test environment with real user data.
4. Direct explicit attention at the areas this project's own documents flag as highest risk: cross-tenant isolation (`PLAN/08` Part B), delegation subset validation (`PLAN/08` Part C), token handling, and session management.
5. Require a written report with severity ratings and reproduction steps.
6. Include the public site in scope — it is a separate surface with its own risks.

**Definition of Done**
- [ ] The test is complete with a written report.
- [ ] Scope covered every area named in `SECURITY/05`.
- [ ] Testers had threat model and architecture access.
- [ ] No production data was used.
- [ ] The report is filed and its findings are tracked as tasks.

---

## P5-03 — Pentest Remediation and Re-Test

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P5-02 |
| **Plan refs** | `PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 5, `PLAN/18-RISK-REGISTER.md`, `PLAN/11-TESTING.md` |
| **Spec required** | Depends on findings |
| **Surface** | all |

**Goal** — Every critical and high finding either remediated or carrying a documented, accepted-risk sign-off — the exact wording of `PLAN/17`'s Phase 5 criterion.

**Steps**
1. Triage every finding by severity and exploitability.
2. Remediate all critical and high findings.
3. For anything not remediated, record an explicit accepted-risk sign-off in `PLAN/18-RISK-REGISTER.md` with owner, rationale, compensating controls, and a review date. "Accepted risk" without an owner and a date is just an unfixed bug with better vocabulary.
4. **Add a regression test for every finding.** A vulnerability fixed without a test is a vulnerability scheduled to return.
5. Request a re-test of remediated findings by the same testers.
6. Feed structural findings back into `SECURITY/02` and `PLAN/10` — through the deliberate plan-edit process, since those are reference documents.

**Definition of Done**
- [ ] Every critical and high finding is remediated or has a signed-off accepted risk in `PLAN/18`.
- [ ] Every finding has a regression test.
- [ ] Re-test confirms the remediations.
- [ ] Structural lessons are reflected in the threat model documents.

---

## P5-04 — Load Testing Against `PLAN/12` Targets

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | Phase 4 exit |
| **Plan refs** | `PLAN/12-PERFORMANCE.md` § Latency Targets, § Load Testing Plan, `PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 5 |
| **Spec required** | No |
| **Surface** | backend, infra |

**Goal** — Meet every latency target in `PLAN/12`'s table under realistic load, measured rather than assumed.

**Steps**
1. Load-test each endpoint against its specific target: `/oauth/token` (p50 < 50ms, p95 < 200ms, p99 < 400ms), `/oauth/authorize` on the silent-SSO path (p50 < 50ms, p95 < 150ms, p99 < 300ms), `/oauth/userinfo`, `/v1/authz/check` for both RBAC and ABAC, and the Management API CRUD paths.
2. Run the **mixed workload** test `PLAN/12` requires: token issuance, management API traffic, and authz checks simultaneously — production never sees one traffic type in isolation.
3. Use realistic data volumes: many organizations, many users, many grants. Performance against an empty database measures nothing.
4. Model realistic concurrency from expected consumer application behavior; `PLAN/12` notes authz checks are usually the highest-volume traffic, since they happen on every protected request across every consumer app.
5. Profile whatever misses target rather than adding hardware first — an unexplained bottleneck at this scale becomes an unexplained outage at the next.
6. Record results against each target and re-run after every optimization.

**Definition of Done**
- [ ] Every latency target in `PLAN/12`'s table is met, or the gap is documented and explicitly accepted.
- [ ] The mixed workload test passes.
- [ ] Testing used realistic data volumes.
- [ ] Results are recorded in `MEMORY/` with the conditions they were measured under.

---

## P5-05 — Degraded-Dependency and Graceful-Degradation Testing

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P5-04 |
| **Plan refs** | `PLAN/12-PERFORMANCE.md` § Load Testing Plan, `PLAN/13-OBSERVABILITY.md` § Graceful Degradation Behavior |
| **Spec required** | No |
| **Surface** | backend, infra |

**Goal** — Confirm the service degrades rather than collapses, and that every fail-safe fails in the safe direction.

**Steps**
1. Test the scenarios `PLAN/12` names explicitly: Redis slow, database read replica lagging, OPA evaluation artificially slowed.
2. Verify circuit breakers around Redis and database calls behave as `PLAN/13` describes — a slow dependency must not block a request indefinitely.
3. Verify the fail-safe default: an authorization check that cannot complete **denies**. `PLAN/13` is unambiguous that availability of a decision is never a reason to weaken security posture.
4. Test each dependency failing completely, not only slowing: what still works with Redis down? With the primary database down but a replica available?
5. Confirm bounded latency increase rather than cascading failure.
6. Verify alerts actually fire during these tests. An alert that has never fired is an untested alert.
7. Test recovery: does the service return to normal automatically when the dependency recovers, or does it need a restart?

**Definition of Done**
- [ ] Every scenario in `PLAN/12`'s degraded-dependency list is tested.
- [ ] Authorization checks deny on failure, verified under fault injection.
- [ ] Circuit breakers bound latency rather than allowing indefinite blocking.
- [ ] Alerts fire during induced failures.
- [ ] Automatic recovery is verified.

---

## P5-06 — Horizontal Scale-Out Verification

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P5-04 |
| **Plan refs** | `PLAN/12-PERFORMANCE.md` § Definition of "Performance Acceptable", `PLAN/14-DEPLOYMENT.md` § Scalability |
| **Spec required** | No |
| **Surface** | infra |

**Goal** — Verify horizontal scaling in practice — `PLAN/12` says explicitly that this must be demonstrated by an actual scale-out test, "not just assumed from 'the service is stateless.'"

**Steps**
1. Run a scale-out test: increase instance count under load and measure the actual throughput gain.
2. Confirm no shared in-memory state: a session created on one instance must work identically on any other, including immediately after a scale event.
3. Verify the horizontal pod autoscaler responds to the intended signal (CPU or latency per `PLAN/14`) with sensible thresholds.
4. Test scale-down: confirm graceful shutdown drains in-flight requests without dropping them.
5. Verify the database connection pool does not become the bottleneck as instance count rises — this is the classic failure mode where adding instances makes things worse.
6. Test Redis under session volume, and confirm whether cluster mode is needed (`PLAN/14`).

**Definition of Done**
- [ ] Adding instances measurably increases throughput.
- [ ] A session works identically across instances, verified after a scale event.
- [ ] Autoscaling triggers correctly in both directions.
- [ ] Scale-down drops no in-flight request.
- [ ] Connection pool behavior under high instance count is understood and configured deliberately.

---

## P5-07 — Disaster Recovery Drill

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P0-20 |
| **Plan refs** | `PLAN/15-DISASTER-RECOVERY.md`, `PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 5 |
| **Spec required** | No |
| **Surface** | infra |

**Goal** — Execute a real drill, satisfying `PLAN/17`'s criterion that one has been performed successfully within the last twelve months.

**Steps**
1. Follow `PLAN/15`'s documented procedures rather than improvising — a drill that deviates from the runbook tests the team, not the runbook.
2. Restore the database from backup into a clean environment and verify data integrity, not merely that the restore command succeeded.
3. Measure actual recovery time and data loss, and compare against the stated objectives. If reality misses the target, that is the drill's most valuable output.
4. Test signing key recovery specifically. Losing signing keys invalidates every token in circulation and every consumer application's ability to verify — it is the worst realistic outcome, and the recovery path must be proven rather than described.
5. Test recovery of the full environment: database, Redis (accepting session loss), configuration, and secrets.
6. Document every deviation from the runbook and update the runbook accordingly.
7. Verify that the audit log survives intact — an audit trail with a gap around an incident is worth much less.

**Definition of Done**
- [ ] A full drill has been executed following `PLAN/15`.
- [ ] Actual recovery time and data loss are measured against objectives.
- [ ] Signing key recovery is proven.
- [ ] Audit log integrity survives the restore.
- [ ] The runbook is updated with every deviation found.
- [ ] The drill date and results are recorded in `MEMORY/`.

---

## P5-08 — Detection and Monitoring Completion

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P0-11, P1-14 |
| **Plan refs** | `SECURITY/03-DETECTION-AND-MONITORING.md`, `PLAN/13-OBSERVABILITY.md` § Alerting, `PLAN/09-SECURITY.md` § Audit |
| **Spec required** | No |
| **Surface** | backend, infra |

**Goal** — Every detection `SECURITY/03` specifies is implemented, firing correctly, and routed to someone who will act on it.

**Steps**
1. Implement every detection in `SECURITY/03`, and confirm coverage against the attack scenarios in `SECURITY/02`.
2. Implement `PLAN/13`'s named critical alerts: failed-login spikes from one IP or account, `/oauth/token` error rate above threshold, database replica lag, and sustained `/v1/authz/check` latency breach.
3. Forward the audit log to an external SIEM — `PLAN/09` calls this "ideally," and by production launch it should be actual. `P0-12` left the hook.
4. Tune thresholds against real staging traffic. An alert that fires constantly is functionally the same as no alert.
5. Define routing and escalation: who receives what, at what hour, and what they are expected to do.
6. Verify each alert fires by inducing its condition. An untested alert is an assumption.
7. Build a security dashboard covering authentication failure rates, grant changes, policy changes, and anomaly detections.

**Definition of Done**
- [ ] Every `SECURITY/03` detection is implemented and verified by induced condition.
- [ ] Every `PLAN/13` critical alert fires correctly.
- [ ] Audit logs reach an external SIEM.
- [ ] Thresholds are tuned against real traffic with a measured false-positive rate.
- [ ] Routing and escalation are documented.

---

## P5-09 — Incident Response Playbook Rehearsal

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P5-08 |
| **Plan refs** | `SECURITY/04-INCIDENT-RESPONSE-PLAYBOOKS.md`, `PLAN/15-DISASTER-RECOVERY.md` |
| **Spec required** | No |
| **Surface** | all |

**Goal** — Rehearse the playbooks before they are needed, because the first execution of an incident procedure should not happen during an incident.

**Steps**
1. Run a tabletop exercise for each playbook in `SECURITY/04`.
2. Rehearse the highest-consequence scenarios end to end: signing key compromise, cross-tenant data exposure, mass credential compromise, and delegation abuse.
3. Verify the practical mechanics exist and work: can a single organization be suspended quickly? Can all sessions be revoked at once? Can signing keys be rotated under emergency conditions? These are the actions the playbooks assume are available.
4. Confirm the audit log actually answers the questions an investigation asks — who did what, when, from where. If it does not, that is a finding.
5. Test the communication path, including the responsible-disclosure channel from `P0-19`.
6. Measure time-to-detect and time-to-contain during the exercise.
7. Update the playbooks with everything the rehearsal revealed.

**Definition of Done**
- [ ] Every `SECURITY/04` playbook has been rehearsed.
- [ ] Emergency actions (org suspension, mass session revocation, emergency key rotation) are proven to work.
- [ ] The audit log answers the investigative questions the playbooks pose.
- [ ] The disclosure channel is verified working.
- [ ] Playbooks are updated with rehearsal findings.

---

## P5-10 — Console: Audit Log Filtering and Export Polish

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-24 |
| **Plan refs** | `PLAN/16-IMPLEMENTATION-ROADMAP.md` § Phase 5, `UI-UX/08-PAGE-SPECIFICATIONS.md` (Audit Log) |
| **Spec required** | No |
| **Surface** | console |

**Steps**
1. Advanced filtering: combined predicates, saved filters, and full-text search across payloads.
2. Export to CSV and JSON, with a bounded row limit and asynchronous generation for large exports.
3. Treat export as a sensitive action: audit the export itself, including what was exported and by whom. An audit log export is a concentrated extract of the organization's activity.
4. Apply redaction to exports identically to on-screen display — an export path that bypasses redaction is a data-exposure bug.
5. Improve rendering of large result sets: virtualized scrolling and stable pagination while events are being written.
6. Add per-event-type detail views that render the payload readably rather than as raw JSON.

**Definition of Done**
- [ ] Combined filters work correctly and performantly on a large table.
- [ ] Export is asynchronous, bounded, redacted, and audited.
- [ ] Large result sets render without degrading the browser.
- [ ] Accessibility requirements are met for the new controls.

---

## P5-11 — Console: Accessibility Audit and Remediation

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | all console tasks |
| **Plan refs** | `UI-UX/13-ACCESSIBILITY.md`, `UI-UX/17-UX-ACCEPTANCE-CRITERIA.md`, `PLAN/02-REQUIREMENTS.md` (WCAG 2.1 AA), `PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 5 |
| **Spec required** | No |
| **Surface** | console |

**Goal** — Pass a WCAG 2.1 AA audit — a Phase 5 acceptance criterion and a stated non-functional requirement since `PLAN/02`.

**Steps**
1. Audit every screen against `UI-UX/17-UX-ACCEPTANCE-CRITERIA.md`'s detailed checklist.
2. Run automated tooling across the console, then test manually — automated tools catch a minority of real barriers.
3. Test with an actual screen reader across the highest-traffic flows: login, user invitation, role assignment, session revocation.
4. Verify keyboard-only operation of every flow, including modals, the typed-confirmation dialogs, and the Rego editor if Phase 4b shipped.
5. Verify color contrast for every token pair in every state (`UI-UX/05` requires AA against both background tokens).
6. Verify that state is never communicated by color alone — role-source badges and status badges both need a non-color signal.
7. Verify focus management: focus moves correctly into and out of modals and side panels, and is never lost to the document body.
8. Remediate everything found, and add automated accessibility checks to CI to prevent regression.

**Definition of Done**
- [ ] The console passes a WCAG 2.1 AA audit — `PLAN/17` Phase 5 criterion.
- [ ] Every screen meets `UI-UX/17`'s checklist.
- [ ] Screen reader testing covers the highest-traffic flows.
- [ ] Every flow is completable by keyboard alone.
- [ ] No state is communicated by color alone.
- [ ] Automated accessibility checks run in CI.

---

## P5-12 — Console: Performance Pass

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | all console tasks |
| **Plan refs** | `PLAN/16-IMPLEMENTATION-ROADMAP.md` § Phase 5 ("UI-side load/perf pass"), `UI-UX/12-RESPONSIVE-BEHAVIOR.md`, `UI-UX/14-EMPTY-LOADING-ERROR-STATES.md` |
| **Spec required** | No |
| **Surface** | console |

**Steps**
1. Measure and reduce bundle size; code-split by route so a user reaching the Users list does not download the Rego editor.
2. Test every list screen with large datasets — thousands of users, hundreds of projects. Virtualize where needed.
3. Tune TanStack Query cache settings per resource: audit events and user lists have very different staleness tolerances.
4. Eliminate layout shift during loading, per `UI-UX/14`'s loading-state requirements.
5. Optimize the initial load path, particularly the OIDC redirect round-trip, which is the first thing every user experiences.
6. Verify performance on a mid-range device at tablet width, not only on a developer's machine.

**Definition of Done**
- [ ] Bundle size is measured and route-split.
- [ ] List screens perform acceptably with large datasets.
- [ ] No layout shift during loading states.
- [ ] Initial load is measured on a representative device, not a developer machine.

---

## P5-13 — Public Security and Trust Page

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P5-03 |
| **Plan refs** | `PLAN/20-PUBLIC-SITE-ARCHITECTURE.md` § What Never Gets Published, `UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md`, `UI-UX/21-CONTENT-AND-COPY-STRATEGY.md` |
| **Spec required** | No |
| **Surface** | public-site |

**Goal** — Publish the trust page `PLAN/20` schedules for this phase, communicating security posture without publishing defensive documentation.

**Steps**
1. Write at a marketing and trust level: TLS everywhere, asymmetric token signing, Argon2id password hashing, audit logging, penetration testing cadence, responsible disclosure.
2. Honor `PLAN/20`'s explicit prohibitions absolutely. **Never publish**: specific attack scenarios or mitigation mechanics from `SECURITY/02`; infrastructure topology from `PLAN/14`; anything from `PLAN/18-RISK-REGISTER.md`.
3. Document the responsible-disclosure program properly: scope, how to report, expected response time, and safe-harbor language.
4. State compliance posture honestly. "SOC 2 in progress" is fine if true; claiming a certification not held is both a legal and a trust problem.
5. Have the page reviewed by whoever owns security, specifically checking that nothing internal leaked into it.
6. Apply `UI-UX/21`'s governance rule: no claim beyond what actually ships.

**Definition of Done**
- [ ] The page is published and matches `UI-UX/20`'s specification.
- [ ] A review confirms nothing from `SECURITY/02`, `PLAN/14` topology, or `PLAN/18` appears on it.
- [ ] The disclosure program is documented with scope and response expectations.
- [ ] Every compliance claim is accurate.

---

## P5-14 — Integrator Documentation Completion

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | Phase 4 exit |
| **Plan refs** | `PLAN/16-IMPLEMENTATION-ROADMAP.md` § Phase 5, `PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`, `UI-UX/21-CONTENT-AND-COPY-STRATEGY.md` |
| **Spec required** | No |
| **Surface** | docs |

**Goal** — Complete documentation for internal and external developers who will integrate, per `PLAN/16`'s Phase 5 item.

**Steps**
1. Complete the guide set for every shipped capability, verifying each by having someone unfamiliar follow it.
2. Complete the `/docs/console` section — how to use the management console at an end-user level (`PLAN/20`).
3. Add a migration guide for teams moving off an existing auth system, honoring `PLAN/01`'s constraint that migration is gradual and app-by-app, never a big-bang cutover.
4. Add a troubleshooting section covering the failures integrators actually hit: redirect URI mismatch, audience validation failure, clock skew on token expiry, PKCE verifier mismatch.
5. Document every operational limit: rate limits, token lifetimes, revocation windows, pagination bounds, attribute size caps.
6. Verify docs versioning works, and that the version matching the current API is the default.
7. Ensure the generated API reference covers every endpoint with no gaps.

**Definition of Done**
- [ ] Every shipped capability has a guide, verified by someone who did not write it.
- [ ] Console documentation is complete.
- [ ] The migration guide reflects `PLAN/01`'s gradual approach.
- [ ] Troubleshooting covers the common real failures.
- [ ] Every operational limit is documented.
- [ ] Versioned docs work correctly.

---

## P5-15 — Risk Register Reconciliation

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P5-03 |
| **Plan refs** | `PLAN/18-RISK-REGISTER.md`, `PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 5, `AGENTS.md` rule 9 |
| **Spec required** | No |
| **Surface** | docs |

**Goal** — Bring the risk register up to date with what was actually built, so accepted risks are visible decisions rather than forgotten ones.

**Steps**
1. Review every risk in `PLAN/18` against the built system: still relevant, mitigated, or superseded?
2. Add every accepted risk from `P5-03`, each with owner, rationale, compensating controls, and review date.
3. Add risks discovered during load testing, DR drilling, and incident rehearsal.
4. Confirm every accepted risk has a named owner and a review date. An accepted risk with neither is an ignored risk.
5. Because `PLAN/18` is a reference document (`AGENTS.md` rule 9), make this a deliberate, reviewed edit rather than an incidental one.
6. Establish the review cadence for keeping it current after launch.

**Definition of Done**
- [ ] Every existing risk is reviewed and updated.
- [ ] Every accepted risk has owner, rationale, compensating controls, and review date.
- [ ] Risks found during Phase 5 testing are captured.
- [ ] The edit went through the deliberate plan-change process.
- [ ] A post-launch review cadence is established.

---

## P5-16 — Production Launch Readiness Review

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | all Phase 5 tasks |
| **Plan refs** | `PLAN/17-ACCEPTANCE-CRITERIA.md` (all phases), `PLAN/11-TESTING.md` § "Production-Ready" Criteria, `PLAN/12-PERFORMANCE.md` § Definition of "Performance Acceptable" |
| **Spec required** | No |
| **Surface** | all |

**Goal** — A single, deliberate go/no-go review against every acceptance criterion in the project, not just Phase 5's.

**Steps**
1. Walk `PLAN/17`'s criteria for every phase, confirming each still holds — a Phase 1 criterion can regress during Phase 4.
2. Confirm `PLAN/11`'s "Production-Ready" criteria: every Phase 1 and 2 flow has passing integration and E2E tests in CI; no unaddressed critical or high SAST or dependency findings; load testing meets `PLAN/12`; pentest findings reviewed and critical ones remediated; every high-priority abuse scenario has an automated test.
3. Confirm `PLAN/12`'s "Performance Acceptable" definition: latency targets met, graceful degradation demonstrated, horizontal scaling verified in practice.
4. Confirm operational readiness: monitoring, alerting, on-call routing, runbooks, and rehearsed playbooks.
5. Confirm the general definition of done from `PLAN/17` holds across the codebase — test coverage, OpenAPI accuracy, audit coverage, trust boundary analysis.
6. Confirm the public site claims nothing beyond what ships.
7. Produce a written go/no-go with named sign-off.
8. Write the launch summary in `MEMORY/`, including everything deferred and every accepted risk.

**Definition of Done**
- [ ] Every `PLAN/17` criterion across every phase is verified as still holding.
- [ ] `PLAN/11`'s production-ready criteria are all met.
- [ ] `PLAN/12`'s performance-acceptable definition is met.
- [ ] Operational readiness is confirmed with rehearsed procedures.
- [ ] A written go/no-go decision exists with named sign-off.
- [ ] The launch summary is recorded in `MEMORY/`.

---

## Phase 5 Exit Checklist

From `PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 5:

- [ ] External pentest findings rated critical or high are all remediated, or have a documented, accepted-risk sign-off in `PLAN/18-RISK-REGISTER.md`.
- [ ] Load test results meet every target in `PLAN/12-PERFORMANCE.md`.
- [ ] A disaster recovery drill has been executed successfully within the last 12 months.
- [ ] The console passes a WCAG 2.1 AA accessibility audit.

Plus, from this phase's own scope:

- [ ] All nineteen `SECURITY/02` categories have documented mitigation, test, and detection.
- [ ] Graceful degradation and horizontal scaling are verified in practice, not assumed.
- [ ] Incident response playbooks have been rehearsed and updated.
- [ ] The public security page is live and leaks nothing internal.
- [ ] Integrator documentation is complete and verified by someone unfamiliar with the project.
- [ ] A written go/no-go decision exists.
