# P0-22 — Reference Documentation: Domain Categories Written From The Code

| | |
|---|---|
| **Date** | 2026-09-18 |
| **Task** | `TASKS/PHASE-0-FOUNDATION.md` § P0-22 |
| **Phase** | Phase 0 — Foundation (documentation) |
| **Surface** | docs |
| **Branch** | `docs/restore-and-detail` |
| **Status** | Complete |

---

## Why this was needed

`docs/` held three folders of design intent (`PLAN`, `UI-UX`, `SECURITY`) and nothing that
described the system **as built**. The request was to detail it the way
`github.com/zed378/image-management` organises documentation: a folder per domain, numbered
documents, an index per folder.

That reference repository's documents are largely empty scaffolds. Ours could not be: there
is a working service behind them, and a scaffold that guesses is worse than no document,
because it reads as authoritative.

## What was wrong when this started

An earlier attempt had already produced 17 category folders, and it had also:

- **overwritten reference documents it should not have touched.** `SECURITY/02` lost 281 of
  its 281 scenario lines, replaced by a five-line summary; `SECURITY/00`, `SECURITY/01` and
  `PLAN/00`–`02` were rewritten the same way. Every task card in `TASKS/` cites those
  abuse-case scenarios by number;
- **added duplicate-numbered scaffolds** shadowing the originals — a second
  `03-…`, `04-…`, `05-…` in both `PLAN/` and `SECURITY/`;
- **stated things the codebase does not do**: a `/v1/project-grants` endpoint (the real
  paths are nested under organizations and projects), `/v1/admin`, `/v1/auth/*`, ids like
  `proj_123` (this service uses UUIDs, `PG-23`), SDKs that do not exist, a webhook API that
  does not exist, and a token blacklist that does not exist — all under a header reading
  "Status: Final specification".

## What was done

1. **Restored** `PLAN/00`–`02` and `SECURITY/00`–`02` verbatim from `827b14f`, and removed
   the eight shadow scaffolds. Those three folders now differ from their pre-restructure
   state only by a new `README.md` index each.
2. **Rewrote the 14 domain categories from the code.** Every document carries a status —
   `Implemented`, `Partially implemented`, `Draft specification` — and cites the migration,
   handler, contract entry, test or record behind each claim. Anything unbuilt names its
   owning task.
3. **Wrote `scripts/check-docs.py`**, which fails on: a scaffold marker outside a draft, a
   missing or unknown status, an endpoint in neither the contract nor the router, a
   prefixed-id example, or a citation of a path that does not exist. It reports 0 problems
   across 149 documents.

Written partly by hand and partly by subagents working from a written brief; every
subagent's output was checked against the code, and several of their claims were corrected.

## Defects this surfaced in the product, not the documentation

| Defect | Status |
|---|---|
| `openapi/openapi.yaml` still announced "Phase 0 … operational probes only" in its `info.description`, and the published API reference repeated it | **Fixed here**; the generated reference and console client were regenerated |
| The backup document claimed alerting on a missing success; `deploy/observability/alerts.yml` has 23 rules and none about backups | **Documented as a gap** in `docs/DEVOPS/05-BACKUP-AND-DISASTER-RECOVERY.md` |
| Tracing: the OTel SDK, exporter and propagator are wired, but `StartSpan` has no call site, so no span is ever created — `docs/PLAN/13` asks for end-to-end tracing | **Documented** in `docs/OBSERVABILITY/04-DISTRIBUTED-TRACING.md` |
| `manager_roles` still has no RLS policy, while `TASKS/BACKLOG.md` DV-02 says that gap closes with `P2-05`, which is marked DONE | **Documented** in `docs/DATABASE/02-ROW-LEVEL-SECURITY-POLICIES.md` |
| No endpoint lets a receiving organization list the grants made to it | **Documented**; belongs to `P4-06` |
| `docs/PLAN/04`'s `webhook_endpoints.secret_hash` cannot support HMAC signing (a hash cannot produce a signing key); it needs sealing like `P3-02`'s TOTP secrets | **Documented** in `docs/WEBHOOK/`; a plan change for `P4-12` |

## Verification

| Check | Result |
|---|---|
| `python scripts/check-docs.py` | 0 problems across 149 documents |
| `git diff 827b14f -- docs/PLAN docs/UI-UX docs/SECURITY` | only the three new `README.md` files |
| Redocly lint after the contract text change | valid |
| Regenerated server interface, console client, public API reference | committed, no drift |
| `cd console && npm run check` | lint, typecheck, 276 tests pass |
| `bash scripts/check.sh` | see below |

The first full gate run after the contract change reported `console tests` failing; run
directly, `npm run check` passes (276 tests, twice). The gate run overlapped several
subagents doing heavy work on the same machine, which is the likely cause. Re-run before
merging.

## Not done here

- `docs/PLAN`, `docs/UI-UX` and `docs/SECURITY/00`–`05` were deliberately not edited. The
  contradictions the work found between plan and code are recorded in the domain documents
  and in `TASKS/BACKLOG.md`, not resolved by quietly editing the plan.
- `CLAUDE.md`'s documentation map still points only at the plan folders; adding the domain
  categories to it is a separate, reviewed change.
