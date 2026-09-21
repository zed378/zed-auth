package authz

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache holds the INPUTS to a decision, never a decision (P2-07).
//
// `P2-07` step 3 says so, and the reason is Phase 4b: once policies can read
// `resource.attributes`, a decision depends on values that differ per request,
// and a cached decision would be served for a resource it was never computed
// against. Caching what a user holds and what a role carries stays correct
// whatever the decision function grows into.
//
// # Two keys, not one
//
// Grants and role definitions change independently and are invalidated by
// different events. One combined entry would mean a single role edit
// invalidating every user who holds it — a scan or a guess, neither of which
// belongs in a request path.
//
//	grants:{org}:{user}:{project}  → the role keys the user holds there
//	roles:{org}:{project}          → every role in the project, with what it carries
//
// The second is per-project rather than per-role, because a decision needs to
// look up several roles at once and N lookups to answer one question is how a
// cache makes things slower.
type Cache struct {
	client redis.UniversalClient

	// TTL bounds an entry. See DefaultTTL.
	TTL time.Duration

	// Observer receives hit, miss and staleness. Optional.
	Observer CacheObserver

	Log *slog.Logger

	Now func() time.Time
}

// CacheObserver records what the cache did.
type CacheObserver interface {
	// CacheLookup reports a hit or a miss, by kind.
	CacheLookup(kind string, hit bool)

	// CacheEntryAge reports how old an entry was when it was USED.
	//
	// This is the observed staleness distribution `P2-07` step 5 asks for, and
	// it is the honest version: the TTL is an upper bound anybody can read off
	// a constant, while this says what the fleet actually served.
	CacheEntryAge(kind string, age time.Duration)

	// CacheUnavailable reports that the cache could not be reached. Every one
	// of these is a database read that did not have to happen — and never a
	// decision that did not happen.
	CacheUnavailable()

	// InvalidationFailed reports an invalidation that did not land, which is
	// the only thing that makes the TTL load-bearing.
	InvalidationFailed(kind string)
}

// DefaultTTL bounds a cached entry.
//
// **Thirty seconds**, and the number matters less than what makes it
// defensible: invalidation is proactive, so the TTL is the backstop for the
// cases invalidation cannot cover, not the mechanism.
//
// It covers exactly two situations. Redis was unreachable at the moment a
// grant changed, so the delete never landed. And somebody changed a grant
// outside the API — a migration, a support script, direct SQL — where no code
// runs to invalidate anything.
//
// So the revocation window a consumer must be told about is: **immediate in
// the normal case, and at most thirty seconds if an invalidation was lost.**
// That is a sentence a security reviewer can act on, which "we cache
// aggressively" is not.
//
// Longer would buy very little: `P1-28` measured the database as able to serve
// this load, so the cache exists to protect latency rather than to make the
// system possible, and a cache that is 90% effective at 30s is not meaningfully
// worse than one that is 93% effective at five minutes — while the window it
// opens is ten times larger.
const DefaultTTL = 30 * time.Second

// generationTTL keeps a grant's revocation counter well beyond the lifetime of
// any entry that could reference it. A counter that expired first would reset
// to zero and make entries written before the revocation match again.
const generationTTL = 24 * time.Hour

// OperationTimeout bounds one cache operation.
//
// **The fall-through has to be fast, or it is not a fall-through.** Without
// this, an unreachable Redis makes every check wait for the client's own dial
// and retry budget — seconds — and the request times out instead of reading
// the database. A cache outage then looks exactly like a service outage, which
// is the failure `P2-07` step 6 exists to prevent.
//
// Found by pointing the cache at a dead port and watching the check answer 503
// TIMEOUT rather than the right answer a few milliseconds later.
//
// Fifty milliseconds is generous for a local Redis — `P1-28` measured the whole
// token endpoint at a p50 of 9.9ms — and small enough that paying it on every
// request during an outage is invisible beside the database read that follows.
const OperationTimeout = 50 * time.Millisecond

const (
	kindGrants = "grants"
	kindRoles  = "roles"
)

// bounded gives one cache operation its own deadline, without shortening the
// request's.
func bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, OperationTimeout)
}

func NewCache(client redis.UniversalClient, observer CacheObserver, log *slog.Logger) *Cache {
	return &Cache{client: client, TTL: DefaultTTL, Observer: observer, Log: log}
}

