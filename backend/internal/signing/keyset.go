package signing

import (
	"crypto"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

// Status is a key's place in the rotation lifecycle.
//
// Four states rather than two, because rotation needs an overlap in both
// directions: a key that is published before it signs (so consumers have it
// cached when the first token arrives), and a key that verifies after it stops
// signing (so tokens issued a moment before rotation still work).
type Status string

const (
	// StatusNext is published in JWKS but does not sign. It exists so that
	// consumers have fetched the key BEFORE the first token signed with it
	// arrives — otherwise the first request after a rotation races the
	// consumer's cache refresh.
	StatusNext Status = "next"

	// StatusCurrent signs. Exactly one per purpose, enforced by a partial
	// unique index in the database: two current keys would make "which key
	// signed this token" nondeterministic.
	StatusCurrent Status = "current"

	// StatusPrevious verifies but no longer signs. This is the overlap window
	// docs/PLAN/09 requires. Retiring straight from current would invalidate every
	// token issued in the seconds before the rotation.
	StatusPrevious Status = "previous"

	// StatusRetired is out of JWKS entirely. Tokens it signed no longer
	// verify, which is the point.
	StatusRetired Status = "retired"
)

// Verifying reports whether a key in this state should still validate tokens.
func (s Status) Verifying() bool {
	return s == StatusNext || s == StatusCurrent || s == StatusPrevious
}

var (
	// ErrNoCurrentKey means nothing can be signed. Treated as fatal at
	// startup rather than degraded: a service that accepts requests and fails
	// every login is a worse outage than one that refuses to start, and it is
	// much harder to diagnose.
	ErrNoCurrentKey = errors.New("no current signing key")

	// ErrUnknownKID means the token names a key this service does not have.
	ErrUnknownKID = errors.New("unknown key id")

	// ErrKeyRetired means the token was signed by a key that has since been
	// retired.
	ErrKeyRetired = errors.New("signing key is retired")
)

// Key is one signing key, as the service holds it in memory.
//
// The private key is present only for keys that can sign. A `previous` key is
// loaded with its public half alone — there is no reason for the process to
// hold material it is not allowed to use, and not holding it removes a way to
// use it by mistake.
type Key struct {
	KID       string
	Algorithm Algorithm
	Status    Status
	Public    crypto.PublicKey

	// private is unexported and never included in any string, JSON, or log
	// representation of this struct.
	private crypto.Signer
}

// CanSign reports whether this key is the one tokens are signed with.
func (k *Key) CanSign() bool { return k.Status == StatusCurrent && k.private != nil }

// KeySet is an immutable snapshot of every key the service knows about.
//
// Immutable on purpose: a set that could be mutated in place would need a lock
// on every verification, which is the hot path for every protected request in
// every consumer application (docs/PLAN/12). Rotation replaces the whole snapshot.
type KeySet struct {
	keys    map[string]*Key
	current *Key
}

// NewKeySet builds a snapshot and validates its invariants.
func NewKeySet(keys []*Key) (*KeySet, error) {
	set := &KeySet{keys: make(map[string]*Key, len(keys))}

	for _, key := range keys {
		if _, clash := set.keys[key.KID]; clash {
			// The database has a unique index on kid, so this means the rows
			// were assembled wrongly rather than stored wrongly.
			return nil, fmt.Errorf("duplicate kid %q in key set", key.KID)
		}
		set.keys[key.KID] = key

		if key.Status == StatusCurrent {
			if set.current != nil {
				return nil, fmt.Errorf("two current keys: %q and %q", set.current.KID, key.KID)
			}
			set.current = key
		}
	}

	return set, nil
}

// Current returns the key tokens are signed with.
func (s *KeySet) Current() (*Key, error) {
	if s.current == nil || !s.current.CanSign() {
		return nil, ErrNoCurrentKey
	}
	return s.current, nil
}

// ByKID looks up a key for verification.
//
// A retired key is reported distinctly from an unknown one. Both fail, and the
// difference matters to whoever reads the log: "retired" means the token is
// simply too old, "unknown" means something is wrong — a misconfigured
// consumer, a token from another deployment, or a forgery attempt.
func (s *KeySet) ByKID(kid string) (*Key, error) {
	key, ok := s.keys[kid]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownKID, kid)
	}
	if !key.Status.Verifying() {
		return nil, fmt.Errorf("%w: %q", ErrKeyRetired, kid)
	}
	return key, nil
}

