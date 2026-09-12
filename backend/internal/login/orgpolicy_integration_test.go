//go:build integration

// Per-organization policy, enforced at login (P2-10).
//
// `docs/PLAN/17`'s Phase 2 criterion is precise about the distinction: settings
// must be **enforced at login time, not merely stored**. Until this task
// `session_lifetime_hours` and `allowed_login_methods` were stored, validated,
// returned by the API, and read by nothing.
//
// Every test here changes a setting and asserts the change shows up in
// behaviour. A test that only checked one value could not tell enforcement from
// a constant that happened to match.
package login

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/authn"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Two organizations, two configured lifetimes, two different expiries.
//
// The second organization is what makes this about PER-ORGANIZATION policy
// rather than about a setting being read at all: a service that read the
// setting once at startup would pass a single-organization test.
func TestSessionLifetimeIsEnforcedPerOrganization(t *testing.T) {
	s := setup(t)
	other := s.secondOrganization(t)

	s.setSettings(t, s.orgID, `{"session_lifetime_hours": 1}`)
	s.setSettings(t, other.orgID, `{"session_lifetime_hours": 720}`)

	shortLived := s.expiryAfterSignIn(t, s.appID, s.orgID, testEmail)
	longLived := s.expiryAfterSignIn(t, other.appID, other.orgID, other.email)

	if short := time.Until(shortLived); short > 3*time.Hour {
		t.Errorf("an organization configured for 1 hour got a session lasting %s", short)
	}
	if long := time.Until(longLived); long < 24*time.Hour {
		t.Errorf("an organization configured for 720 hours got a session lasting %s", long)
	}
	if !longLived.After(shortLived.Add(24 * time.Hour)) {
		t.Error("both organizations got effectively the same lifetime, so the setting is not enforced")
	}
}

// A lifetime outside the bounds is clamped rather than honoured or refused.
//
// Refusing would mean one bad settings value stops every login in the
// organization; honouring it would mean a session that outlives any reasonable
// policy. The clamp is reported at WARN, because a policy an administrator
// believes is in force and is not should be discoverable from a log rather than
// by experiment.
func TestAnOutOfRangeSessionLifetimeIsClamped(t *testing.T) {
	s := setup(t)
	s.setSettings(t, s.orgID, `{"session_lifetime_hours": 100000}`)

	expires := s.expiryAfterSignIn(t, s.appID, s.orgID, testEmail)
	if span := time.Until(expires); span > 31*24*time.Hour {
		t.Errorf("a 100000-hour lifetime produced a session lasting %s", span)
	}
}

// An organization that permits no login method refuses the only one that
// exists — `P2-10` step 3: refused **even if it is implemented**.
func TestALoginMethodExcludedByPolicyIsRefused(t *testing.T) {
	s := setup(t)
	other := s.secondOrganization(t)

	s.setSettings(t, s.orgID, `{"allowed_login_methods": []}`)

	id := s.begin(t)
	token := s.form(t, id)
	rec := s.submit(t, id, token, testEmail, testPassword)

	if !strings.Contains(rec.Body.String(), "not available for this organization") {
		t.Errorf("the refusal does not say what is wrong:\n%s", rec.Body.String())
	}

	// The half that matters: no session.
	var sessions int
	s.factory.QueryRow(&sessions, `SELECT count(*) FROM sessions WHERE org_id = $1`, s.orgID)
	if sessions != 0 {
		t.Errorf("%d session(s) exist for an organization that permits no login method", sessions)
	}

	// And the other organization still works, so this is not sign-in being
	// broken generally.
	s.expiryAfterSignIn(t, other.appID, other.orgID, other.email)
}

// The refusal does not consume the address's rate-limit budget.
//
// Nothing about a credential was attempted. Counting it would let an
// organization's own configuration lock out its users' addresses — a denial of
// service delivered by a settings change, against people who did nothing.
func TestARefusedMethodDoesNotConsumeTheRateLimit(t *testing.T) {
	s := setup(t)
	s.setSettings(t, s.orgID, `{"allowed_login_methods": []}`)

	for i := 0; i < 12; i++ {
		id := s.begin(t)
		token := s.form(t, id)
		s.submit(t, id, token, testEmail, testPassword)
	}

	var lockouts int
	s.factory.QueryRow(&lockouts, `SELECT count(*) FROM events WHERE event_type = 'user.lockout'`)
	if lockouts != 0 {
		t.Errorf("%d lockout(s) after 12 refusals that never touched a credential", lockouts)
	}
}

// Tightening a password policy does not lock out existing users.
//
// `P2-10` step 6 asks for this to be designed rather than left emergent. The
// design is that `password_policy` governs what a NEW password must satisfy,
// and the only thing that acts on an existing one is `max_age_days` — which
// `P1-02` already enforces at login, deliberately, and which this leaves alone.
func TestTighteningAPasswordPolicyDoesNotLockOutExistingUsers(t *testing.T) {
	s := setup(t)

	s.expiryAfterSignIn(t, s.appID, s.orgID, testEmail)

	// Demand far more than the existing password provides.
	s.setSettings(t, s.orgID,
		`{"password_policy": {"min_length": 64, "require_uppercase": true, "require_symbol": true}}`)

	s.expiryAfterSignIn(t, s.appID, s.orgID, testEmail)
}

// --- helpers ----------------------------------------------------------------

type secondOrg struct {
	orgID string
	appID string
	email string
}

// secondOrganization builds a complete second tenant on the same stack.
func (s *stack) secondOrganization(t *testing.T) secondOrg {
	t.Helper()

	orgID := s.factory.Organization(s.factory.Instance())
	email := "second@example.test"
	userID := s.factory.User(orgID, email)

	hash, err := authn.Hash(testPassword)
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}
	s.factory.Exec(`UPDATE users SET password_hash = $2, status = 'active' WHERE id = $1`, userID, hash)

	var projectID, appID string
	s.factory.QueryRow(&projectID,
		`INSERT INTO projects (org_id, name) VALUES ($1, 'second') RETURNING id`, orgID)
	s.factory.QueryRow(&appID,
		`INSERT INTO applications (project_id, org_id, name, type, redirect_uris)
		 VALUES ($1, $2, 'second', 'web', ARRAY['https://app.example.com/cb']) RETURNING id`,
		projectID, orgID)

	return secondOrg{orgID: orgID, appID: appID, email: email}
}

func (s *stack) setSettings(t *testing.T, orgID, settings string) {
	t.Helper()
	s.factory.Exec(`UPDATE organizations SET settings = $2::jsonb WHERE id = $1`, orgID, settings)
}

// expiryAfterSignIn completes a login through the given application and returns
// the resulting session's expiry.
func (s *stack) expiryAfterSignIn(t *testing.T, appID, orgID, email string) time.Time {
	t.Helper()

	id := s.beginFor(t, appID)
	token := s.form(t, id)
	rec := s.submit(t, id, token, email, testPassword)
	if rec.Code != 302 {
		t.Fatalf("signing in to %s answered %d:\n%s", orgID, rec.Code, rec.Body.String())
	}

	var expires time.Time
	if err := s.db.WithTenant(context.Background(), orgID, func(tx *postgres.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT expires_at FROM sessions WHERE org_id = $1 ORDER BY created_at DESC LIMIT 1`,
			orgID).Scan(&expires)
	}); err != nil {
		t.Fatalf("reading the session for %s: %v", orgID, err)
	}
	return expires
}
