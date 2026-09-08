# The Test Harness, and a Suite That Reported Success Without Running

**Date**: 2026-09-08
**Task**: `P0-15`
**Branch**: `feat/P0-15-test-harness`

---

## The Thing That Was Actually Wrong

The integration tests resolved a database from `AUTH_TEST_APP_DSN` with a `localhost:5432` fallback, and called `t.Skipf` when nothing answered.

So on a machine without PostgreSQL — a fresh clone, a new laptop, a misconfigured runner — the suite ran, skipped every test that touches a database, and **reported success**. Five skips across three files, covering row-level security, the audit log's append-only guarantee, and the schema.

That is worse than having no tests. No tests is a known gap; a green suite that skipped the tests is a false statement about the system, and it is the statement everyone acts on.

`P0-15`'s Definition of Done says the integration suite must run "from a clean machine with only Docker installed". That is the sentence this whole task turned out to be about.

---

## What Replaced It

`internal/testsupport` starts PostgreSQL and Redis with testcontainers, applies the embedded migrations — the same `embed.FS` the binary carries (ADR-007), not a copy read from disk — and creates both database roles.

A failure to start is `t.Fatalf`, never `Skip`. A missing Docker daemon is a broken environment, and the suite must say so rather than shrug.

Containers start **once per test binary**, not per test, because a suite that costs seconds per test stops being run. Isolation comes from `Truncate` between tests, which is cheap, and `go test -shuffle=on` in CI makes an accidental ordering dependency fail rather than lurk for months as an unreproducible flake.

**Both roles matter.** `auth_owner` migrates and seeds and bypasses row-level security; `auth_app` is `NOSUPERUSER NOBYPASSRLS` and owns nothing. A suite connecting as the owner would find every isolation test passing and prove nothing — which is not hypothetical here: this project shipped a check for "auth_app is not superuser" that passed because the role did not exist.

---

## The Harness Broke a Production Guarantee, and a Test Caught It

The first version granted `SELECT, INSERT, UPDATE, DELETE ON ALL TABLES` after running the migrations.

Production does it the other way round: `deploy/postgres/init/01-roles.sh` sets `ALTER DEFAULT PRIVILEGES` **before** any migration, so migration-created tables inherit DML automatically — and the revokes those migrations then perform survive, because nothing grants over them afterwards.

Granting afterwards re-granted `UPDATE` and `DELETE` on the `events` partitions that migration `000006` had carefully revoked. The test harness had a weaker privilege model than production, so a suite that looked like it was testing the deployed system was testing a database with the audit log's append-only guarantee removed.

`TestNewPartitionsAreAppendOnly` failed and said so — and it could only do that because the skip hiding it was removed in the same change. The two findings are the same finding.

The rule this leaves behind: **a test harness mirrors production's privilege model rather than approximating it**, in the same order, or it tests a database nothing will run.

---

## All Four Pyramid Layers, With Real Tests

`PLAN/11`'s pyramid, and `P0-15`'s requirement of "one passing example test at each layer". Not a placeholder at any of them — a test that would pass against a blank page is not an example of anything.

**Unit** — table-driven, no database. Already existed.

**Integration** — real PostgreSQL and Redis, migrations applied, per-test truncation.

**Security** — `backend/tests/security/`, kept apart from feature tests. `P0-15` step 4 asks for the separation, and the reason is that these are a checklist as much as a suite: `PLAN/11` lists seven scenarios from the threat model, and scattered across packages they become seven tests nobody can enumerate. Here, `go test ./tests/security/...` is the answer to "have we covered the threat model". The package comment carries a coverage map naming the four that exist and the six that cannot exist until the feature does — an unwritten test nobody knows is unwritten is worse than a failing one.

**End-to-end** — Playwright against the console's **production build**, not the dev server. An E2E suite that has never seen the built bundle is testing something nobody deploys. It is also the only layer that applies a real stylesheet, which is exactly where the console's dead design tokens would have been caught: every jsdom test passed while `text-body` generated no CSS at all.

---

## Two Tests About the Tests

