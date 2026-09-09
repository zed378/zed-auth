//go:build integration

package session

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type fixture struct {
	db      *postgres.DB
	rdb     *redis.Client
	mgr     *Manager
	cache   *Cache
	factory *testsupport.Factory
	orgID   string
	userID  string
}

func setup(t *testing.T) fixture {
	t.Helper()

	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	orgID := factory.Organization(factory.Instance())
	userID := factory.User(orgID)

	// The runtime role, not the owner: the service reads sessions as auth_app,
	// so a grant it lacks must fail here rather than in production.
	db, err := postgres.Open(context.Background(), config.PostgresConfig{
		DSN: stack.AppDSN, MaxOpenConns: 4, MaxIdleConns: 2, ConnMaxLifetime: time.Minute,
	}, discard())
	if err != nil {
		t.Fatalf("opening the app connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	rdb := redis.NewClient(&redis.Options{Addr: stack.RedisAddr})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.FlushAll(context.Background()).Err(); err != nil {
		t.Fatalf("flushing redis: %v", err)
	}

	cache := NewCache(rdb, nil)
	return fixture{
		db:      db,
		rdb:     rdb,
		cache:   cache,
		mgr:     NewManager(db, cache, audit.NewWriter(db, discard(), nil), discard()),
		factory: factory,
		orgID:   orgID,
		userID:  userID,
	}
}

func (f fixture) tx(t *testing.T, fn func(*postgres.Tx) error) error {
	t.Helper()
	return f.db.WithTenant(context.Background(), f.orgID, fn)
}

func (f fixture) create(t *testing.T, now time.Time) (Session, Token) {
	t.Helper()

	var (
		session Session
		token   Token
	)
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		session, token, err = f.mgr.Create(context.Background(), tx, New{
			UserID:      f.userID,
			OrgID:       f.orgID,
			AuthMethods: []string{"pwd"},
			IP:          "203.0.113.7",
			UserAgent:   "Mozilla/5.0 test",
		}, DefaultPolicy, now)
		return err
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return session, token
}

// --- PG-14: the cookie is not the row ------------------------------------------

// The finding this task opened: if the cookie were the primary key, the
// sessions API would hand an administrator a live credential per row. The row
// must contain the hash and nothing that equals the cookie.
func TestOnlyTheTokenHashIsStored(t *testing.T) {
	f := setup(t)
	now := time.Now()

	session, token := f.create(t, now)
	plaintext := token.Reveal()

	// The whole row as text: a token leaking into any column is caught, not
	// just the one we remembered to check.
	var dump string
	f.factory.QueryRow(&dump, `SELECT sessions::text FROM sessions WHERE id = $1`, session.ID)

	if strings.Contains(dump, plaintext) {
		t.Error("the session token appears in the stored row")
	}
	if !strings.Contains(dump, HashToken(plaintext)) {
		t.Error("the stored row does not contain the token's hash")
	}
	if session.ID == plaintext {
		t.Error("the session id IS the cookie value; PG-14 is not implemented")
	}

	// And the audit payload, which is the other place a credential classically
	// ends up.
	var payloads string
	f.factory.QueryRow(&payloads,
		`SELECT COALESCE(string_agg(payload::text, ' '), '') FROM events WHERE org_id = $1`, f.orgID)
	if strings.Contains(payloads, plaintext) || strings.Contains(payloads, HashToken(plaintext)) {
		t.Error("a session token or its hash appears in an audit payload")
	}
	if !strings.Contains(payloads, session.ID) {
		t.Error("the audit payload does not name the session id, which is the safe identifier")
	}
}

// The control: the assertions above would pass against a Create that issued no
// usable token at all.
func TestTheIssuedTokenResolves(t *testing.T) {
	f := setup(t)
	now := time.Now()

	session, token := f.create(t, now)

	got, err := f.mgr.Lookup(context.Background(), token.Reveal(), DefaultPolicy, now)
	if err != nil {
		t.Fatalf("the issued token does not resolve: %v", err)
	}
	if got.ID != session.ID || got.UserID != f.userID || got.OrgID != f.orgID {
		t.Errorf("Lookup returned %+v, want the created session", got)
	}
	if len(got.AuthMethods) != 1 || got.AuthMethods[0] != "pwd" {
		t.Errorf("auth_methods = %v, want [pwd] — P1-07 builds amr from this", got.AuthMethods)
	}

	// An unrelated token does not resolve, so Lookup is not simply succeeding.
	other, _, _ := NewToken()
	if _, err := f.mgr.Lookup(context.Background(), other.Reveal(), DefaultPolicy, now); !errors.Is(err, ErrNotFound) {
		t.Errorf("an unrelated token resolved: %v", err)
	}
}

// --- revocation is immediate -----------------------------------------------------

// DoD item 5, and the reason cache.go is shaped the way it is. The session is
// deliberately warm in the cache at the moment of revocation, because a test
// that revokes a cold session proves nothing about a cache.
func TestRevocationTakesEffectOnTheNextRequest(t *testing.T) {
	f := setup(t)
	now := time.Now()

	session, token := f.create(t, now)
	hash := HashToken(token.Reveal())

	// Warm the cache, and confirm it is warm — otherwise the assertion below
	// would pass against a system with no cache at all.
	if _, err := f.mgr.Lookup(context.Background(), token.Reveal(), DefaultPolicy, now); err != nil {
		t.Fatalf("warming lookup: %v", err)
	}
	if _, ok := f.cache.Get(context.Background(), hash); !ok {
		t.Fatal("the session was not cached; this test would not exercise the invalidation")
	}

	var invalidate func(context.Context) error
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		invalidate, err = f.mgr.Revoke(context.Background(), tx, session.ID, ReasonLogout, f.userID, now)
		return err
	}); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := invalidate(context.Background()); err != nil {
		t.Fatalf("invalidating: %v", err)
	}

	// The very next call, with no TTL wait.
	if _, err := f.mgr.Lookup(context.Background(), token.Reveal(), DefaultPolicy, now); !errors.Is(err, ErrNotFound) {
		t.Errorf("a revoked session still resolves: %v", err)
	}
}

