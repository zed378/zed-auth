package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// The lookup copy in front of PostgreSQL.
//
// PLAN/12 allows 150ms at p95 for the whole /oauth/authorize silent-SSO
// request. A cache is how that is met, and a cache in front of an
// authoritative store normally means a revocation takes effect after a TTL —
// which is precisely what P1-11's Definition of Done forbids.
//
// So the invalidation is explicit, and so is the race it would otherwise
// leave:
//
//  1. Read: GET. Hit, use it. Miss, read PostgreSQL and populate.
//  2. Revoke: commit to PostgreSQL, THEN delete the key. Deleting before the
//     commit lets a concurrent reader repopulate from the pre-commit state,
//     and the stale entry then outlives the revocation.
//  3. The remaining race: a reader misses, reads a live row, and is still
//     holding it when the revoker commits and deletes the (absent) key. The
//     reader then writes its stale snapshot, and the session looks live until
//     the TTL.
//
// Step 3 is closed rather than documented away. Revocation writes a tombstone
// covering the session's remaining lifetime, and the populate is a Lua script
// that refuses to write when the tombstone exists. One extra key per
// revocation, one atomic script on cache misses only, and no window.
//
// The TTL remains as a backstop for the case where the DEL itself fails —
// bounded staleness instead of permanent.

// Cache holds sessions in Redis.
type Cache struct {
	client redis.UniversalClient

	// TTL bounds a cached entry. Short, because it is the fallback for a
	// failed invalidation rather than the primary expiry mechanism — the
	// session's own expires_at is that.
	TTL time.Duration

	// Observer receives invalidation failures. Optional; nil means the
	// metrics package is not wired.
	Observer CacheObserver
}

// CacheObserver reports what the cache could not do.
//
// An invalidation failure is the one event that breaks the guarantee above, so
// it is surfaced rather than logged and forgotten — the same reasoning as
// P1-02's fail-open counter. A mechanism whose failure nobody sees is a
// mechanism that is only working by luck.
type CacheObserver interface {
	InvalidationFailed()
	Lookup(source string, d time.Duration)
}

// DefaultCacheTTL bounds a cached session.
//
// One minute. Long enough that a burst of requests from one browser is served
// from cache; short enough that a failed invalidation is an incident measured
// in seconds rather than hours.
const DefaultCacheTTL = time.Minute

func NewCache(client redis.UniversalClient, observer CacheObserver) *Cache {
	return &Cache{client: client, TTL: DefaultCacheTTL, Observer: observer}
}

func sessionKey(hash string) string   { return "session:" + hash }
func tombstoneKey(hash string) string { return "session:revoked:" + hash }

// populate refuses to write when a tombstone exists.
//
// Atomic, because the check and the write must not be separated: between a
// plain EXISTS and a plain SET, a revocation could land and be overwritten,
// which is the exact race this exists to close.
var populate = redis.NewScript(`
	if redis.call("EXISTS", KEYS[2]) == 1 then
		return 0
	end
	redis.call("SET", KEYS[1], ARGV[1], "EX", ARGV[2])
	return 1
`)

// Get returns a cached session.
//
// A miss and a malformed entry are the same answer: the caller reads
// PostgreSQL. A cache that returns an error the caller has to interpret is a
// cache that makes the system less reliable than not having one.
func (c *Cache) Get(ctx context.Context, hash string) (Session, bool) {
	if c == nil || c.client == nil {
		return Session{}, false
	}

	start := time.Now()
	raw, err := c.client.Get(ctx, sessionKey(hash)).Bytes()
	if err != nil {
		return Session{}, false
	}

	var s Session
	if err := json.Unmarshal(raw, &s); err != nil {
		return Session{}, false
	}

	if c.Observer != nil {
		c.Observer.Lookup("cache", time.Since(start))
	}
	return s, true
}

// Put caches a session unless it has been revoked.
//
// Returns whether it was written. A refusal is not an error: it means a
// revocation happened while this read was in flight, and declining to cache is
// the correct outcome.
func (c *Cache) Put(ctx context.Context, hash string, s Session) (bool, error) {
	if c == nil || c.client == nil {
		return false, nil
	}

	encoded, err := json.Marshal(s)
	if err != nil {
		return false, fmt.Errorf("session: encoding for cache: %w", err)
	}

	ttl := c.ttl()
	// Never cache past the session's own expiry: an entry that outlives the
	// row it copies is an entry that answers for a session PostgreSQL would
	// refuse.
	if remaining := time.Until(s.ExpiresAt); remaining < ttl {
		if remaining <= 0 {
			return false, nil
		}
		ttl = remaining
	}

	written, err := populate.Run(ctx, c.client,
		[]string{sessionKey(hash), tombstoneKey(hash)},
		encoded, int(ttl.Seconds()),
	).Int()
	if err != nil {
		// A cache that cannot be written is a slower system, not a broken
		// one. The caller already has the session.
		return false, nil
	}
	return written == 1, nil
}

// Invalidate removes a session from the cache and blocks its repopulation.
//
// Called AFTER the PostgreSQL revocation commits. The tombstone lives for the
// session's remaining lifetime, because that is exactly how long a stale
// snapshot could otherwise be written back — after the session would have
// expired anyway, there is nothing left to protect.
func (c *Cache) Invalidate(ctx context.Context, hash string, expiresAt time.Time) error {
	if c == nil || c.client == nil {
		return nil
	}

	remaining := time.Until(expiresAt)
	if remaining <= 0 {
		// Already expired; the read path would refuse it regardless.
		remaining = time.Second
	}

	pipe := c.client.TxPipeline()
	pipe.Del(ctx, sessionKey(hash))
	pipe.Set(ctx, tombstoneKey(hash), "1", remaining)

	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		if c.Observer != nil {
			c.Observer.InvalidationFailed()
		}
		// Surfaced, not swallowed. PostgreSQL is already authoritative and has
		// the revocation, so correctness on a cache miss is intact — but until
		// the TTL expires, a cache hit may still serve this session, and that
		// window is exactly what an operator needs to be told about.
		return fmt.Errorf("session: invalidating cache: %w", err)
	}
	return nil
}

func (c *Cache) ttl() time.Duration {
	if c.TTL <= 0 {
		return DefaultCacheTTL
	}
	return c.TTL
}
