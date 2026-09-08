# Metrics Endpoint Authentication, and Two Deployment Failures Worth Keeping

**Date**: 2026-09-08
**Task**: `P0-11` (follow-up)
**Branch**: `feat/P0-11-admin-auth`

---

## Why This Exists

The metrics listener was bound to loopback and reached through a tunnel, which was sufficient while nothing else could route to it. Making it reachable at `metrics.zedth.my.id` changes that: the only thing between an unauthenticated reconnaissance summary — request rates, error rates, login success and failure counts, pool saturation, service version — and whoever can reach the address becomes one piece of network configuration.

Network configuration is not a bad control. It is a control that fails silently and is edited by people who do not know the rule exists. So the endpoint now authenticates on its own behalf, and the tunnel policy is the second layer rather than the only one.

---

## What Was Built

**`AdminConfig.TokenRef`** — a secret *reference*, resolved through the same `file:`/`env:` indirection as every other secret (`P0-14`). The token lives in a file with checked permissions rather than in an environment variable visible in `docker inspect`.

**`requireBearerToken`** — constant-time comparison, and a deliberately bare `404` on failure. Not `401`: a `401` with a `WWW-Authenticate` header confirms to anyone probing that the endpoint exists and tells them what it wants. The rejection carries no body and no challenge header. Tested for exactly that, since it is the kind of property a later "helpful error messages" refactor would quietly undo.

**A config guard that refuses to boot** when `AUTH_ADMIN_ADDR` is not loopback and no token is configured. The guard caught two real misconfigurations within an hour of being written — both of them mine.

---

## Failure 1: The Guard Caught Me First

The compose files passed `AUTH_ADMIN_ADDR: "0.0.0.0:9090"` but not `AUTH_ADMIN_TOKEN_REF`. The service refused to start, correctly.

Worth noting *why* the bind is `0.0.0.0` and why that is not itself the bug: inside the container it must be, because the host publishes the port to `127.0.0.1` only and the process cannot see the host's port mapping. The service correctly declines to assume a mapping it cannot verify. The fix was to make the variable required at the compose layer too:

```yaml
AUTH_ADMIN_TOKEN_REF: ${AUTH_ADMIN_TOKEN_REF:?required when the metrics listener binds beyond loopback}
```

Which then broke `scripts/check.sh`, because `${VAR:?}` also fails static validation with no environment. The check now supplies a synthetic value — its job is to validate syntax, not to deploy.

---

## Failure 2: `chmod 400` Is Necessary and Not Sufficient

The token file was mode `400` owned by `infra`, exactly as documented. The service still could not read it:

```
resolve admin token: stat secret file /etc/zed-auth/secrets/metrics-token: permission denied
```

The file plainly existed and plainly had the right mode. The directory containing it was mode `700` owned by `infra` (uid 1000), and the runtime image is distroless `:nonroot`, so the process runs as uid **65532**. The container could not *traverse* the directory. The mode on the file was never reached.

The repair that comes to mind first — make the file group-readable — is refused by the secret resolver, correctly: a group-readable secret is readable by every other service on the host. That leaves exactly one shape:

> **The secrets directory and its contents belong to the service's uid, not to the operator.** `0700` and `0400`, owned by `65532:65532`. The operator reads them with `sudo`.

That is the right relationship anyway. The operator is not the service.

Encoded in `deploy/vm/secrets.sh` rather than in prose, because the prose already said `chmod 400` and the prose was not wrong — it was incomplete, and incomplete instructions read as complete ones. The signing key had the same wrong ownership and would have failed identically in `P1`, at a moment when the cause would have been much less obvious.

---

## Failure 3: One Cause, Three Symptoms, None of Them the Cause

While the token was unreadable, the logs showed this on repeat:

```
ERROR msg=initial partition maintenance failed error=begin transaction: sql: database is closed
```

Nothing was wrong with the database. Secrets were being resolved late in `run()`, next to the listener that used them — *after* the pool was opened and *after* the partition-maintenance goroutine had started. So a secret failure returned an error, the deferred pool close ran, and the still-running goroutine reported the closed pool on its way out. The loudest and most repeated message in the log was a consequence three steps removed from the cause, and it pointed at the wrong subsystem.

Secrets are now resolved in one place, immediately after configuration loads, before anything is opened or started. A bad secret reference is one error message and nothing else.

The general shape is worth keeping: **startup failures should be ordered so that the first thing to fail is the thing that is wrong.** Anything that acquires a resource or starts a goroutine before validation is complete will produce a second, louder, misleading error.

---

## Also Fixed

`vmssh.py`'s shell quoting wrote the POSIX escape for an embedded single quote inside a non-raw Python string, so Python collapsed it to three quotes. Any remote command containing an apostrophe was silently rewritten into a different command. It had been producing truncated output that looked like network flakiness. Replaced with `shlex.quote`.

---

## Verified on the VM

| Check | Result |
|---|---|
| Container | `Up (healthy)` |
| `metrics` with no token | `404`, empty body, no `WWW-Authenticate` |
| `metrics` with a wrong token | `404` |
| `metrics` with the correct bearer | `200` |
| `audit_partition_runway_months` | `3` |
| `https://auth.zedth.my.id/healthz` | `200` |
| `https://metrics.zedth.my.id/metrics` | gated as above |
| `scripts/check.sh` | 22 passed, 0 failed |

---

## Resolved Same Day: the Port Was Closed Instead

`metrics.zedth.my.id` existed for about an hour. Rather than add the Access policy that was still missing, the port was unpublished and the DNS record removed.

That is the better answer, and the reasoning is worth keeping. The token, the tunnel and an Access policy were three controls arranged to make an exposure safe — but nothing on that host scrapes the endpoint, and nothing is planned to. **A port published for a scraper that does not exist is a port open for no one.** The correct amount of defence for an exposure with no purpose is not to defend it well; it is to not have it.

The endpoint stays reachable inside the compose network, which is where a Prometheus container would run if one were added, so nothing was lost. `docker-compose.metrics-port.yml` is the opt-in override for a host-side scraper — loopback-only, with no variable to change the interface, and a header saying what publishing it means.

The token stays, and not as belt-and-braces for its own sake: publishing a port is one line in a file that someone will eventually add for a good reason, and the token is what makes that line safe to add.

## Outstanding
- The VM password appeared in a chat transcript and should be rotated.
- `AUTH_BACKUP_REMOTE` is unset, so backups sit on the same disk as the database they protect.
