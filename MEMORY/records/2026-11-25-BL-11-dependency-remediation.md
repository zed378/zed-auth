# BL-11 — Vulnerable dependency baseline remediation

Date: 2026-11-25. Validation in progress; see the final verification section when
available. No vulnerability gate was disabled or suppressed.

The baseline scan on Go 1.26.11 reported six reachable vulnerabilities from
three modules. Updated the affected dependency families to the fixed versions
named by the scanner: OpenTelemetry 1.47.0 (`GO-2026-6974`),
`github.com/klauspost/compress` 1.20.2 (`GO-2026-6865`), and `golang.org/x/net`
0.60.0 (`GO-2026-6617`, `GO-2026-6612`, `GO-2026-6611`, `GO-2026-6603`).

`go get` and `go mod tidy` succeeded. Required transitive changes include
OpenTelemetry log/metric/exporter alignment, gRPC 1.83.2 and genproto updates.
Only `backend/go.mod` and `backend/go.sum` were edited by the dependency agent.
Existing working-tree Go 1.26.11 pins in CI, Dockerfiles and the demo module were
preserved. No new application feature or data model was introduced by this fix.

Full backend unit/integration validation, build/vet and vulnerability scanning
belong to the single testing agent for this parallel continuation. Earlier
failed baseline scans remain historical evidence in the P4-10 records.

## Verification so far

- Full backend unit suite: 1,727 tests pass across 45 packages; build and vet pass.
- Elevated `govulncheck ./...`: zero reachable or imported-package vulnerabilities.
  Three module-only advisories in code outside imported packages remain; this is
  not a claim that every module is advisory-free. The initial sandbox scan failed
  loading cache entries and had no vulnerability verdict.
- CI-equivalent gosec: zero issues across 178 files. `go mod verify` passes.
- Full backend integration: initial sandbox run failed on Docker startup/cache
  access (394 passes, 148 failures, three skipped entries); elevated serial rerun
  is in progress. Do not interpret the failed infrastructure run as a green suite.
- Initial demo test invocation required module tidying. `go mod tidy` removed
  the redundant toolchain directive while preserving `go 1.26.11`; the rerun
  passes 34 tests across four packages. No demo checksum changes were needed.

References: `docs/PLAN/09-SECURITY.md`, `docs/PLAN/11-TESTING.md`,
`docs/SECURITY/05-VERIFICATION-AND-REDTEAM-PLAN.md`. Rollback means reverting the
dependency manifest/checksum changes together and restores the known vulnerable
baseline; it requires a separate assessment before release. No deployment or
commit was performed.
