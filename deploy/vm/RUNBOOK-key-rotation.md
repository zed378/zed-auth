# Runbook — Rotating the Token Signing Key

**Cadence**: every 90 days (`PLAN/09` § Tokens & Keys).
**Duration**: about 10 minutes of work, then a wait measured in token lifetimes.
**Blast radius if done wrong**: every user logged out, every consumer application rejecting every token.

Rotation is a **deliberate operator action**, not a scheduled job. A rotation that fails at 03:00 is worse than one done at 11:00: the failure modes are a key that cannot be read, or a consumer whose JWKS cache has not refreshed — both want a person watching (`P1-03` step 5).

---

## The shape of it

Four states, and the two middle ones are the whole point:

```
generate ──▶ next ──rotate──▶ current ──rotate──▶ previous ──retire──▶ retired
             │                │                   │                    │
       published,          signing            still verifies,      gone from JWKS;
       not signing                            no longer signs      its tokens are dead
```

**Why a key is published before it signs.** Consumers cache JWKS. If the first token signed with a new key arrives before the consumer has fetched that key, the consumer rejects a perfectly good token. Publishing first removes the race.

**Why a key keeps verifying after it stops signing.** A token issued one second before the rotation is valid for its full lifetime. Retiring immediately would kill it. This overlap is what `PLAN/09` means by an overlap period.

**Never skip from `current` straight to `retired`.**

---

## Before you start

There is no Go toolchain on the VM, so `keyctl` runs from a container. Build it
once after a `git pull`:

```bash
cd ~/auth
docker run --rm -v "$PWD:/src" -w /src/backend \
  -v /home/infra/auth-state/bin:/out \
  -v /home/infra/auth-state/gocache:/gocache -e GOCACHE=/gocache -e GOMODCACHE=/gocache/mod \
  golang:1.26.6 go build -buildvcs=false -o /out/keyctl ./cmd/keyctl
```

Then define the wrapper. **Read the mount paths before you paste this** — they
are the whole correctness of the procedure:

```bash
set -a && . /home/infra/auth-state/.env && set +a

kc() {
  docker run --rm --network zedauth_default \
    -v /home/infra/auth-state/bin:/bin/kc \
    -v /home/infra/auth-state/secrets:/etc/zed-auth/secrets \
    -e AUTH_MIGRATE_DSN="postgres://auth_owner:${AUTH_POSTGRES_OWNER_PASSWORD}@postgres:5432/auth?sslmode=disable" \
    -e AUTH_SECRETS_DIR=/etc/zed-auth/secrets \
    debian:12-slim /bin/kc/keyctl "$@"
}
```

**`/etc/zed-auth/secrets` is not decoration.** `keyctl generate` records the
path it wrote to, and the *service* is what later reads it. Mounting the
secrets anywhere else produces a reference that resolves for the tool and not
for the service — a row pointing at a file that does not exist, and a service
that refuses to start at the next restart with no obvious connection to a
rotation performed hours earlier.

The first execution of this runbook did exactly that, with the secrets mounted
at `/secrets`. Matching the service's own mount point makes the reference
correct by construction rather than by remembering.

If the reference is already wrong, it is a one-line repair — see
**Fixing a wrong key reference** below.

**Pre-checks.**

```bash
# 1. What exists now. Expect exactly one `current`.
kc list

# 2. The service is healthy before you change anything.
curl -sf https://auth.zedth.my.id/healthz

# 3. A backup exists. Rotation is a database write.
ls -la /home/infra/auth-state/backups/ | tail -2
```

If `list` shows anything other than exactly one `current`, **stop** and work out why before continuing.

---

## Step 1 — Generate

```bash
kc generate
sudo ~/auth/deploy/vm/secrets.sh fix     # ownership: the service reads it, you do not
```

The new key is now in `next`: published in JWKS, not signing.

**Verify it is published:**

```bash
curl -s https://auth.zedth.my.id/.well-known/jwks.json | jq '.keys[].kid'
```

The new `kid` must appear. If it does not, the service has not refreshed its key cache yet — wait for the TTL (5 minutes) and look again.

