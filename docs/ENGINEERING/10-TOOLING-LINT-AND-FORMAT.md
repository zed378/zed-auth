# 10 - Tooling, Lint and Format

> Category: **Engineering Practice** (`docs/ENGINEERING/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-11, P0-13, P0-15, P0-22 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

List every automated check in the repository, what each one catches, and which ones are
unusual enough to be worth knowing about before you trip one.

## Scope

`scripts/check.sh` (841 lines), `scripts/check-coverage.sh`, `scripts/check-docs.py`,
`scripts/hooks/`, `console/eslint.config.js`, `.github/workflows/`.

## As Built

`scripts/check.sh` is the gate. It runs locally and in CI, prints one line per check, and
exits non-zero on any failure. Slow layers are behind `CHECK_FULL=1` locally and always on
in CI.

### Backend

| Check | Catches |
|---|---|
| `gofmt` | formatting, on `cmd`, `internal`, `migrations`, `demo` |
| `go vet` | the usual suspects |
| unit tests, `-race`, shuffled | data races, order dependencies |
| `go mod tidy` | an undeclared or unused dependency |
| demo has no `go.sum` | the zero-dependency demo gaining one |
| integration tests | the real Postgres/Redis behaviour |
| **every tenant-scoped table has RLS** | a new table added without a policy |
| **audit relations are append-only for `auth_app`** | an `UPDATE`/`DELETE` grant that would let the service rewrite its own audit trail |
| every `up` migration has a `down` | an unreversible migration |
| destructive migrations carry an expand/contract note | a rollout that breaks the running previous version |
| **rows with an expires-after-creation `CHECK` are written from one clock** | a constraint that silently asserts something about clock skew |
| `gosec`, `govulncheck` | known vulnerable patterns and dependencies |
| **no raw request material in a log call** | a body or header reaching a log positionally, past the redacting logger |
| no PEM private key blocks; no credential-shaped tokens | a committed secret |
| coverage floors | a security-relevant package losing coverage |

### Contract and generated code

| Check | Catches |
|---|---|
| OpenAPI spec is valid | a malformed contract |
| generated API code matches the spec | a handler interface regenerated in one place only |
| **spec claims no endpoint beyond what has shipped** | documentation promising a route nobody implemented |
| console API client matches the spec | the console not absorbing a contract change |
| shared validation patterns match the spec | a client rule drifting from the server's |

### Repository hygiene

| Check | Catches |
|---|---|
| CI workflows parse and pass `actionlint` | a broken workflow discovered on push |
| **scripts are executable in the git index** | a fresh clone getting files it cannot run |
| **the commit-msg hook actually rejects** | a hook that was edited into a no-op |
| `shellcheck` | shell bugs |
| **documentation citations resolve** | a document naming a file, path or endpoint that no longer exists |

### Console and public site

Lint (token discipline and a11y rules), typecheck, unit tests, the three generation diffs,
and end-to-end tests. For the public site: brand tokens match the console's, contrast meets
AA in both themes, **no code is shared with the console**, and the generated API reference
is current.

### Deployment

Every compose file parses; the demo SPA is given no client secret; the service container
receives no owner credentials.

### The three checks worth understanding before you trip them

**"Scripts are executable in the git index."** Not on disk — in the index. A clone gets the
index mode, and this surfaced when the VM deployment moved to `git clone`: a fresh checkout
got files nothing could run, and `secrets.sh` failed with "command not found" at the moment
it was needed to regenerate a signing key. Fix with `git update-index --chmod=+x <file>`.

**"The commit-msg hook actually rejects."** The gate feeds the hook a bad subject and a
good one and checks both answers. A hook is only a control if it rejects; asserting that it
exists asserts nothing.

**"Rows with an expires-after-creation check are written from one clock."** Described in
[`07-DATABASE-ACCESS-STANDARDS.md`](./07-DATABASE-ACCESS-STANDARDS.md). Its first version
looked for an `INSERT` into a table that did not exist, found nothing, skipped and passed
while the bug was in the tree — so a table it cannot find an `INSERT` for is now a failure.

### Hooks

`scripts/install-hooks.sh` points `core.hooksPath` at `scripts/hooks/`.

- **`commit-msg`** rejects a subject without a task ID. Git's own generated subjects
  (merge, revert, fixup, squash) pass through.
- **`pre-commit`** refuses to stage a credential: `.env` files, `*.pem`, `*.key`, `*.p12`,
  `id_rsa`, and credential-shaped strings. CI's `gitleaks` is the real gate; the hook
  exists because of the gap between them — once a secret reaches a remote branch, deleting
  the commit does not un-leak it, and the value has to be rotated. Catching it here is the
  difference between deleting a line and rotating a production key.

Both can be bypassed with `--no-verify`, and the pre-commit hook says what to do if you do:
rotate whatever you just committed.

## Rules and Defaults

| Rule | Value |
|---|---|
| Local gate | `bash scripts/check.sh` |
| Full gate | `CHECK_FULL=1 bash scripts/check.sh` |
| Integration tests | `RUN_INTEGRATION=1` and a running Postgres |
| Security scans | `RUN_SECURITY=1` |
| Hooks | `bash scripts/install-hooks.sh` |

## Not Yet Built / Open Questions

- **No `golangci-lint`** and **no Prettier**.
- **`gosec` and `govulncheck` skip if not installed** rather than failing. CI has them;
  a local run without them is quieter than it looks, and the skip line says so.
- **No dependency-update automation.**

## Related Documents

- [`09-TESTING-CONVENTIONS.md`](./09-TESTING-CONVENTIONS.md)
- [`11-GIT-AND-REVIEW-CONVENTIONS.md`](./11-GIT-AND-REVIEW-CONVENTIONS.md)
- [`../DEVOPS/`](../DEVOPS/)