// The race cache.go exists to close, driven deterministically rather than by
// timing: read the row, revoke, and only then attempt the populate. Without
// the tombstone the stale snapshot would be written and served.
func TestARevocationDuringAnInFlightReadIsNotOverwritten(t *testing.T) {
	f := setup(t)
	now := time.Now()

	session, token := f.create(t, now)
	hash := HashToken(token.Reveal())

	// Step 1: a reader misses the cache and reads a live row.
	store := NewStore()
	read, err := store.LookupByTokenHash(context.Background(), f.db, hash, now)
	if err != nil {
		t.Fatalf("the reader could not load the session: %v", err)
	}

	// Step 2: a revocation commits and invalidates, while the reader still
	// holds its snapshot.
	var invalidate func(context.Context) error
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		invalidate, err = f.mgr.Revoke(context.Background(), tx, session.ID, ReasonAdmin, "", now)
		return err
	}); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := invalidate(context.Background()); err != nil {
		t.Fatalf("invalidating: %v", err)
	}

	// Step 3: the reader writes back. It must be refused.
	written, err := f.cache.Put(context.Background(), hash, read)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if written {
		t.Error("a stale snapshot was cached after the session was revoked; " +
			"the tombstone did not stop the repopulate race")
	}
	if _, ok := f.cache.Get(context.Background(), hash); ok {
		t.Error("the revoked session is in the cache")
	}
	if _, err := f.mgr.Lookup(context.Background(), token.Reveal(), DefaultPolicy, now); !errors.Is(err, ErrNotFound) {
		t.Errorf("the revoked session resolves: %v", err)
	}
}

// The control for the test above: with no tombstone, the same sequence DOES
// cache the stale snapshot. Without this, a Put that always refused would make
// the race test pass while the cache was simply broken.
func TestPutSucceedsWhenThereIsNoRevocation(t *testing.T) {
	f := setup(t)
	now := time.Now()

	_, token := f.create(t, now)
	hash := HashToken(token.Reveal())

	store := NewStore()
	read, err := store.LookupByTokenHash(context.Background(), f.db, hash, now)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}

	written, err := f.cache.Put(context.Background(), hash, read)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !written {
		t.Fatal("Put refused a session that was never revoked; the race test would " +
			"pass against a cache that never writes anything")
	}
}

