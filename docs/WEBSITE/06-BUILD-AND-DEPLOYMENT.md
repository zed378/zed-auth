# 06 - Build and Deployment

> Category: **Public Website** (`docs/WEBSITE/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-18, P0-19, P0-10 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

How the site is built, which checks run against the build rather than the source, and how
the output reaches staging.

## Scope

`public-site/package.json`, `deploy/public-site/`.

## As Built

### The build writes `robots.txt` as part of itself

```
"build": "docusaurus build && node scripts/write-robots.mjs"
```

Not a separate step somebody can forget, because forgetting it publishes a sitemap pointer
to a host that does not exist and nothing 404s.

### `npm run check` is a sequence, and the order matters

```
check:tokens → check:contrast → check:boundary → typecheck → build → check:claims → check:leak
```

The last two run **after** the build, against `public-site/build`, because that is what a
visitor sees. A claims check over the source would miss anything a component interpolates,
and a leak check over the source would miss anything the build inlines.

| Step | Fails when |
|---|---|
| `check:tokens` | the site's brand values have drifted from the console's |
| `check:contrast` | any pair falls below WCAG 2.1 AA in either theme |
| `check:boundary` | any code is shared with the console |
| `typecheck` | — |
| `build` | a broken internal link, among the usual |
| `check:claims` | an unlabelled capability, or a label that contradicts the roadmap board |
| `check:leak` | eight consecutive words from a never-publish document appear in the output |

### Independent deployment, meant literally

`deploy/public-site/docker-compose.yml` is its own compose project
(`zedauth-site-web-1`), not a service inside another one:

> "independent" that shares a lifecycle is not independent.

The build is produced locally or in CI and copied up; a Node toolchain on the VM is a
dependency it does not otherwise need.

### The bind-mount trap, in writing, because it has bitten twice

> Deploying a new build: **extract in place, do not swap the directory.**
>
> The obvious safe-looking sequence — unpack to `.new`, `mv` the old aside, `mv` the new
> into place — breaks this mount. A bind mount resolves to an inode, so after the swap the
> container is still serving the directory that was moved away.

Every request 404s while the files are correct on disk, which looks exactly like a
permissions problem. The alternative is `--force-recreate` after the swap.

The second trap: **source the environment before `docker compose up`**. The mount is
`${AUTH_SITE_DIST:-…}`; without it the default path inside the checkout is mounted, it is
empty, and nginx answers 403 with the right files sitting on disk.

### The API reference is regenerated in the same commit as a contract change

See [`03-GENERATED-API-REFERENCE.md`](./03-GENERATED-API-REFERENCE.md). `scripts/check.sh`
fails on a stale reference, which is how it is caught rather than noticed.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| `robots.txt` | generated during the build | `public-site/package.json` |
| Claims and leak checks | run against the build output | `public-site/package.json` |
| Deployment | its own compose project | `deploy/public-site/docker-compose.yml` |
| New build | extracted in place, or `--force-recreate` | `deploy/public-site/docker-compose.yml` |
| Environment | sourced before `up` | `deploy/` runbook |
| Node on the VM | none | by design |

## Verification

- `npm run check` in `public-site/`.
- `scripts/check.sh` § Public site — brand tokens, contrast, no shared code, generated
  reference, and (with `CHECK_FULL=1`) the build, capability audit and leak check.

## Not Yet Built / Open Questions

- **No CDN.** Staging serves from nginx on the VM.
- **No automated deployment.** The VM has no public IP, so the rollout is manual and
  pull-based (`P0-20`).
- **No preview build per branch.**

## Related Documents

- [`05-SEO-PERFORMANCE-AND-ACCESSIBILITY.md`](./05-SEO-PERFORMANCE-AND-ACCESSIBILITY.md)
- [`../DEVOPS/`](../DEVOPS/)
- [`../FRONTEND/11-BUILD-AND-DELIVERY.md`](../FRONTEND/11-BUILD-AND-DELIVERY.md)