func (c *Cache) now() time.Time {
	if c != nil && c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// entry wraps a value with when it was written, so the age it was served at
// can be observed rather than assumed.
type entry[T any] struct {
	At    time.Time `json:"at"`
	Value T         `json:"v"`
}

func grantsKey(orgID, userID, projectID string) string {
	return fmt.Sprintf("authz:grants:%s:%s:%s", orgID, userID, projectID)
}

func rolesKey(orgID, projectID string) string {
	return fmt.Sprintf("authz:roles:%s:%s", orgID, projectID)
}

// generationKey counts a Project Grant's revocations (P4-04).
//
// A cached decision that came through a delegation records the grant and the
// generation it was written at; a revocation increments the generation, and
// every entry that depended on that grant stops matching at once.
//
// Why a generation rather than deleting the entries: a grant can be held by
// thousands of the partner's users, and this cache is keyed per user. Deleting
// them would mean either scanning the keyspace or keeping a second index of
// holders — the "scan or a guess" this file's own comments rule out of a
// request path (threat review T4-3). Revocation stays one INCR.
func generationKey(grantID string) string {
	return fmt.Sprintf("authz:grantgen:%s", grantID)
}

// grants is what a cached grant entry holds: the effective role keys, and the
// delegation they depended on, if any.
type grants struct {
	Keys []string `json:"k"`

	// Via is the Project Grant the keys came through, or empty for a direct
	// grant. A direct entry needs no generation check and pays no extra read.
	Via string `json:"v,omitempty"`

	// Gen is the generation Via was at when this entry was written.
	Gen int64 `json:"g,omitempty"`
}

// --- reads ------------------------------------------------------------------

// RoleKeys returns the role keys a user holds in a project, and whether the
// answer came from the cache.
//
// A cache failure is a MISS, never an error. `P2-07` step 6: a cache backend
// failure falls through to the database rather than failing the check — and
// certainly never to an allow.
func (c *Cache) RoleKeys(ctx context.Context, orgID, userID, projectID string) ([]string, string, bool) {
	var value grants
	if ok := c.get(ctx, kindGrants, grantsKey(orgID, userID, projectID), &value); !ok {
		return nil, "", false
	}
	if value.Via != "" && c.generation(ctx, value.Via) != value.Gen {
		// The delegation was revoked (or re-granted) since this was written.
		// A miss, not an error: the caller reads the database, which resolves
		// the row against the grant and returns nothing for a revoked one.
		c.observeLookup(kindGrants, false)
		return nil, "", false
	}
	return value.Keys, value.Via, true
}

// generation reads a grant's revocation counter. A missing key is generation 0,
// so a cold or emptied Redis does not invalidate every delegated entry — the
// TTL remains the backstop, exactly as it is for a direct entry.
func (c *Cache) generation(ctx context.Context, grantID string) int64 {
	if c == nil || c.client == nil {
		return 0
	}
	opCtx, cancel := bounded(ctx)
	defer cancel()

	n, err := c.client.Get(opCtx, generationKey(grantID)).Int64()
	switch {
	case err == redis.Nil:
		return 0
	case err != nil:
		c.observeUnavailable()
		// Unreadable: treat the entry as stale rather than trusting it. The
		// database answer is correct; a served stale one might not be.
		return -1
	}
	return n
}

// Roles returns the project's role definitions, and whether it was a hit.
func (c *Cache) Roles(ctx context.Context, orgID, projectID string) (map[string][]string, bool) {
	var roles map[string][]string
	if ok := c.get(ctx, kindRoles, rolesKey(orgID, projectID), &roles); !ok {
		return nil, false
	}
	return roles, true
}

func (c *Cache) get(ctx context.Context, kind, key string, into any) bool {
	if c == nil || c.client == nil {
		return false
	}

	opCtx, cancel := bounded(ctx)
	defer cancel()

	raw, err := c.client.Get(opCtx, key).Bytes()
	switch {
	case err == redis.Nil:
		c.observeLookup(kind, false)
		return false
	case err != nil:
		// Unreachable, not absent. Counted separately, because "the cache is
		// down" and "this user was not cached" have different remedies.
		c.observeUnavailable()
		if c.Log != nil {
			c.Log.Warn("the authorization cache could not be read; falling through to the database",
				"error", err.Error(), "kind", kind)
		}
		return false
	}

	// The wrapper is decoded first so a malformed entry is a miss rather than
	// a decision made from garbage.
	var wrapper entry[json.RawMessage]
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		c.observeLookup(kind, false)
		return false
	}
	if err := json.Unmarshal(wrapper.Value, into); err != nil {
		c.observeLookup(kind, false)
		return false
	}

	c.observeLookup(kind, true)
	c.observeAge(kind, c.now().Sub(wrapper.At))
	return true
}