// --- fixation ----------------------------------------------------------------------

// Abuse case A-1. A token held before login must not be honoured after it.
func TestLoginIssuesAFreshTokenAndTheOldOneDies(t *testing.T) {
	f := setup(t)
	now := time.Now()

	first, firstToken := f.create(t, now)

	// The user authenticates again. The previous session is revoked and a new
	// token issued — so a copy of the old cookie, however it was obtained, is
	// worthless.
	var invalidate func(context.Context) error
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		invalidate, err = f.mgr.Revoke(context.Background(), tx, first.ID, ReasonReauth, f.userID, now)
		return err
	}); err != nil {
		t.Fatalf("revoking the previous session: %v", err)
	}
	if err := invalidate(context.Background()); err != nil {
		t.Fatalf("invalidating: %v", err)
	}

	second, secondToken := f.create(t, now)

	if secondToken.Reveal() == firstToken.Reveal() {
		t.Fatal("the second login reused the first token; that is session fixation")
	}
	if second.ID == first.ID {
		t.Fatal("the second login reused the first session row")
	}
	if _, err := f.mgr.Lookup(context.Background(), firstToken.Reveal(), DefaultPolicy, now); !errors.Is(err, ErrNotFound) {
		t.Error("the pre-login token still resolves after logging in again")
	}
	if _, err := f.mgr.Lookup(context.Background(), secondToken.Reveal(), DefaultPolicy, now); err != nil {
		t.Errorf("the new token does not resolve: %v", err)
	}
}

// --- degradation --------------------------------------------------------------------

// NFR-3: Redis being unavailable costs latency, not correctness.
func TestLookupsSurviveRedisBeingUnavailable(t *testing.T) {
	f := setup(t)
	now := time.Now()

	_, token := f.create(t, now)

	// Point the cache at a closed port. Nothing about the durable record
	// changes.
	broken := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 100 * time.Millisecond})
	t.Cleanup(func() { _ = broken.Close() })

	mgr := NewManager(f.db, NewCache(broken, nil), audit.NewWriter(f.db, discard(), nil), discard())

	got, err := mgr.Lookup(context.Background(), token.Reveal(), DefaultPolicy, now)
	if err != nil {
		t.Fatalf("lookups do not survive Redis being down: %v", err)
	}
	if got.UserID != f.userID {
		t.Errorf("Lookup returned the wrong session: %+v", got)
	}
}

// --- expiry and the sweep ---------------------------------------------------------

func TestExpiredSessionsAreUnusableAndSwept(t *testing.T) {
	f := setup(t)
	now := time.Now()

	// A session that has already expired, written directly because Create
	// refuses to produce one.
	_, expiredToken := f.create(t, now)
	f.factory.Exec(`UPDATE sessions SET created_at = $1, expires_at = $2 WHERE token_hash = $3`,
		now.Add(-3*time.Hour), now.Add(-time.Hour), HashToken(expiredToken.Reveal()))

	live, _ := f.create(t, now)

	if _, err := f.mgr.Lookup(context.Background(), expiredToken.Reveal(), DefaultPolicy, now); !errors.Is(err, ErrNotFound) {
		t.Error("an expired session still resolves")
	}

	deleted, err := f.mgr.Sweep(context.Background(), 0, 100, now)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if deleted != 1 {
		t.Errorf("Sweep deleted %d rows, want 1 — a sweep that cannot count is a "+
			"maintenance job that cannot tell you whether it is keeping up", deleted)
	}

	var remaining int
	f.factory.QueryRow(&remaining, `SELECT count(*) FROM sessions`)
	if remaining != 1 {
		t.Errorf("%d sessions remain, want 1 (the live one)", remaining)
	}

	var liveStillThere bool
	f.factory.QueryRow(&liveStillThere, `SELECT EXISTS (SELECT 1 FROM sessions WHERE id = $1)`, live.ID)
	if !liveStillThere {
		t.Error("the sweep deleted a live session")
	}
}

// --- revoke everywhere ---------------------------------------------------------------

