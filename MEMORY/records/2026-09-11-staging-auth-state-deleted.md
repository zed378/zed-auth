# Staging: `~/auth-state` was deleted, and the signing key was the only thing that did not come back

**Date**: 2026-09-11
**Branch**: `feat/P1-28-acceptance` (operational; the restore itself is not a code change)
**Found while**: verifying `scripts/loadtest/` from the repo on the VM — it exited with "no `/home/infra/auth-state/.env`".

---

## What happened

Zed deleted `~/auth-state` without looking closely at what was in it, and said so as soon as I raised it. Nothing automated did this: nothing in the repository removes that path, no systemd timer or cron job touches it, and the deploy scripts confine themselves to `/tmp` and the checkout.

That directory is deliberately **outside** `~/auth` — `P0-14`'s rule that runtime state must survive a `git pull` or a re-clone. The same property that protects it from a redeploy gives it no protection at all from an `rm`.

| What lived there | Outcome |
|---|---|
| `.env` — DSNs, passwords, issuer, image ref | **Recovered in full** from the running containers |
| `artifacts/console`, `artifacts/public-site` | **Rebuilt** from source and redeployed |
| `bin/keyctl`, `gocache/` | Rebuilt; they are build outputs |
| `secrets/metrics-token` | Regenerated |
| `secrets/jwt-signing-*.pem` | **Gone permanently** |

## Why the service kept working, which was the confusing part

`/healthz` answered `200` and JWKS served the old key for the better part of an hour after the deletion. The service had loaded the private key at startup and held it in memory; the file it came from no longer existed and nothing needed to read it again.

The two nginx containers behaved differently and that difference is the useful lesson. A bind mount resolves to an **inode**, and `rm -rf` unlinks the files inside the directory before the directory itself. nginx does not hold every file open, so its docroot went empty immediately — `console.zedth.my.id` and `app-auth.zedth.my.id` both returned `403` while the auth service carried on as though nothing had happened.

So the blast radius was invisible from the outside for exactly as long as nobody restarted anything.

## The signing key could not be recovered, and the backup is why

There is a nightly backup, `zed-auth-backup.timer`, and it had run cleanly at 03:25 that morning. It dumps the **database**, restores it into a throwaway instance and compares every row count — a better backup than most.

It does not contain the signing key. `signing_keys.private_key_ref` is a *reference*; the private half is a `0400` file in the secrets directory, and nothing backs that directory up.

`docs/PLAN/15-DISASTER-RECOVERY.md` line 9 says "Signing keys backed up separately from the database, with restricted access" — a decision that was written down and never implemented, which is a worse state than one that was never decided, because the recovery procedure that depends on it reads as though it works. Logged as **`PG-29`**, with the detail.

Recovery was therefore `keyctl generate` → `rotate` → `retire` on both orphaned key ids, following `deploy/vm/RUNBOOK-key-rotation.md`. Every token and session issued before 14:49 stopped verifying. On staging that costs nothing; the same hour in production is every consumer application signed out at once.

## Two things the restore itself exposed

**`sudo` strips the environment, and `secrets.sh` guessed.** `AUTH_SECRETS_DIR` defaults to `/etc/zed-auth/secrets` — the *container's* path. The runbook's own `sudo ~/auth/deploy/vm/secrets.sh fix` therefore created a brand-new secrets directory at the default path on the host, reported success, repaired nothing, and wrote a **live metrics token** to a location no inventory knows about. I did this, noticed it in the output, and removed it.

Repairing a directory that exists is safe whatever the path. *Creating* one is the operation that succeeds in the wrong place, so `secrets.sh` now refuses to create a directory it was not explicitly told to use, and names the right command in the refusal. The runbook and `deploy/vm/README.md` pass the variable explicitly.

**The service warns that it cannot identify clients.** On start: *"no client IP source is configured; per-IP rate limiting will treat every request as one client if anything is proxying in front of this service"*. It is pre-existing rather than something the rebuilt `.env` lost — the compose file never passed `AUTH_CLIENT_IP_HEADER` or `AUTH_TRUSTED_PROXY_CIDRS`, so they could not have been set. Behind the Cloudflare tunnel every request arrives from one address, which means the login limiter's bucket is shared by everybody: one brute-force attempt locks out every user of the deployment. `PG-19` already owns per-client rate limiting; this is its staging face, and it is worth saying out loud that today's "a brute force was blocked after 7 attempts" was measuring a shared bucket.

## How the `.env` was rebuilt, and why nothing was printed

Every value came from `docker inspect` on the still-running containers and went straight into the file. Nothing passed through a terminal: the DSNs carry the database passwords and an SMTP URL can carry `user:password@`, and a restore verified by reading the credentials aloud has put them somewhere new.

Verification compared SHA-256 prefixes rather than values — 14 keys matched — and then did the thing that actually matters: used the recovered credentials to open the database and authenticate to Redis. Plus one check that a **wrong** Redis password is refused, because two passing credential checks against a server that accepts anything prove nothing.

The writer quotes values as `'…'` with embedded quotes escaped, and that was round-tripped against `$`, backticks, spaces, backslashes and quotes **before** it touched a real credential. An unquoted `$` in a password would have been expanded on `set -a; . .env`, producing a different password and a failure that reads as a wrong credential rather than a mangled one.

## Verified after the restore

`scripts/loadtest` and the `P1-28` acceptance harness, against the live hostnames:

| | |
|---|---|
| Acceptance walk, all eight Phase 1 criteria | **35 passed, 0 failed** |
| Sign-in through demo A, token verified by the app itself | ✔ |
| Silent SSO into demo B with no second prompt | ✔ |
| Console deep links (`/`, `/projects`) | `200`, client id baked into the bundle |
| Public site, quickstart, API reference, changelog | `200` |
| JWKS | exactly one key, `I7jnBopn…`, the new one |
| Audit log still append-only to the runtime role | ✔ |
| Fixtures left behind | 0 |

## What I would tell the next person

The service being healthy told us nothing about whether it could survive a restart, and that gap lasted an hour. "Is it up?" and "can it come back?" are different questions, and only the second one was interesting that afternoon.
