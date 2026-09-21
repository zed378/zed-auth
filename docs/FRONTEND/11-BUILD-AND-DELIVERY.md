# 11 - Build and Delivery

> Category: **Frontend Engineering** (`docs/FRONTEND/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-17, P0-10, P1-27 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

Describe how the console is built, what is injected at build time and why, and how the
resulting files reach staging.

## Scope

`console/vite.config.ts`, `console/package.json`, `deploy/console/`. Environments and the
backend's own deployment are [`../DEVOPS/`](../DEVOPS/).

## As Built

### A static bundle, deployed independently of the backend

`vite build` → `console/dist`, served as files. `docs/PLAN/06-FRONTEND-ARCHITECTURE.md`
requires the decoupling in both directions: a console fix must not require shipping the
auth service, and a service release must not require rebuilding the console.

On staging that is literal — the console runs as its own compose project
(`deploy/console/docker-compose.yml`, container `zedauth-console-web-1`), so taking the
console down cannot take the service down.

### `build` typechecks first

```
"build": "tsc --noEmit && vite build"
```

Vite does not typecheck; it transpiles. Without the first half, a type error ships.

### Three values are injected at build time

| Variable | Why build time |
|---|---|
| `VITE_API_BASE_URL` | A runtime-configurable API base URL in a static SPA means anyone who can influence that value redirects every bearer token the console holds. Defaults to `window.location.origin` |
| `VITE_AUTH_ISSUER` | The identity provider the console authenticates against |
| `VITE_AUTH_CLIENT_ID` | The console's own OIDC client, per environment |

Because they are baked in, **a new environment means a new build**. That is the trade for
not letting a deployed artifact be re-pointed at a different service.

### The build fails loudly rather than shipping something slow

`chunkSizeWarningLimit: 600` — a chunk large enough to hurt first paint on the login
redirect is a warning, not a silent regression. `sourcemap: true`, because a production
stack trace that names `index-a1b2c3.js:1:48211` is a stack trace nobody can act on.

### Reaching staging

There is no Node toolchain on the VM, so the bundle is **built locally and copied up**.
The artifacts are bind-mounted from `/home/infra/auth-state/artifacts/console`, outside
the checkout, because a re-clone once destroyed state that a redeploy has to survive.

Two operational traps are recorded in the runbook and worth repeating here, because both
present as a permissions problem while the files are correct on disk:

- **Extract over the existing directory, or `--force-recreate` afterwards.** A bind mount
  resolves to an inode; unpacking to `.new` and moving it into place leaves the container
  serving the directory that was moved away, and every request 404s.
- **Source the environment before `docker compose up`.** The mounts are
  `${AUTH_CONSOLE_DIST:-…}`; without the environment the default path inside the checkout
  is mounted, it is empty, and nginx answers 403.

### CI regenerates and diffs

`scripts/check.sh` § Console regenerates the API client, the validation patterns and the
settings bounds from `openapi/openapi.yaml` and fails on a diff. A contract change the
console has not absorbed breaks the build before it breaks a user.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Typecheck before build | `tsc --noEmit && vite build` | `console/package.json` |
| API base URL | build-time injection | `console/src/lib/api/client.ts` |
| Chunk size warning | 600 kB | `console/vite.config.ts` |
| Source maps | on | `console/vite.config.ts` |
| Dev server port | 5173, `strictPort` | `console/vite.config.ts` |
| Generated files match the spec | regenerated and diffed | `scripts/check.sh` |

## Verification

- `scripts/check.sh` § Console — lint, typecheck, unit tests, three generation diffs, and
  (with `CHECK_FULL=1`) the end-to-end suite.
- `scripts/check.sh` § Public site — asserts **no code is shared** between the public site
  and the console, which is what keeps the two deployable separately.

## Not Yet Built / Open Questions

- **No enforced performance budget.** Vite's chunk-size warning is the only signal; there
  is no Lighthouse or bundle-size gate in CI, and `P5-12`/`PF-52` still owe one.
- **No CDN.** Staging serves the bundle from nginx on the VM. `docs/PLAN/06` anticipates a
  CDN; nothing depends on it yet.
- **No preview deployment per branch.** Review is local.
- **The deploy is manual and pull-based.** The VM has no public IP, so there is no GitHub
  Actions path to it; `P0-20` remains open on exactly this.

## Related Documents

- [`01-APPLICATION-STRUCTURE.md`](./01-APPLICATION-STRUCTURE.md)
- [`../DEVOPS/`](../DEVOPS/)
- [`../WEBSITE/`](../WEBSITE/)
