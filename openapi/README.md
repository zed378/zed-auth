# openapi/

`openapi.yaml` is the **single contract artifact** for the Management REST API.

**Governing documents**: `docs/PLAN/05-API-CONTRACT.md` (the contract itself), `docs/PLAN/20-PUBLIC-SITE-ARCHITECTURE.md` § API Reference Generation. Where this file and `docs/PLAN/05` disagree, `docs/PLAN/05` is right and this file is a bug.

---

## Spec-first, and why that is the stronger choice

The spec is hand-written and **generates the code**, not the other way round ([ADR-013](../MEMORY/DECISIONS.md)).

`docs/PLAN/05` § Documentation accepts either direction — "generated from code or validated in CI" — so the decision turned on which one makes drift *impossible* rather than merely *detectable*. Code-first from Go annotations feels safer because the annotation sits next to the handler, but an annotation is a comment: it can say `200` while the handler returns `201` and nothing objects.

Spec-first with generated server interfaces makes it a compile error instead:

```
backend/internal/httpserver/health.go
    var _ api.StrictServerInterface = (*Health)(nil)
```

Change a response shape in the spec, regenerate, and every handler that no longer matches fails to build. That is enforcement in the editor rather than a CI message after the push.

**Adding an endpoint means editing the spec first.** That ordering is the discipline cost, and it is also the point — the contract is designed before the handler, which is what API-first (`docs/PLAN/02` FR-14) means in practice rather than as an aspiration.

---

## What is generated

| Artifact | From | Consumer |
|---|---|---|
| `backend/internal/api/api.gen.go` | `oapi-codegen`, chi + strict-server | The Go handlers implement its `StrictServerInterface` |
| Console TypeScript client | same spec | `console/` (wired in `P0-17`) |
| `/docs/api-reference` | same spec | `public-site/` (wired in `P0-18`) |

The generated Go file is **committed**. A reviewer sees the contract change and its consequences in one diff, and a fresh clone builds without a code-generation step. CI regenerates and fails on any difference.

```bash
make openapi-generate   # regenerate after editing the spec
make openapi-lint       # redocly lint
make openapi-check      # lint + fail if the generated code is stale
```

---

## Two rules that are not style preferences

### The spec documents only what has shipped

`/docs/api-reference` renders from this file, so **an endpoint documented here is a public claim that it exists**. `docs/PLAN/05` Part B lists the whole `/v1` surface and writing it all out now as a design exercise would publish that claim for every unbuilt endpoint at once — exactly what `docs/UI-UX/21`'s governance rule and `CLAUDE.md` forbid.

`scripts/openapi-shipped-paths.py` enforces this in CI and in `scripts/check.sh`. Adding an endpoint means adding it to `SHIPPED` in the same commit. The duplication is deliberate: it makes publishing an endpoint a two-line act rather than a side effect of editing YAML.

The shared **component schemas are different in kind** and are deliberately ahead of the endpoints. They are contract infrastructure — the error envelope, the pagination token, the common parameters — and settling them now is the point of `P0-16`. An error format that changes after twenty endpoints exist is a breaking change to all twenty.

### 3.0.3, not 3.1

`oapi-codegen` prints `OpenAPI 3.1.x is not yet supported ... some functionality may not be available` and then generates anyway. Generating anyway is the problem: the whole value of spec-first here is that the generated interface is trustworthy enough to be the enforcement mechanism, and a generator that has told you it may be silently incomplete cannot be that.

Nothing in this contract needs 3.1. The cost is `info.summary`, `license.identifier`, and schema-level `examples` arrays. Tracked as `BACKLOG` `SL-03` for when the generator catches up.

---

## Something this file already caught

Writing the spec against the running service rather than from memory turned up that the two probes report different vocabularies: `/healthz` returns `{"status":"ok"}` and `/readyz` returns `{"status":"ready"}`.

It is recorded accurately rather than tidied. A consumer already parsing `ready` would break if the server were changed to match a prettier spec, so the difference is now pinned by a test that says so. That is a small thing, and it is the whole argument for spec-first in one example: the contract was never wrong, it was just never written down where anything could check it.
