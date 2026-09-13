//go:build integration

package session

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Sessions as a resource a person manages (P3-09), against real Postgres and
// Redis.

func (f fixture) createFor(t *testing.T, userID string, now time.Time) (Session, Token) {
	t.Helper()

	var (
		session Session
		token   Token
	)
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		session, token, err = f.mgr.Create(context.Background(), tx, New{
			UserID: userID, OrgID: f.orgID, AuthMethods: []string{"pwd"},
			IP: "203.0.113.7", UserAgent: "Mozilla/5.0 test",
		}, DefaultPolicy, now)
		return err
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return session, token
}

func (f fixture) revocationEvents(t *testing.T) int {
	t.Helper()
	var n int
	f.factory.QueryRow(&n, `SELECT count(*) FROM events WHERE event_type = 'session.revoked'`)
	return n
}

// --- the touch fix -----------------------------------------------------------------------

// A cache hit right after a database read does not write activity again.
//
// The bug: the database path cached the row as READ, with its old
// last_seen_at, and then recorded the activity. Every cache hit for the next
// minute still looked a minute stale, so every one wrote. The sentinel below is
// what makes that observable: if the hit writes, it overwrites the sentinel.
func TestACacheHitAfterATouchDoesNotWriteAgain(t *testing.T) {
	f := setup(t)
	t0 := time.Now()
	session, token := f.create(t, t0)

	// Past the throttle, from the database: this one records activity.
	read := t0.Add(5 * time.Minute)
	if _, err := f.mgr.Lookup(context.Background(), token.Reveal(), DefaultPolicy, read); err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	sentinel := t0.Add(-time.Hour).UTC().Truncate(time.Second)
	f.factory.Exec(`UPDATE sessions SET last_seen_at = $2 WHERE id = $1`, session.ID, sentinel)

	// Thirty seconds later, from the cache.
	if _, err := f.mgr.Lookup(context.Background(), token.Reveal(), DefaultPolicy, read.Add(30*time.Second)); err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	var at time.Time
	f.factory.QueryRow(&at, `SELECT last_seen_at FROM sessions WHERE id = $1`, session.ID)
	if !at.Equal(sentinel) {
		t.Errorf("a cache hit inside the throttle window wrote last_seen_at (%v); that is a write per request", at)
	}
}

// --- listing -------------------------------------------------------------------------------

func TestListLiveShowsOnlyUsableSessionsNewestFirst(t *testing.T) {
	f := setup(t)
	now := time.Now()

	oldest, _ := f.create(t, now.Add(-30*time.Minute))
	newest, _ := f.create(t, now.Add(-time.Minute))
	revoked, _ := f.create(t, now.Add(-10*time.Minute))
	idle, _ := f.create(t, now.Add(-20*time.Minute))
	colleague := f.factory.User(f.orgID, "colleague@example.test")
	f.createFor(t, colleague, now)

	f.factory.Exec(`UPDATE sessions SET revoked_at = $2 WHERE id = $1`, revoked.ID, now)
	f.factory.Exec(`UPDATE sessions SET last_seen_at = $2 WHERE id = $1`, idle.ID, now.Add(-3*time.Hour))

	var got []Session
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		got, err = f.mgr.ListLive(context.Background(), tx, f.userID, DefaultPolicy, now, ListPosition{}, 10)
		return err
	}); err != nil {
		t.Fatalf("ListLive: %v", err)
	}

	if len(got) != 2 || got[0].ID != newest.ID || got[1].ID != oldest.ID {
		ids := []string{}
		for _, s := range got {
			ids = append(ids, s.ID)
		}
		t.Fatalf("listed %v, want [newest, oldest] = [%s %s]", ids, newest.ID, oldest.ID)
	}

	// The next page, from the first row.
	var page []Session
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		page, err = f.mgr.ListLive(context.Background(), tx, f.userID, DefaultPolicy, now,
			ListPosition{CreatedAt: got[0].CreatedAt, ID: got[0].ID}, 10)
		return err
	}); err != nil {
		t.Fatalf("ListLive page 2: %v", err)
	}
	if len(page) != 1 || page[0].ID != oldest.ID {
		t.Errorf("the second page is %d rows, want just the oldest", len(page))
	}
}

// --- revoking one ----------------------------------------------------------------------------

func TestRevokeOwnedRefusesAnotherUsersSession(t *testing.T) {
	f := setup(t)
	now := time.Now()
	colleague := f.factory.User(f.orgID, "colleague@example.test")
	theirs, token := f.createFor(t, colleague, now)

	var rev Revocation
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		rev, err = f.mgr.RevokeOwned(context.Background(), tx, theirs.ID, f.userID, now)
		return err
	}); err != nil {
		t.Fatalf("RevokeOwned: %v", err)
	}

	if rev.Found {
		t.Error("a session belonging to another user was reported as found")
	}
	if _, err := f.mgr.Lookup(context.Background(), token.Reveal(), DefaultPolicy, now); err != nil {
		t.Errorf("another user's session was ended: %v", err)
	}
}