func TestRevokeAllForUserEndsEverySession(t *testing.T) {
	f := setup(t)
	now := time.Now()

	_, a := f.create(t, now)
	_, b := f.create(t, now)

	// Warm both, so the invalidation has something to do.
	for _, token := range []Token{a, b} {
		if _, err := f.mgr.Lookup(context.Background(), token.Reveal(), DefaultPolicy, now); err != nil {
			t.Fatalf("warming: %v", err)
		}
	}

	var invalidate func(context.Context) error
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		invalidate, err = f.mgr.RevokeAllForUser(context.Background(), tx, f.userID, f.orgID, f.userID, now)
		return err
	}); err != nil {
		t.Fatalf("RevokeAllForUser: %v", err)
	}
	if err := invalidate(context.Background()); err != nil {
		t.Fatalf("invalidating: %v", err)
	}

	for i, token := range []Token{a, b} {
		if _, err := f.mgr.Lookup(context.Background(), token.Reveal(), DefaultPolicy, now); !errors.Is(err, ErrNotFound) {
			t.Errorf("session %d still resolves after logout-everywhere: %v", i, err)
		}
	}
}

// --- audit ------------------------------------------------------------------------------

func TestSessionLifecycleIsAudited(t *testing.T) {
	f := setup(t)
	now := time.Now()

	session, _ := f.create(t, now)

	if err := f.tx(t, func(tx *postgres.Tx) error {
		_, err := f.mgr.Revoke(context.Background(), tx, session.ID, ReasonAdmin, "", now)
		return err
	}); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	var types string
	f.factory.QueryRow(&types,
		`SELECT string_agg(event_type, ',' ORDER BY id) FROM events WHERE org_id = $1`, f.orgID)

	for _, want := range []string{"session.created", "session.revoked"} {
		if !strings.Contains(types, want) {
			t.Errorf("no %s event; got: %s", want, types)
		}
	}

	// The reason must survive: "the user logged out" and "an administrator
	// revoked this" are different facts.
	var payload string
	f.factory.QueryRow(&payload,
		`SELECT payload::text FROM events WHERE event_type = 'session.revoked' AND org_id = $1`, f.orgID)
	if !strings.Contains(payload, string(ReasonAdmin)) {
		t.Errorf("the revocation payload does not record why: %s", payload)
	}
}

// --- the paths the happy path does not reach ------------------------------------

// Touch is throttled: NFR-5 forbids a write on every authenticated request,
// so the interval is the mechanism and it has to actually hold.
func TestTouchIsThrottledAndThenWrites(t *testing.T) {
	f := setup(t)
	now := time.Now()

	session, token := f.create(t, now)

	lastSeen := func() time.Time {
		var at time.Time
		f.factory.QueryRow(&at, `SELECT last_seen_at FROM sessions WHERE id = $1`, session.ID)
		return at
	}
	before := lastSeen()

	// Immediately after creation, inside the throttle window: no write.
	if _, err := f.mgr.Lookup(context.Background(), token.Reveal(), DefaultPolicy, now.Add(time.Second)); err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !lastSeen().Equal(before) {
		t.Error("last_seen_at was written inside the throttle window; that is a write per request")
	}

	// Past the interval: it writes.
	later := now.Add(touchInterval + time.Minute)
	if _, err := f.mgr.Lookup(context.Background(), token.Reveal(), DefaultPolicy, later); err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if after := lastSeen(); !after.After(before) {
		t.Errorf("last_seen_at was not updated past the throttle interval: %v -> %v", before, after)
	}
}

// An idle session is refused even though its absolute lifetime has not passed,
// and even when the cache is warm — the cached copy carries last_seen_at, so
// the check must apply to it too.
func TestIdleExpiryAppliesToACachedSession(t *testing.T) {
	f := setup(t)
	now := time.Now()

	_, token := f.create(t, now)

	policy := Policy{AbsoluteLifetime: 12 * time.Hour, IdleTimeout: time.Hour}

	if _, err := f.mgr.Lookup(context.Background(), token.Reveal(), policy, now); err != nil {
		t.Fatalf("warming: %v", err)
	}

	idle := now.Add(2 * time.Hour)
	if _, err := f.mgr.Lookup(context.Background(), token.Reveal(), policy, idle); !errors.Is(err, ErrNotFound) {
		t.Errorf("an idle session resolved from cache: %v", err)
	}
}

