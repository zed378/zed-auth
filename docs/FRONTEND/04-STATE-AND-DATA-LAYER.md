# 04 - State and Data Layer

> Category: **Frontend Engineering** (`docs/FRONTEND/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-22, P2-13, P4-04, P4-06, PF-20 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

Describe how the console reads and writes server data: the generated client, the query-key
scheme, pagination, cache invalidation, and the tenancy scoping that stops one
organization's rows appearing under another.

## Scope

`console/src/lib/api/`. Authentication is [`05-AUTHENTICATION-AND-SESSION.md`](./05-AUTHENTICATION-AND-SESSION.md).

## As Built

### There is no global store

Server data lives in TanStack Query and nowhere else. Two pieces of genuine client state
exist — which organization the console is acting in (`console/src/lib/org/OrgProvider.tsx`)
and the session (`console/src/lib/auth/AuthProvider.tsx`) — and both are React context.
Everything else is either a query result or a `useState` inside the screen that owns it.

Caching API responses in a global store means two copies of the same row with no rule
about which is right. `docs/PLAN/06-FRONTEND-ARCHITECTURE.md` settles it and `AGENTS.md`
repeats it.

### The client is generated, and hand-written requests are the bug it prevents

`openapi/openapi.yaml` → `console/src/lib/api/schema.gen.ts` (via `openapi-typescript`),
called through `openapi-fetch` in `console/src/lib/api/client.ts`. The backend implements
an interface generated from the same file (ADR-013), so the spec is the one place the two
sides meet.

A request written by hand against a path string is the drift generation exists to prevent,
and it will be the call that keeps working locally and fails in staging. `npm run typecheck`
turns a wrong path or a wrong body into a compile error.

Three details in the client that are easy to get wrong:

- **`baseUrl` is injected at build time**, defaulting to `window.location.origin`. A
  runtime-configurable API base URL in a static SPA means anyone who can influence that
  value redirects every bearer token the console holds. An *empty* base is not the same
  thing: it produces relative paths, which `fetch` outside a document cannot parse, and
  the failure names neither the console nor the endpoint.
- **`credentials: "omit"`.** `/v1` reads no cookie; the SSO session cookie belongs to the
  silent-authentication *navigation*, not to these fetches. Sending one would be ambient
  authority created for no purpose, and would force the service to answer
  `Access-Control-Allow-Credentials: true` — a larger claim than this API needs to make.
- **`fetch` is resolved per request**, not captured at module load. `openapi-fetch` takes
  `globalThis.fetch` once when the client is created, so anything installed afterwards —
  instrumentation, a late service worker, a polyfill, a test stub — is silently skipped.

### Query keys are centralised and organization-scoped

`queryKeys` in `console/src/lib/api/client.ts` is the single list. Every key is combined
with the active organization id at the call site:

```ts
queryKey: [...queryKeys.receivedGrants, orgId]
```

Without that, switching organizations renders the previous one's cached data — a leak in
the UI even with a correct API. `PF-20` is on the watch list for exactly this.

The one key deliberately **not** scoped is `administered`: it describes the *caller*, and
re-fetching it on every switch would empty the organization switcher the moment it is
used.

Keys are hierarchical, so invalidating `["organizations"]` also clears every organization
detail beneath it.

### Switching organizations clears the cache

Scoped keys mean stale data cannot be *served* for the wrong organization. It would still
be sitting in memory, and any key that forgot its `orgId` would leak silently. Clearing on
switch is the measure that does not depend on every future hook remembering
(`console/src/lib/org/OrgProvider.tsx`).

The active organization travels in the URL (`?org=`), so a link is shareable and a refresh
lands in the same place. A context held only in memory turns "send me that page" into
"send me that page and also click the switcher first"; one held only in storage makes two
tabs fight.

Forcing `?org=` to an organization the caller does not administer produces a console that
asks and an API that refuses every request. That is the intended behaviour, and an E2E
test drives exactly that URL.

### Pagination follows tokens, with a bound that is reported

`collectPages` in `console/src/lib/api/queries.ts` follows `page_info.next_page_token` up
to **20 pages × 100 rows** and returns `{ items, complete }`.

This replaced a single 100-row read. The API orders most collections oldest first, so past
100 rows the **newest** were the ones missing: an administrator who had just invited
somebody could not find them, and nothing said the list was incomplete. Counts built from
those lists were wrong the same way.

`complete` travels with the rows, and a screen that can reach the bound must say so.

### The query defaults are set deliberately

`console/src/App.tsx` configures one `QueryClient` for the application:

| Default | Value | Reason |
|---|---|---|
| `staleTime` | 30 s | Authorization data goes stale in a way that matters. A role revoked in another tab should not linger behind a long window; a screen that can afford more says so explicitly |
| `refetchOnWindowFocus` | off | Refetching on every focus is noisy for an administrator alt-tabbing between the console and a ticket |
| `refetchOnReconnect` | on | A resumed laptop showing pre-suspend permissions is worse than the noise |
| `retry` | none on 4xx, then twice | A 403 does not become a 200 on the third attempt, and retrying it three times is three audit events for one mistake |

### Mutations invalidate; they do not patch the cache

A write calls `queryClient.invalidateQueries` for every key it could have changed, and the
next render reads the server's answer. Optimistic cache surgery would mean the console
deciding what the server did — which for an authorization system is a second
implementation of the rules.

`P4-06`'s panel is the pattern: assigning a delegated role invalidates both the
assignments for that grant **and** the received-grants list, because `holder_count` on the
row behind the panel is now stale.

### Cache invalidation the server drives

The service invalidates its own authorization cache by **grant generation** rather than
per-user deletion (`P4-04`). The console does not participate in that and must not try to:
a delegated role's revocation window is a server property, documented in
`public-site/docs/guides/authorization-checks.md` and pinned by a drift test.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Page collection bound | 20 × 100, with an explicit notice when reached | `console/src/lib/api/queries.ts`, `console/src/pages/pagination.test.tsx` |
| Every query key carries `orgId` | except `administered` | `console/src/lib/api/client.ts` |
| Switching organizations | clears the whole cache | `console/src/lib/org/OrgProvider.tsx` |
| Requests | generated client only | `console/src/lib/api/client.ts` |
| API base URL | build-time, absolute | `console/src/lib/api/client.ts` |
| Cookies on `/v1` | omitted | `console/src/lib/api/client.ts` |

## Interfaces

- **Read**: one hook per collection in `console/src/lib/api/queries.ts`
  (`useProjects`, `useUsers`, `useProjectGrants`, `useReceivedGrants`,
  `useDelegatedUserGrants`, …). A screen calls hooks, never `api.GET` directly, so the key
  and the bound are decided in one place.
- **Write**: `useMutation` in the screen that owns the action, calling `api.POST`/`PATCH`/
  `DELETE` and invalidating on success.

## Verification

- `console/src/pages/pagination.test.tsx` — the bound, the notice, and that every page is
  walked exactly once.
- `console/src/pages/screens.test.tsx` — screens render loading, error and empty states
  from the hooks.
- `scripts/check.sh` — the generated client is regenerated and diffed against the spec.

## Not Yet Built / Open Questions

- **The 20-page bound is not reachable in practice yet** for most collections; the notice
  is tested rather than observed.

## Related Documents

- [`05-AUTHENTICATION-AND-SESSION.md`](./05-AUTHENTICATION-AND-SESSION.md)
- [`07-FORMS-AND-VALIDATION.md`](./07-FORMS-AND-VALIDATION.md)
- [`../API/`](../API/)
