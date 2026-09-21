# 03 - Generated API Reference

> Category: **Public Website** (`docs/WEBSITE/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-18, P1-25 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

How `/docs/api-reference` is produced, and the two things that follow from it being
generated rather than written.

## Scope

`public-site/docs/api-reference/` (284 files), `public-site/docusaurus.config.ts`.

## As Built

### It is generated from the contract, and hand-writing it is forbidden

`docusaurus-plugin-openapi-docs` reads `../openapi/openapi.yaml` and emits one `.api.mdx`
per operation plus its parameter, request and status-code JSON. `AGENTS.md` rule 5 and
`docs/PLAN/20` both state the rule:

> Never hand-write API reference documentation. If you change an endpoint, update the
> OpenAPI spec, and the docs follow automatically.

### The two consequences

**An endpoint change is a four-file change, in one commit.** Adding
`GET /v1/organizations/{org_id}/project-grants` in `P4-06` meant: the spec, the shipped-paths
list, `make openapi-generate` for the server's interface, `npm run api:generate` in the
console for its client, and `npm run api:generate` in `public-site` for the reference.

**CI catches the one you forget.** `scripts/check.sh` regenerates the reference and fails
if it differs — *"generated API reference is stale — run: cd public-site && npm run
api:generate"*. That check fired on `P4-06`'s first gate run, which is the system working.

### The spec documents nothing that has not shipped

`scripts/openapi-shipped-paths.py` holds the list of paths the service actually serves, and
the gate fails if the spec documents a route beyond it. Without that, the public reference
would be the easiest place in the repository to publish a promise — a fully rendered,
authoritative-looking page for an endpoint that answers 404.

### The spec's prose is published prose

`info.description` and every operation's `description` are rendered onto the public site.
They are written for an integrator, not for a maintainer. This bit rots quietly: the spec's
description still said "Phase 0 … operational probes only" long after Phases 1–3 shipped,
and it was being published the whole time.

So a spec description is reviewed as **copy**, under
[`02-CONTENT-GOVERNANCE.md`](./02-CONTENT-GOVERNANCE.md)'s rule, not as a comment.

### Regeneration is a clean rebuild

```
"api:generate": "docusaurus clean-api-docs all && docusaurus gen-api-docs all"
```

Clean first, so a removed operation's page is actually removed. An incremental generate
leaves the page for an endpoint that no longer exists, which is the most convincing kind of
wrong documentation.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Reference is generated | never hand-written | `AGENTS.md` rule 5 |
| Regeneration | clean then generate | `public-site/package.json` |
| Staleness | fails the build | `scripts/check.sh` |
| Spec documents only shipped paths | required | `scripts/openapi-shipped-paths.py` |
| Spec prose | reviewed as published copy | `docs/UI-UX/21` governance rule |

## Verification

- `scripts/check.sh` — "generated API reference matches the spec".
- `scripts/check.sh` — "spec claims no endpoint beyond what has shipped".

## Not Yet Built / Open Questions

- **No SDK code samples in the reference.** The plugin can render them; there is no SDK to
  render — see [`../SDK/`](../SDK/).
- **No "try it" console.** Deliberate for now: an interactive caller on a public page needs
  a token, and handing one out is a decision nobody has made.

## Related Documents

- [`../API/`](../API/)
- [`02-CONTENT-GOVERNANCE.md`](./02-CONTENT-GOVERNANCE.md)
- [`06-BUILD-AND-DEPLOYMENT.md`](./06-BUILD-AND-DEPLOYMENT.md)
