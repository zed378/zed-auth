//go:build integration

// Revoking every session in an organization (P1-16).
//
// Used when an organization is deleted. Without it a deleted tenant's users keep
// working until their access tokens expire — up to ten minutes of a tenant the
// console says is gone, which is a tenant deleted only in the console.
package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

func TestRevokingAnOrganizationEndsEverySessionInIt(t *testing.T) {
	f := setup(t)
	now := time.Now()

	var ids []string
	for range 3 {
		s, _ := f.create(t, now)
		ids = append(ids, s.ID)
	}

	var revoked int64
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		revoked, err = f.mgr.RevokeOrganization(context.Background(), tx, f.orgID)
		return err
	}); err != nil {
		t.Fatalf("RevokeOrganization: %v", err)
	}

	if revoked != 3 {
		t.Errorf("reported %d revocations, want 3", revoked)
	}

	for _, id := range ids {
		var revokedAt *time.Time
		f.factory.QueryRow(&revokedAt, `SELECT revoked_at FROM sessions WHERE id = $1`, id)
		if revokedAt == nil {
			t.Errorf("session %s is still live", id)
		}
	}
}

// The count is returned rather than discarded because it goes into the audit
// event for the deletion. "The tenant was deleted and 0 sessions were revoked"
// and "...and 340 were" are different facts, and only the first is suspicious
// when the tenant was supposed to be in use.
func TestRevokingAnEmptyOrganizationReportsZero(t *testing.T) {
	f := setup(t)

	var revoked int64
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		revoked, err = f.mgr.RevokeOrganization(context.Background(), tx, f.orgID)
		return err
	}); err != nil {
		t.Fatalf("RevokeOrganization: %v", err)
	}

	if revoked != 0 {
		t.Errorf("reported %d revocations for an organization with no sessions", revoked)
	}
}

// **A revoked session must not still be honoured from the cache.**
//
// The database rows are revoked either way, so a cache MISS is safe — it falls
// through to a revoked row. A stale cache HIT is the exposure, and it is the
// whole reason invalidation is not left to the entry's own expiry.
func TestARevokedOrganizationsSessionsAreNotServedFromCache(t *testing.T) {
	f := setup(t)
	now := time.Now()

	_, token := f.create(t, now)

	// Warm the cache the way a request would.
	if _, err := f.mgr.Lookup(context.Background(), token.Reveal(), DefaultPolicy, now); err != nil {
		t.Fatalf("the warm-up lookup failed: %v", err)
	}

	if err := f.tx(t, func(tx *postgres.Tx) error {
		_, err := f.mgr.RevokeOrganization(context.Background(), tx, f.orgID)
		return err
	}); err != nil {
		t.Fatalf("RevokeOrganization: %v", err)
	}

	if _, err := f.mgr.Lookup(context.Background(), token.Reveal(), DefaultPolicy, now); err == nil {
		t.Fatal("a revoked session was still accepted — the cache is serving it")
	}
}

// Another organization's sessions are untouched.
//
// The query carries no org_id predicate: the transaction is scoped to one
// tenant and RLS supplies it. That is what this asserts — and it asserts it
// from the side that matters, by finding the other tenant's session still live.
func TestRevokingAnOrganizationDoesNotReachAnother(t *testing.T) {
	f := setup(t)
	now := time.Now()

	f.create(t, now)

	// A second tenant with a session of its own, inserted directly because the
	// fixture's manager is bound to the first.
	otherOrg := f.factory.Organization(f.factory.Instance())
	otherUser := f.factory.User(otherOrg)
	var otherSession string
	f.factory.QueryRow(&otherSession, `
		INSERT INTO sessions (org_id, user_id, token_hash, auth_methods, expires_at)
		VALUES ($1, $2, 'not-a-real-hash', ARRAY['pwd'], $3)
		RETURNING id`, otherOrg, otherUser, now.Add(time.Hour))

	if err := f.tx(t, func(tx *postgres.Tx) error {
		_, err := f.mgr.RevokeOrganization(context.Background(), tx, f.orgID)
		return err
	}); err != nil {
		t.Fatalf("RevokeOrganization: %v", err)
	}

	var revokedAt *time.Time
	f.factory.QueryRow(&revokedAt, `SELECT revoked_at FROM sessions WHERE id = $1`, otherSession)
	if revokedAt != nil {
		t.Error("another organization's session was revoked")
	}
}

// An already-revoked session is not counted twice, so the audit event's number
// is the number of people actually signed out.
func TestAnAlreadyRevokedSessionIsNotCountedAgain(t *testing.T) {
	f := setup(t)
	now := time.Now()

	f.create(t, now)
	f.create(t, now)

	if err := f.tx(t, func(tx *postgres.Tx) error {
		_, err := f.mgr.RevokeOrganization(context.Background(), tx, f.orgID)
		return err
	}); err != nil {
		t.Fatalf("the first revocation: %v", err)
	}

	var second int64
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		second, err = f.mgr.RevokeOrganization(context.Background(), tx, f.orgID)
		return err
	}); err != nil {
		t.Fatalf("the second revocation: %v", err)
	}

	if second != 0 {
		t.Errorf("the second revocation reported %d, want 0", second)
	}
}

// The revocation is audited, with the reason that distinguishes it from a
// logout: an incident timeline needs to know a tenant was deleted rather than
// that 340 people happened to sign out at once.
func TestRevokingAnOrganizationIsAudited(t *testing.T) {
	f := setup(t)
	now := time.Now()

	f.create(t, now)

	if err := f.tx(t, func(tx *postgres.Tx) error {
		_, err := f.mgr.RevokeOrganization(context.Background(), tx, f.orgID)
		return err
	}); err != nil {
		t.Fatalf("RevokeOrganization: %v", err)
	}

	var payload string
	f.factory.QueryRow(&payload,
		`SELECT payload::text FROM events
		  WHERE org_id = $1 AND event_type = 'session.revoked'
		  ORDER BY created_at DESC LIMIT 1`, f.orgID)

	if payload == "" {
		t.Fatal("no session.revoked event was written")
	}
	for _, want := range []string{"organization_deleted", "count"} {
		if !strings.Contains(payload, want) {
			t.Errorf("the event payload does not mention %q: %s", want, payload)
		}
	}
}