// Revoking twice is not an error: a logout pressed twice is not a failure, and
// there is nothing left to invalidate.
func TestRevokingTwiceIsNotAnError(t *testing.T) {
	f := setup(t)
	now := time.Now()

	session, _ := f.create(t, now)

	for i := range 2 {
		if err := f.tx(t, func(tx *postgres.Tx) error {
			invalidate, err := f.mgr.Revoke(context.Background(), tx, session.ID, ReasonLogout, "", now)
			if err != nil {
				return err
			}
			return invalidate(context.Background())
		}); err != nil {
			t.Fatalf("revocation %d failed: %v", i+1, err)
		}
	}

	// And exactly one revocation event, not two: the second found nothing to do.
	var events int
	f.factory.QueryRow(&events,
		`SELECT count(*) FROM events WHERE event_type = 'session.revoked' AND org_id = $1`, f.orgID)
	if events != 1 {
		t.Errorf("%d revocation events, want 1 — the second revoke should be a no-op", events)
	}
}

// Revoking a user with no live sessions does nothing and says so.
func TestRevokeAllWithNoSessionsIsANoOp(t *testing.T) {
	f := setup(t)
	now := time.Now()

	if err := f.tx(t, func(tx *postgres.Tx) error {
		invalidate, err := f.mgr.RevokeAllForUser(context.Background(), tx, f.userID, f.orgID, "", now)
		if err != nil {
			return err
		}
		return invalidate(context.Background())
	}); err != nil {
		t.Fatalf("RevokeAllForUser on a user with no sessions: %v", err)
	}

	var events int
	f.factory.QueryRow(&events, `SELECT count(*) FROM events WHERE org_id = $1`, f.orgID)
	if events != 0 {
		t.Errorf("%d events written for a no-op revoke-all", events)
	}
}

// A session with no recorded factor would make P1-07's `amr` claim a lie, so
// it cannot be created at all.
func TestCreateRequiresAuthMethods(t *testing.T) {
	f := setup(t)

	err := f.tx(t, func(tx *postgres.Tx) error {
		_, _, err := f.mgr.Create(context.Background(), tx, New{
			UserID: f.userID, OrgID: f.orgID, AuthMethods: nil,
		}, DefaultPolicy, time.Now())
		return err
	})
	if err == nil {
		t.Error("a session was created with no auth_methods; amr would be unfounded")
	}
}

// An unparseable IP is stored as NULL rather than failing the login: a login
// that fails because a proxy sent a malformed header is a worse outcome than
// an incomplete sessions screen.
func TestAnUnparseableIPDoesNotFailTheLogin(t *testing.T) {
	f := setup(t)
	now := time.Now()

	var session Session
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		session, _, err = f.mgr.Create(context.Background(), tx, New{
			UserID: f.userID, OrgID: f.orgID, AuthMethods: []string{"pwd"},
			IP: "not-an-ip", UserAgent: strings.Repeat("x", maxUserAgent*2),
		}, DefaultPolicy, now)
		return err
	}); err != nil {
		t.Fatalf("Create with a malformed IP: %v", err)
	}

	if session.IP != "" {
		t.Errorf("IP = %q, want empty for an unparseable value", session.IP)
	}
	// And the oversized user agent is bounded rather than stored whole.
	if len(session.UserAgent) > maxUserAgent {
		t.Errorf("user agent stored at %d bytes, want at most %d", len(session.UserAgent), maxUserAgent)
	}
}

// The cache never outlives the session it copies: an entry that answered for a
// session PostgreSQL would refuse is worse than a cache miss.
func TestPutRefusesAnAlreadyExpiredSession(t *testing.T) {
	f := setup(t)
	now := time.Now()

	written, err := f.cache.Put(context.Background(), "deadbeef", Session{
		ID: "x", ExpiresAt: now.Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if written {
		t.Error("an already-expired session was cached")
	}
}

// A malformed cookie must not become a database query.
func TestMalformedCookiesNeverReachTheDatabase(t *testing.T) {
	f := setup(t)

	for _, bad := range []string{"", "short", strings.Repeat("!", 43)} {
		if _, err := f.mgr.Lookup(context.Background(), bad, DefaultPolicy, time.Now()); !errors.Is(err, ErrNotFound) {
			t.Errorf("Lookup(%q) = %v, want ErrNotFound", bad, err)
		}
	}
}