func TestRevokeOwnedEndsTheSessionThroughTheCache(t *testing.T) {
	f := setup(t)
	now := time.Now()
	session, token := f.create(t, now)

	// Warm the cache, so the refusal below has to come through invalidation.
	if _, err := f.mgr.Lookup(context.Background(), token.Reveal(), DefaultPolicy, now); err != nil {
		t.Fatalf("warming: %v", err)
	}

	var rev Revocation
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		rev, err = f.mgr.RevokeOwned(context.Background(), tx, session.ID, f.userID, now)
		return err
	}); err != nil {
		t.Fatalf("RevokeOwned: %v", err)
	}
	if !rev.Found || len(rev.SessionIDs) != 1 {
		t.Fatalf("revocation = %+v, want one session ended", rev)
	}
	if err := rev.Invalidate(context.Background()); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}

	if _, err := f.mgr.Lookup(context.Background(), token.Reveal(), DefaultPolicy, now); !errors.Is(err, ErrNotFound) {
		t.Errorf("the revoked session still resolves: %v", err)
	}
	if rev.UserID != f.userID {
		t.Errorf("Revocation.UserID = %q, want the owner", rev.UserID)
	}
	// The caller audits (see Revocation); the manager must not, or the API
	// would record every revocation twice.
	if f.revocationEvents(t) != 0 {
		t.Errorf("the manager wrote %d events; the Management API writes these", f.revocationEvents(t))
	}

	// Again: found, nothing ended, no second event.
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		rev, err = f.mgr.RevokeOwned(context.Background(), tx, session.ID, f.userID, now)
		return err
	}); err != nil {
		t.Fatalf("RevokeOwned again: %v", err)
	}
	if !rev.Found || len(rev.SessionIDs) != 0 {
		t.Errorf("a second revocation = %+v, want found with nothing ended", rev)
	}
}

// --- revoking the others -------------------------------------------------------------------

func TestRevokeOthersKeepsExactlyTheCurrentSession(t *testing.T) {
	f := setup(t)
	now := time.Now()

	current, currentToken := f.create(t, now)
	_, otherToken := f.create(t, now)
	_, thirdToken := f.create(t, now)
	colleague := f.factory.User(f.orgID, "colleague@example.test")
	_, colleagueToken := f.createFor(t, colleague, now)

	for _, tok := range []Token{currentToken, otherToken, thirdToken} {
		if _, err := f.mgr.Lookup(context.Background(), tok.Reveal(), DefaultPolicy, now); err != nil {
			t.Fatalf("warming: %v", err)
		}
	}

	var rev Revocation
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		rev, err = f.mgr.RevokeOthers(context.Background(), tx, f.userID, current.ID, now)
		return err
	}); err != nil {
		t.Fatalf("RevokeOthers: %v", err)
	}
	if err := rev.Invalidate(context.Background()); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}

	if len(rev.SessionIDs) != 2 {
		t.Errorf("ended %d sessions, want 2", len(rev.SessionIDs))
	}
	if _, err := f.mgr.Lookup(context.Background(), currentToken.Reveal(), DefaultPolicy, now); err != nil {
		t.Errorf("the current session was ended: %v", err)
	}
	for _, tok := range []Token{otherToken, thirdToken} {
		if _, err := f.mgr.Lookup(context.Background(), tok.Reveal(), DefaultPolicy, now); !errors.Is(err, ErrNotFound) {
			t.Errorf("another session survived: %v", err)
		}
	}
	if _, err := f.mgr.Lookup(context.Background(), colleagueToken.Reveal(), DefaultPolicy, now); err != nil {
		t.Errorf("a colleague's session was ended: %v", err)
	}
	if f.revocationEvents(t) != 0 {
		t.Errorf("the manager wrote %d events; the Management API writes these", f.revocationEvents(t))
	}
}

// An empty keep is refused rather than becoming "revoke all".
func TestRevokeOthersRefusesAnEmptyKeep(t *testing.T) {
	f := setup(t)
	now := time.Now()
	_, token := f.create(t, now)

	err := f.tx(t, func(tx *postgres.Tx) error {
		_, err := f.mgr.RevokeOthers(context.Background(), tx, f.userID, "", now)
		return err
	})
	// Refused by the guard, by name. Without it Postgres would also error —
	// '' is not a uuid — so asserting only "an error" could not tell a
	// deliberate refusal from a type accident that a schema change could remove.
	if err == nil || !strings.Contains(err.Error(), "needs the one to keep") {
		t.Fatalf("RevokeOthers with nothing to keep: err = %v, want the explicit refusal", err)
	}
	if _, err := f.mgr.Lookup(context.Background(), token.Reveal(), DefaultPolicy, now); err != nil {
		t.Errorf("an empty keep ended a session: %v", err)
	}
}

func TestRevokeOthersWithNothingElseIsANoOp(t *testing.T) {
	f := setup(t)
	now := time.Now()
	current, _ := f.create(t, now)

	var rev Revocation
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		rev, err = f.mgr.RevokeOthers(context.Background(), tx, f.userID, current.ID, now)
		return err
	}); err != nil {
		t.Fatalf("RevokeOthers: %v", err)
	}
	if len(rev.SessionIDs) != 0 {
		t.Errorf("revoking others with none ended %d sessions", len(rev.SessionIDs))
	}
	if err := rev.Invalidate(context.Background()); err != nil {
		t.Errorf("Invalidate: %v", err)
	}
}