// --- writes -----------------------------------------------------------------

func (c *Cache) PutRoleKeys(ctx context.Context, orgID, userID, projectID string, keys []string, via string) {
	value := grants{Keys: keys, Via: via}
	if via != "" {
		value.Gen = c.generation(ctx, via)
		if value.Gen < 0 {
			// The generation could not be read, so an entry written now could
			// not be checked later. Not cached at all rather than cached
			// unverifiably.
			return
		}
	}
	c.put(ctx, grantsKey(orgID, userID, projectID), value)
}

func (c *Cache) PutRoles(ctx context.Context, orgID, projectID string, roles map[string][]string) {
	c.put(ctx, rolesKey(orgID, projectID), roles)
}

// put stores a value. A failure is ignored on purpose: a cache that cannot be
// written is a cache that will miss, which is slower and never wrong.
func (c *Cache) put(ctx context.Context, key string, value any) {
	if c == nil || c.client == nil {
		return
	}

	encoded, err := json.Marshal(entry[any]{At: c.now(), Value: value})
	if err != nil {
		return
	}
	opCtx, cancel := bounded(ctx)
	defer cancel()

	if err := c.client.Set(opCtx, key, encoded, c.TTL).Err(); err != nil {
		c.observeUnavailable()
	}
}

// --- invalidation -----------------------------------------------------------

// InvalidateUser drops one user's grants in one project.
//
// Called when a grant is created, replaced or revoked (`P2-03`). Precise, so a
// grant change costs one delete rather than a scan.
func (c *Cache) InvalidateUser(ctx context.Context, orgID, userID, projectID string) {
	c.del(ctx, kindGrants, grantsKey(orgID, userID, projectID))
}

// InvalidateGrant retires every cached decision that came through one Project
// Grant, whoever holds it and however many of them there are (P4-04).
//
// One INCR. Called after a revocation commits — before it, a concurrent read
// could re-cache the pre-revocation answer against the new generation.
func (c *Cache) InvalidateGrant(ctx context.Context, grantID string) {
	if c == nil || c.client == nil || grantID == "" {
		return
	}
	opCtx, cancel := bounded(ctx)
	defer cancel()

	if err := c.client.Incr(opCtx, generationKey(grantID)).Err(); err != nil {
		c.observeUnavailable()
		if c.Log != nil {
			// Worth a line: until the TTL expires, a revoked delegation may
			// still be served from cache.
			c.Log.Warn("a project grant revocation could not be pushed to the authorization cache",
				"error", err.Error(), "grant_id", grantID, "ttl", c.TTL.String())
		}
		return
	}
	// The generation key must outlive every entry that references it, or a
	// restarted counter would make stale entries look current again.
	_ = c.client.Expire(opCtx, generationKey(grantID), generationTTL).Err()
}

// InvalidateProjectRoles drops a project's role definitions.
//
// Called when a role's permissions change (`P2-02`). One delete, whatever the
// number of users holding it — which is the reason roles are cached separately
// from grants.
func (c *Cache) InvalidateProjectRoles(ctx context.Context, orgID, projectID string) {
	c.del(ctx, kindRoles, rolesKey(orgID, projectID))
}

func (c *Cache) del(ctx context.Context, kind, key string) {
	if c == nil || c.client == nil {
		return
	}
	opCtx, cancel := bounded(ctx)
	defer cancel()

	if err := c.client.Del(opCtx, key).Err(); err != nil {
		// The one failure that makes the TTL load-bearing, so it is counted
		// and logged rather than swallowed: until the entry expires, a
		// revoked permission may still be served.
		c.observeInvalidationFailure(kind)
		if c.Log != nil {
			c.Log.Error("an authorization cache entry could not be invalidated; it will expire on its own",
				"error", err.Error(), "kind", kind, "ttl", c.TTL.String())
		}
	}
}

// --- observation ------------------------------------------------------------

func (c *Cache) observeLookup(kind string, hit bool) {
	if c != nil && c.Observer != nil {
		c.Observer.CacheLookup(kind, hit)
	}
}

func (c *Cache) observeAge(kind string, age time.Duration) {
	if c != nil && c.Observer != nil && age >= 0 {
		c.Observer.CacheEntryAge(kind, age)
	}
}

func (c *Cache) observeUnavailable() {
	if c != nil && c.Observer != nil {
		c.Observer.CacheUnavailable()
	}
}

func (c *Cache) observeInvalidationFailure(kind string) {
	if c != nil && c.Observer != nil {
		c.Observer.InvalidationFailed(kind)
	}
}
