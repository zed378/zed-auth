# The API Contract Becomes Executable

**Date**: 2026-09-08
**Task**: `P0-16` (steps 1, 2, 5 — steps 3 and 4 carried to `P0-17`/`P0-18`)
**Decisions**: [ADR-012](../DECISIONS.md), [ADR-013](../DECISIONS.md)

---

## The Choice That Mattered

`P0-16` step 2 asked for a decision between spec-first and code-first, and `PLAN/05` § Documentation accepts either — "generated from code or validated in CI". So the question was not which one the plan wanted. It was which one makes drift *impossible* rather than *detectable*.

Code-first from Go annotations looks safer than it is. The annotation sits next to the handler, which feels like proximity enforces agreement, but an annotation is a comment: it can say `200` while the handler returns `201` and nothing objects. It converts a compile-time property into a review-time one.

Spec-first with generated server interfaces goes the other way:

```go
var _ api.StrictServerInterface = (*Health)(nil)
```

Change a response in the spec, regenerate, and every handler that no longer matches fails to build. A CI check finds the same problem after the push; the compiler finds it in the editor.

The task asked for "CI validates the spec". That is now the backstop rather than the mechanism.

---

## What Writing It Down Immediately Found

The spec was written against the running service rather than from memory, which turned up that the two probes report different vocabularies: `/healthz` returns `{"status":"ok"}`, `/readyz` returns `{"status":"ready"}`.

Small, harmless, and instructive. The instinct is to tidy it — one enum, one word, done. That would be a breaking API change for any consumer already parsing `ready`, made silently, in the name of a prettier document.

So it is recorded as it is, in two schemas rather than one, with the reason in the schema description and a test pinning both values. Changing it now has to defeat a failing test that says what it would break.

The contract was never wrong. It had just never been written anywhere that could check it.

---

## Three Rules Encoded Rather Than Written Down

**The spec documents only what has shipped.** `/docs/api-reference` renders from this file, so a documented endpoint is a public claim that it exists — which `UI-UX/21`'s governance rule and `CLAUDE.md` both forbid for unshipped capability. `PLAN/05` Part B lists the whole `/v1` surface and writing it out now as a design exercise was tempting; it would have published that claim for every unbuilt endpoint at once.

`scripts/openapi-shipped-paths.py` gates it, in CI and in `check.sh`. Adding an endpoint means adding it to `SHIPPED` in the same commit. The duplication is the point: publishing an endpoint becomes a deliberate two-line act rather than a side effect of editing YAML. The script also fails in the reverse direction — a `SHIPPED` entry the spec no longer documents — because a stale allowlist quietly widens.

**The shared component schemas are deliberately ahead of the endpoints.** The error envelope, the pagination token, the common parameters. These are contract infrastructure, not capability claims, and settling them now is the point of the task: an error format that changes after twenty endpoints exist is a breaking change to all twenty. `oapi-codegen`'s default is to prune unreferenced components, which would have deleted exactly these — `skip-prune: true`, with the reasoning in the config.

**3.0.3, not 3.1.** `oapi-codegen` prints `OpenAPI 3.1.x is not yet supported ... some functionality may not be available` and then generates anyway. Generating anyway is the problem. The entire argument above rests on the generated interface being trustworthy enough to be the enforcement mechanism, and a generator that has announced it may be silently incomplete cannot be that. Nothing in this contract needs 3.1; tracked as `SL-03` for when the generator catches up.

---

## A Bug in the Checker Itself

`scripts/check.sh` defined a shell function named `head`. That shadows the `head` command for the whole script, so every `... | head -20` in a pipeline called the function instead — which ignores stdin, prints a cyan `-20`, and **discards the piped output entirely**.

Two failure paths had been broken since they were written. On a unit-test failure or a `govulncheck` finding, the twenty lines of detail meant to explain the failure were thrown away and replaced with `-20`. Nobody noticed, because the effect only appears when something fails, and until now nothing had.

Renamed to `section`. The general shape is one this project keeps meeting: **a diagnostic path that only runs on failure is a path that is never exercised**, so it is exactly where a silent bug survives longest. It is worth deliberately failing a check to watch its output, which is how this was found — the drift gate was tested by breaking the spec on purpose.

---

## Verified

| Check | Result |
|---|---|
| `redocly lint` | Valid, zero warnings |
| Generated code current | Gate passes |
| Gate catches drift | Renamed an `operationId`; gate failed with a readable diff |
| Unshipped-endpoint gate | Added `/v1/organizations`; gate failed. Removed it; gate passed |
| Backend build, vet, gofmt, tests | Clean |
| `scripts/check.sh` | 25 passed, 0 failed |

Three new gates: spec validity, generated-code freshness, and the shipped-endpoint rule. `scripts/check.sh` went from 22 gates to 25.

---

## Carried Forward

Steps 3 and 4 — the console's typed client and the public site's API reference — need `console/` and `public-site/` to exist. Both are carried into `P0-17` and `P0-18` rather than marked done, and `P0-16` stays `WIP` until they land.

Also written on the way past: **ADR-012**, the audit write-semantics decision. It was referenced by four code comments and a change record but had never been written into `DECISIONS.md`, which `P0-12`'s Definition of Done required. A reference to a decision that does not exist is worse than no reference — it reads as though the reasoning was recorded somewhere.
