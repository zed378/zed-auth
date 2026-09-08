# Clone-Based Deploy, and the State That Lived in the Code Directory

**Date**: 2026-09-08
**Type**: Operational event + infrastructure change
**Closes**: `OQ-11`, `OQ-12`

---

## What Happened

The owner switched the VM from ad-hoc file uploads to a `git clone` of the repository — a better arrangement, and the right answer to `OQ-11`, since a pull needs no inbound access to a machine with no public IP.

Deleting `~/auth` and re-cloning destroyed four things that lived inside it:

| Lost | Consequence |
|---|---|
| `secrets/jwt-signing-current.pem` | The signing key. Nothing had been signed with it yet |
| `secrets/metrics-token` | The metrics bearer token |
| `backups/` | **Every backup**, including the one verified hours earlier |
| `console/dist`, `public-site/build` | Both frontends went unhealthy; rebuildable |

The database survived — it lives in a Docker named volume, not in the directory.

The auth service kept running because its mounts were already open. **It would not have survived a restart**, and nothing would have said so until it was restarted. That is the worst shape for a fault: invisible until the moment you need the thing to work.

Nothing here was serious. Staging, no users, no tokens issued, live data intact. It is worth a record because the *cause* is general and the fix is structural.

---

## The Cause

**Runtime state lived inside the code directory.** Secrets, backups and build artifacts sat under `~/auth`, alongside the checkout.

That was never a decision. It was where things landed when the deployment was files-uploaded-into-a-directory, and it stayed there because nothing forced the question. Then a perfectly reasonable operation — delete and re-clone — became destructive, and there was no warning because the arrangement had never been written down as an assumption.

The general shape: **an operation is only safe if the layout makes it safe.** "Do not delete that directory" is not a control, because the person deleting it is doing something ordinary and has no reason to suspect otherwise.

---

## The Fix

Runtime state moved out of the checkout entirely:

```
/home/infra/auth/          git clone — disposable, `git pull` always safe
/home/infra/auth-state/    mode 700
    .env                   mode 600
    secrets/               owned by uid 65532, mode 700/400
    backups/
    artifacts/console      built by CI or locally, copied here
    artifacts/public-site
```

The compose files already read every path from an environment variable — `AUTH_SECRETS_DIR`, `AUTH_CONSOLE_DIST`, `AUTH_SITE_DIST`, `AUTH_BACKUP_DEST` — so this was a change to `.env` rather than to the deployment. That indirection was written for a different reason and paid for itself here.

`git pull` and even a full re-clone are now non-destructive.

---

## Three Things Found While Recovering

**`.env` was world-readable.** Mode `664`, containing the database passwords. A file recreated by hand picks up the shell's umask, and nothing was checking. Now `600`, in the state directory.

**No script was executable in a fresh clone.** `secrets.sh` failed with `command not found` at the moment it was needed to regenerate the destroyed signing key.

On Windows, git does not track the executable bit unless `core.fileMode` is set, so every `chmod +x` made locally never reached the repository. Fifteen scripts were committed `100644` while being executable in the working tree — and `check.sh` had a gate for exactly this that passed, because it tested the *filesystem* rather than the *index*. A clone gets the index.

The gate now reads `git ls-files -s`. It found one more on its first run: `deploy/postgres/init/01-roles.sh`, which Docker executes at database initialisation.

**Backups were written to the wrong place, twice.** Setting up the state directory I invented `AUTH_BACKUP_DIR`; the script reads `AUTH_BACKUP_DEST`. The dump silently went to the old path — which was still *inside the checkout*, so the fix had reintroduced the original bug. Caught because the new directory was empty after a run that reported success.

Then fixing it left **two** `AUTH_BACKUP_DEST` lines in `.env`. Shell sourcing takes the last; a person reading top-down takes the first. Deduplicated.

The lesson in both: **read the script rather than guessing its variable names**, and a config file with a duplicated key is a trap even when it currently behaves.

---

## Recovered and Verified

| Check | Result |
|---|---|
| Secrets regenerated, owned by uid 65532 | mode 400 |
| Auth service restarted **with the new secrets** | healthy — the thing that would have failed before |
| Console, public site | healthy, artifacts rebuilt |
| `auth.zedth.my.id` `/healthz` `/readyz` | 200 |
| `console.zedth.my.id` incl. a deep link | 200 |
| `app-auth.zedth.my.id` incl. a docs page | 200 |
| Fresh backup, restore-verified | 19 tables, row counts match the source |
| Backup timer | repointed at the state directory |

---

## `OQ-11` — Answered: no GitHub Actions

The owner's answer: the VM has no public IP, so nothing external can reach it, and GitHub Actions will not be used for deployment.

That settles it in the direction the open question recommended. Deployment is pull-based: the VM runs `git pull`, builds or receives artifacts, and restarts its own services. Nothing outside holds a credential to the machine, which is the property that matters more than the convenience of a push-based pipeline.

`P0-20`'s "a merge to `main` deploys to staging automatically" is therefore **not** going to be satisfied as written, and should be re-read rather than left failing: the intent was fast, repeatable deployment, and a pull on the VM achieves that without the trust relationship the original phrasing assumed.

## `OQ-12` — Answered: local for now, S3 or NFS later

The owner accepts local-only backups for the moment, with object storage or NFS planned.

This event is a small demonstration of the cost: the re-clone destroyed every backup, and only the Docker volume saved the data. `backup.sh` continues to warn on every run, which is correct — the warning should keep appearing until `AUTH_BACKUP_REMOTE` is set.

---

## Outstanding

- The secrets were regenerated with no consequence because nothing had been signed. **After `P1-03` that stops being true**: destroying the signing key will invalidate every issued token. The state directory now protects it; the backup story for it does not exist yet and belongs with `P1-03`'s key rotation work.
- Artifacts are still copied to the VM by hand. With a clone in place, the VM could build them itself — worth doing when the frontend changes often enough to be annoying.
- The VM password from the original transcript is still unrotated.