**A control.** `TestIsolationTestsAreNotVacuous` connects as the owner and asserts it sees *both* tenants' rows. Without it, every isolation test would still pass if the factory silently failed to create the second user, or a botched truncation left the table empty. "Tenant A saw one user" is only evidence of isolation if the row it did not see was actually there.

**A proof.** Pointing the security suite's connection at the owner DSN makes all four tests fail — cross-tenant reads, unfiltered queries, the role attributes, and the append-only log. They are measuring the property, not agreeing with themselves.

This project has now hit the vacuous pass five times. A control test is the cheapest known answer.

---

## Coverage Floors, Not a Coverage Number

`P0-15` step 6 is specific: floors on `internal/authn`, `internal/authz` and `internal/oidc`, "rather than a meaningless repo-wide average".

The reasoning holds up. A repo-wide percentage is satisfied by testing whatever is easiest, and the easiest code to test is rarely the code where a bug matters — eighty per cent overall can mean complete coverage of configuration parsing and none of token verification.

None of the three packages exists yet. `scripts/check-coverage.sh` reports them as pending and passes, so each floor starts applying the moment its package appears rather than on the day somebody remembers to add it.

It also had a bug worth recording: the first version tested for a *directory*, and `P0-02` had scaffolded empty ones. So `go test` failed with "no Go files" and the script called that a coverage failure — a floor that fails before the code exists is a floor everyone learns to ignore. It now tests for Go source. Verified both ways: pending when empty, and firing at `0.0% < 80%` against a deliberately untested package.

---

## The Same Bug, Three Times in One Day

Playwright waited two minutes for a server that had started immediately. `vite preview` binds `localhost`, which resolves to `::1` first; the readiness probe dialled `127.0.0.1`.

That is the third occurrence today: the console's nginx healthcheck probed `localhost` against an IPv4-only `listen 80`, and a local preview server was unreachable on `127.0.0.1` while answering on `localhost`.

**Name the address family on both sides.** `localhost` means different things to the thing binding and the thing connecting, and the symptom — a healthy server that nothing can reach — never looks like a name-resolution problem.

---

## Verified

| Layer | Result |
|---|---|
| Unit | passing, shuffled |
| Integration | passing, shuffled, from containers with no prior setup |
| Security | 5 tests passing; all 4 assertions fail when RLS cannot apply |
| End-to-end | 4 tests passing against the production build |
| Coverage floors | 3 pending, firing correctly when a package exists |

The whole integration suite now runs on a machine with nothing but Docker: no `make up`, no migrations applied by hand, no environment variables.

---

## What the New Dependencies Cost

testcontainers pulls a large transitive tree, and `govulncheck` immediately reported three vulnerabilities through it — two in `golang.org/x/crypto/ssh` and one in `moby/go-archive`, a tar path-traversal. All reachable only from test code, and all fixed by an upgrade that was already available.

Worth stating plainly rather than waving away: a test-only dependency is still a dependency, it still runs on developer machines and CI runners, and a tar extraction flaw reachable from a test harness is reachable from anything that harness pulls. The upgrade was two lines. The alternative — an exception for "it's only tests" — is how a scanner's output becomes noise.

The console's lint also failed on the Playwright fixtures, and correctly from its own point of view: Playwright's fixture API takes a `use` callback that has nothing to do with React's `use` hook, and `async ({}, use) =>` is its idiomatic empty-dependency form. Both are errors under rules that are right about React and wrong about this file, so the React rules are now scoped to the application source. Turning the rules off globally would have been the easier fix and would have removed them from the code they exist for.

---

## Outstanding

- **`PLAN/11` § Load & Performance is untouched.** `PLAN/12` sets p95 targets and nothing measures them. There is no endpoint to measure yet, so this belongs with Phase 1 rather than here.
- **Fuzz testing** is named in `PLAN/11` for parsers accepting external input — JWTs, SAML assertions. No such parser exists yet.
- **The E2E fixtures are deliberately unimplemented.** Seeding an organization, project, application and user needs the Management API from `P1-15`. Each throws with the task that unblocks it rather than returning fake data: a fixture that silently returned a plausible object would let a Phase 1 test pass against nothing.