// Verifying returns every key that should still validate a token, in a stable
// order (current first, then next, then previous).
func (s *KeySet) Verifying() []*Key {
	var current, next, previous []*Key

	for _, key := range s.keys {
		switch key.Status {
		case StatusCurrent:
			current = append(current, key)
		case StatusNext:
			next = append(next, key)
		case StatusPrevious:
			previous = append(previous, key)
		}
	}

	out := make([]*Key, 0, len(current)+len(next)+len(previous))
	out = append(out, current...)
	out = append(out, sortByKID(next)...)
	out = append(out, sortByKID(previous)...)
	return out
}

// JWKS renders the public key set for publication.
//
// Public halves only, and every non-retired key. A token signed just before a
// rotation must still verify afterwards, which requires its key to still be
// published (docs/PLAN/09's overlap period).
func (s *KeySet) JWKS() jose.JSONWebKeySet {
	verifying := s.Verifying()
	out := jose.JSONWebKeySet{Keys: make([]jose.JSONWebKey, 0, len(verifying))}

	for _, key := range verifying {
		out.Keys = append(out.Keys, jose.JSONWebKey{
			Key:       key.Public,
			KeyID:     key.KID,
			Algorithm: string(key.Algorithm),
			Use:       "sig",
		})
	}

	return out
}

// Len reports how many keys the set holds, including retired ones.
func (s *KeySet) Len() int { return len(s.keys) }

// --- caching ---------------------------------------------------------------

// Loader fetches the key set from storage.
type Loader func() (*KeySet, error)

// Cache holds the key set with a bounded TTL.
//
// Bounded in both directions, and both bounds are decisions:
//
//   - Verification must not touch the database. It runs on every protected
//     request across every consumer application (docs/PLAN/12), and a database
//     round trip there would make this service's availability a dependency of
//     every authorization check in the estate.
//   - A new key must reach every instance without a deploy. So the TTL is
//     minutes, not hours — long enough that signing is a memory lookup,
//     short enough that a rotation propagates while the operator is still
//     watching (P1-03 step 7).
type Cache struct {
	load Loader
	ttl  time.Duration
	now  func() time.Time

	mu        sync.RWMutex
	set       *KeySet
	refreshed time.Time
}

// DefaultCacheTTL is the refresh interval for the key set.
//
// Five minutes: a rotation is visible everywhere within one coffee, and a
// service issuing a thousand tokens a minute reads the database twelve times
// an hour rather than sixty thousand.
const DefaultCacheTTL = 5 * time.Minute

// NewCache returns a cache that refreshes through load.
func NewCache(load Loader, ttl time.Duration) *Cache {
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	return &Cache{load: load, ttl: ttl, now: time.Now}
}

// Get returns the current snapshot, refreshing if the TTL has passed.
//
// On a refresh failure the previous snapshot is returned rather than an error.
// That is deliberate: the keys have not changed just because the database is
// briefly unreachable, and failing every token verification during a database
// blip would turn a recoverable dependency failure into a total outage — the
// same reasoning that keeps the liveness probe off the database (docs/PLAN/14).
//
// The one case that does fail is having no snapshot at all, which only happens
// before the first successful load.
func (c *Cache) Get() (*KeySet, error) {
	c.mu.RLock()
	set, refreshed := c.set, c.refreshed
	c.mu.RUnlock()

	if set != nil && c.now().Sub(refreshed) < c.ttl {
		return set, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Re-check: another goroutine may have refreshed while this one waited.
	if c.set != nil && c.now().Sub(c.refreshed) < c.ttl {
		return c.set, nil
	}

	fresh, err := c.load()
	if err != nil {
		if c.set != nil {
			return c.set, nil
		}
		return nil, fmt.Errorf("loading signing keys: %w", err)
	}

	c.set = fresh
	c.refreshed = c.now()
	return c.set, nil
}

// Invalidate forces the next Get to reload.
//
// Called by the rotation command so an operator sees the effect immediately
// rather than waiting out the TTL on the instance they happen to be watching.
func (c *Cache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refreshed = time.Time{}
}

// --- helpers ---------------------------------------------------------------

func base64url(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func sortByKID(keys []*Key) []*Key {
	// Insertion sort: these slices hold at most a handful of keys, and a
	// stable deterministic order matters more than the algorithm. Deterministic
	// output means the JWKS response is byte-identical between calls, which
	// lets consumers and caches compare it cheaply.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j].KID < keys[j-1].KID; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