---

## Step 2 — Wait

**Wait at least as long as your consumers cache JWKS.** For most libraries that is 5–15 minutes; if you do not know, an hour is safe and costs nothing.

This wait is the step people skip, and skipping it is what turns a rotation into an incident: the new key starts signing before consumers have it, and every token is rejected until their caches refresh.

---

## Step 3 — Rotate

```bash
kc rotate
```

The new key is now `current` and signs. The old one is `previous` and still verifies.

**Verify immediately:**

```bash
# Both keys are published.
curl -s https://auth.zedth.my.id/.well-known/jwks.json | jq '.keys[].kid'

# The service is still healthy.
curl -sf https://auth.zedth.my.id/healthz

# New tokens carry the new kid. (From Phase 1 onward, once tokens are issued.)
```

**If something is wrong, roll back now** — see below. The old key is still `previous`, so this is reversible until you retire it.

---

## Step 4 — Wait again, then retire

Leave the old key in `previous` for **at least one full access-token lifetime plus a margin**. With 15-minute tokens, an hour is comfortable. There is no cost to waiting longer, and retiring early kills tokens that are still legitimately in use.

```bash
kc retire <old-kid>
```

After this, tokens signed by that key stop verifying. That is the intended effect and it is not reversible in practice — a client still holding such a token now has a dead one.

---

## Fixing a wrong key reference

If `private_key_ref` points at a path the service cannot see — the mistake
above — the key material is fine and only the reference is wrong:

```sql
UPDATE signing_keys
SET private_key_ref = replace(private_key_ref, 'file:/secrets/', 'file:/etc/zed-auth/secrets/')
WHERE private_key_ref LIKE 'file:/secrets/%';
```

Then confirm the service can start:

```bash
docker restart zedauth-authservice-1 && sleep 15
docker logs zedauth-authservice-1 2>&1 | tail -5
```

Nothing is lost by this: the reference is metadata, the key itself never moved.

---

## Rolling back

**While the old key is still `previous`** — generate nothing, just promote it back:

```sql
BEGIN;
UPDATE signing_keys SET status = 'previous' WHERE status = 'current'  AND purpose = 'oidc';
UPDATE signing_keys SET status = 'current'  WHERE kid = '<old-kid>'   AND purpose = 'oidc';
COMMIT;
```

The order matters: demote before promoting, or the partial unique index on `current` rejects the transaction. That index is doing real work — it makes "two keys signing at once" unrepresentable.

**After retiring**, there is no rollback. Tokens signed by a retired key are dead. This is why step 4 waits.

---

## If the service will not start after a rotation

It refuses to start rather than running without a signing key, deliberately: a service that accepts requests and fails every login is a worse outage and a much harder one to diagnose.

```bash
docker logs zedauth-authservice-1 2>&1 | tail -20
```

| Message | Cause | Fix |
|---|---|---|
| `no current signing key` | The rotation did not commit | `keyctl list`, rotate again |
| `resolving private key for ...` | The key file is missing or unreadable | `sudo secrets.sh fix` — usually ownership |
| `resolving private key ... no such file` | `private_key_ref` records a path the service cannot see | See **Fixing a wrong key reference** |
| `signing key is too short` | An RSA key below 2048 bits | Generate a new one; do not lower the bound |
| `recorded as RS256 but is an ES256 key` | Row edited by hand | Correct the `algorithm` column |

---

## What must never happen

- **The private key must never leave the VM.** `PLAN/02` § Constraints is absolute. Do not copy it to a laptop to "look at it"; a key that has been on a laptop has been on a laptop.
- **Never store key material in `signing_keys.private_key_ref`.** The column holds a reference. A `CHECK` constraint refuses PEM headers, which is the one thing stopping this "simplification".
- **Never rotate and retire in the same maintenance window.** The gap between them is the feature.

---

## Executed against staging

| Date | Operator | Outcome |
|---|---|---|
| 2026-09-09 | `P1-03` | First rotation. See the MEMORY record. |
