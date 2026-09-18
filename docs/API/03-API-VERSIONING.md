# 03 - API Versioning

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: none opened — raise one against `docs/PLAN/16-IMPLEMENTATION-ROADMAP.md` before a breaking change is attempted &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Record what versioning exists today, and be explicit that the deprecation half of the policy has never been exercised, so a breaking change is not designed against a mechanism this service has not built.

## Scope

The version identifier in the URL and the (currently unimplemented) deprecation signal for it. Not in scope: `openapi.yaml`'s own `info.version` field, which is the spec document's version and unrelated to the path prefix; the OAuth/OIDC protocol surface, which is unversioned by design (see below).

## As Built

- **The version identifier is a path prefix, `/v1`**, applied to every Management API route (`08`–`16`; see `openapi/openapi.yaml`). There is no header-based or media-type-based versioning anywhere in this service.
- **The OAuth/OIDC protocol surface (`/oauth/*`, `/oidc/*`, `/.well-known/*`) and the operational probes (`/healthz`, `/readyz`) are unversioned.** They describe a protocol and a process respectively, not the Management API's evolving contract (`openapi/openapi.yaml` `info.description`: "The operational probes are unversioned: they describe the process, not the API").
- **The Management API has shipped forward from Phase 1 through the current Phase 4 work entirely by additive change** — new routes, new optional fields, new enum values — and has never needed a breaking change. Consequently there is no `/v2` mount, no `Sunset` header, no `Link: rel="sunset"`, and no deprecation registry anywhere in `backend/internal/httpserver/`.
- `docs/PLAN/05-API-CONTRACT.md` Part B states the intended policy for when a breaking change does happen: it goes to `/v2` with a published deprecation window for `/v1`. That policy is documented intent, not implemented mechanism.
- Additive, backward-compatible change is the default posture, matching `CLAUDE.md`'s migration convention (expand/contract): a new optional field or a new endpoint is not, by itself, a version bump.

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Version identifier | `/v1` path prefix | Every route in `openapi/openapi.yaml` under `/v1` |
| OAuth/OIDC surface | Unversioned | `openapi/openapi.yaml` |
| Operational probes | Unversioned | `openapi/openapi.yaml` |
| Breaking-change policy (documented, not yet built) | New major prefix (`/v2`) + published deprecation window for the old one | `docs/PLAN/05-API-CONTRACT.md` Part B |
| Deprecation signal | Not implemented | — |

## Interfaces

Not applicable — there is no versioning-specific endpoint or header to document; the version is purely the `/v1` path segment already covered in `00-API-OVERVIEW.md`.

## Security Considerations

None specific to versioning today. A future `/v2` migration should keep the same authentication and 404-vs-403 disclosure rules `02-AUTHENTICATION-AND-AUTHORIZATION.md` documents; nothing about running two versions concurrently should be allowed to weaken them.

## Verification

Not applicable — there is no deprecation mechanism to test yet.

## Not Yet Built / Open Questions

- **No `/v2`, no deprecation header, no deprecation registry exists.** `docs/PLAN/05`'s policy has never been exercised by a real breaking change.
- What actually triggers a `/v2` is not enumerated: is a new required field breaking? A tightened validation rule that now rejects previously-accepted input? A changed error code for an existing failure?
- The mechanics of running `/v1` and `/v2` against one database and one generated-code pipeline (`openapi/README.md` assumes a single `openapi.yaml`) are undecided — one file with two prefixes, or two files, is an open design question.
- The concrete deprecation signal a client would see (a `Sunset` response header per RFC 8594, a field in the response envelope, a discovery-document entry, or some combination) is undecided.
- Minimum notice period before a deprecated version stops answering is undecided.
- Whether the console and generated TypeScript client would pin to a version or float is undecided.

None of the above should be answered speculatively in this document; the first real breaking change should drive the decision and this document should be updated from that experience rather than a design exercise done in advance.

## Related Documents

- `docs/PLAN/05-API-CONTRACT.md` Part B
- `docs/PLAN/16-IMPLEMENTATION-ROADMAP.md`
- `openapi/README.md`, `openapi/openapi.yaml`
- `docs/API/00-API-OVERVIEW.md`
