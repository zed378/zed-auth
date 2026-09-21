# Engineering Practice

How code is written, reviewed and landed in this repository. Conventions, not design:
`docs/PLAN/` says what to build, and these documents say how the building is done so that
twelve packages do not answer the same question twelve ways.

Everything here describes practice **as it is**, with the file or gate that enforces it. A
convention nothing enforces is listed as a convention, not as a rule — the difference is
the point.

## Documents

| File | Topic | Status |
|---|---|---|
| [`00-ENGINEERING-CONTEXT.md`](./00-ENGINEERING-CONTEXT.md) | What governs code here, and the order of authority | Implemented |
| [`01-GO-CODING-STANDARDS.md`](./01-GO-CODING-STANDARDS.md) | Idiom, comments, package shape, dependency policy | Implemented |
| [`02-PROJECT-STRUCTURE.md`](./02-PROJECT-STRUCTURE.md) | Where every kind of file lives, across all four surfaces | Implemented |
| [`03-NAMING-CONVENTIONS.md`](./03-NAMING-CONVENTIONS.md) | Packages, files, migrations, tests, branches, task IDs | Implemented |
| [`04-TYPESCRIPT-STANDARDS.md`](./04-TYPESCRIPT-STANDARDS.md) | Console and public-site TypeScript rules | Implemented |
| [`05-HANDLER-AND-STORE-TEMPLATES.md`](./05-HANDLER-AND-STORE-TEMPLATES.md) | The shape every Management API endpoint takes | Implemented |
| [`06-ERROR-AND-FAULT-STANDARDS.md`](./06-ERROR-AND-FAULT-STANDARDS.md) | One error envelope, one class-to-status mapping | Implemented |
| [`07-DATABASE-ACCESS-STANDARDS.md`](./07-DATABASE-ACCESS-STANDARDS.md) | Tenant transactions, parameterised SQL, migration rules | Implemented |
| [`08-CACHE-AND-BACKGROUND-WORK.md`](./08-CACHE-AND-BACKGROUND-WORK.md) | Redis use, invalidation by generation, scheduled work | Implemented |
| [`09-TESTING-CONVENTIONS.md`](./09-TESTING-CONVENTIONS.md) | Layers, build tags, mutation discipline, coverage floors | Implemented |
| [`10-TOOLING-LINT-AND-FORMAT.md`](./10-TOOLING-LINT-AND-FORMAT.md) | Every tool the gate runs, and what each one catches | Implemented |
| [`11-GIT-AND-REVIEW-CONVENTIONS.md`](./11-GIT-AND-REVIEW-CONVENTIONS.md) | Branches, commit subjects, hooks, merge policy | Implemented |
| [`12-LOGGING-CONVENTIONS.md`](./12-LOGGING-CONVENTIONS.md) | Structured logging, redaction by key, what never appears | Implemented |
| [`13-SECURITY-CODING-RULES.md`](./13-SECURITY-CODING-RULES.md) | The rules that exist because breaking them is an incident | Implemented |
| [`14-CODE-REVIEW-CHECKLIST.md`](./14-CODE-REVIEW-CHECKLIST.md) | What a reviewer checks that no tool can | Implemented |

## Related

- [`../ARCHITECTURE/`](../ARCHITECTURE/) — what the code is shaped like.
- [`../FRONTEND/`](../FRONTEND/) — the console's own engineering detail.
- [`../TESTING/`](../TESTING/) — the test strategy these conventions serve.
- [`../../AGENTS.md`](../../AGENTS.md) and [`../../CLAUDE.md`](../../CLAUDE.md) — the
  short forms that live at the repository root.
