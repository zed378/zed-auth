//go:build integration

// What ends a refresh token, presented at the endpoint rather than read out of
// the table (P3-14).
//
// The earlier tests for each of these asserted a database row: `revoked` set,
// `family_expires_at` inherited. A row is the precondition. The property a
// consumer depends on is that `/oauth/token` refuses, and a handler that forgot
// to read the row would leave every one of those assertions green.
package token

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/authn"
)

func (f fixture) expectRefused(t *testing.T, refresh, why string) {
	t.Helper()
	rec, _ := f.refreshWith(t, refresh)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("refresh answered %d, want 400 — %s:\n%s", rec.Code, why, rec.Body.String())
	}
	if body := decode(t, rec); body["error"] != ErrInvalidGrant {
		t.Errorf("error = %v, want invalid_grant — %s", body["error"], why)
	}
}

func (f fixture) expectRefreshed(t *testing.T, refresh, why string) string {
	t.Helper()
	rec, body := f.refreshWith(t, refresh)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh answered %d, want 200 — %s:\n%s", rec.Code, why, rec.Body.String())
	}
	next, _ := body["refresh_token"].(string)
	return next
}

// --- session revocation -----------------------------------------------------------------

// Revoking the browser session a refresh token came from ends the token at the
// endpoint (docs/PLAN/17 Phase 3: "a revoked session is immediately unusable").
func TestARefreshTokenFromARevokedSessionIsRefused(t *testing.T) {
	f := setup(t)
	refresh := f.firstRefresh(t)
	refresh = f.expectRefreshed(t, refresh, "positive control: the session is live")

	f.factory.Exec(`UPDATE sessions SET revoked_at = now() WHERE id = $1`, f.sessionID)
	// The product path invalidates the session cache in the same operation as
	// the write (ADR-003); a direct UPDATE has to do the same by hand.
	if err := f.rdb.FlushAll(context.Background()).Err(); err != nil {
		t.Fatalf("flushing the session cache: %v", err)
	}

	f.expectRefused(t, refresh, "the session it came from is revoked")
}

// --- the two expiries --------------------------------------------------------------------

// A token past its own expiry is refused, however young its family.
func TestARefreshTokenPastItsOwnExpiryIsRefused(t *testing.T) {
	f := setup(t)
	refresh := f.firstRefresh(t)

	f.factory.Exec(`UPDATE refresh_tokens SET expires_at = now() - interval '1 second'
	                 WHERE token_hash = $1`, HashRefresh(refresh))

	f.expectRefused(t, refresh, "the token's own 14 days are over")
}

// A token whose FAMILY has aged out is refused, even though the token itself
// was issued moments ago. This is the endpoint half of "refreshing cannot
// extend a session forever": the inherited expiry is the precondition, and the
// refusal is the control.
func TestARefreshTokenInAnExpiredFamilyIsRefused(t *testing.T) {
	f := setup(t)
	refresh := f.firstRefresh(t)
	refresh = f.expectRefreshed(t, refresh, "positive control: the family is young")

	// Both columns, because `refresh_tokens_family_outlives_token` requires the
	// family to outlive each token.
	f.factory.Exec(`UPDATE refresh_tokens
	                   SET expires_at = now() - interval '1 second',
	                       family_expires_at = now() - interval '1 second'
	                 WHERE user_id = $1`, f.userID)

	f.expectRefused(t, refresh, "the family's 90 days are over")
}

// --- the MFA mandate (P3-07's A-2) ------------------------------------------------------

func (f fixture) withMandate(types ...string) fixture {
	f.handler.Mandate = &authn.MandateCheck{
		DB:       f.db,
		Policies: authn.NewPolicyStore(slog.New(slog.NewTextHandler(io.Discard, nil))),
		Types:    types,
	}
	return f
}

// A token issued before the mandate's grace ended does not outlive it. Before
// P3-14 nothing on the refresh path read the mandate, so a user with no factor
// kept refreshing for the family's 90 days.
func TestARefreshIsRefusedOnceTheMandateRequiresEnrolment(t *testing.T) {
	f := setup(t).withMandate("totp")
	refresh := f.firstRefresh(t)
	refresh = f.expectRefreshed(t, refresh, "positive control: no mandate yet")

	// Switched on long ago: the grace is over.
	f.factory.Exec(`UPDATE organizations SET settings = '{"mfa_required": true, "mfa_required_since": "2020-01-01T00:00:00Z"}' WHERE id = $1`, f.orgID)
	f.expectRefused(t, refresh, "the mandate's grace is over and the user has no factor")

	// Enrolling lifts it — the refusal is about the mandate, not the token.
	f.factory.Exec(`INSERT INTO user_mfa_factors (user_id, org_id, type, status, secret_encrypted)
	                VALUES ($1, $2, 'totp', 'active', '\x01')`, f.userID, f.orgID)
	f.expectRefreshed(t, refresh, "the user now holds a factor")
}

// Inside the grace, refreshing continues: the grace exists so the switch is
// not a lockout.
func TestARefreshContinuesInsideTheGrace(t *testing.T) {
	f := setup(t).withMandate("totp")
	refresh := f.firstRefresh(t)

	since := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	f.factory.Exec(`UPDATE organizations SET settings = jsonb_build_object('mfa_required', true, 'mfa_required_since', $2::text) WHERE id = $1`, f.orgID, since)

	f.expectRefreshed(t, refresh, "the mandate was switched on an hour ago; the grace is 14 days")
}

// A factor this build cannot challenge does not satisfy the mandate, for the
// reason the login path gives: otherwise a user could hold a factor nobody can
// ask for and pass a policy they cannot meet.
func TestAFactorThisBuildCannotServeDoesNotSatisfyTheMandate(t *testing.T) {
	f := setup(t).withMandate("totp")
	refresh := f.firstRefresh(t)

	f.factory.Exec(`UPDATE organizations SET settings = '{"mfa_required": true, "mfa_required_since": "2020-01-01T00:00:00Z"}' WHERE id = $1`, f.orgID)
	f.factory.Exec(`INSERT INTO user_mfa_factors (user_id, org_id, type, status, secret_encrypted, credential_id, public_key)
	                VALUES ($1, $2, 'webauthn', 'active', NULL, 'Y3JlZGVudGlhbA', 'cHVibGljLWtleQ')`, f.userID, f.orgID)

	f.expectRefused(t, refresh, "a passkey on a build that cannot challenge passkeys is not a factor")
}

// A deployment that cannot enrol anybody never refuses on the mandate — the
// same non-lockout answer the login page gives.
func TestAMandateThisBuildCannotEnforceRefusesNothing(t *testing.T) {
	f := setup(t).withMandate()
	refresh := f.firstRefresh(t)

	f.factory.Exec(`UPDATE organizations SET settings = '{"mfa_required": true, "mfa_required_since": "2020-01-01T00:00:00Z"}' WHERE id = $1`, f.orgID)
	f.expectRefreshed(t, refresh, "no factor type is available, so refusing would lock everybody out")
}

// --- concurrency ------------------------------------------------------------------------

// Simultaneous presentations of one refresh token (P3-14; docs/SECURITY/05's
// business-logic races). Every earlier rotation test presents tokens one after
// another, and the property that matters is only visible when they overlap:
// however the race resolves, the family must end with EXACTLY ONE live token,
// every answer must be a success or an ordinary refusal — never a 5xx that a
// client would retry — and exactly one of the tokens handed out must work.
func TestConcurrentRefreshesLeaveExactlyOneLiveToken(t *testing.T) {
	f := setup(t)
	refresh := f.firstRefresh(t)

	const racers = 8
	type answer struct {
		code  int
		token string
	}
	answers := make(chan answer, racers)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		go func() {
			<-start
			rec, body := f.refreshWith(t, refresh)
			next, _ := body["refresh_token"].(string)
			answers <- answer{code: rec.Code, token: next}
		}()
	}
	close(start)

	var issued []string
	for i := 0; i < racers; i++ {
		a := <-answers
		switch a.code {
		case http.StatusOK:
			issued = append(issued, a.token)
		case http.StatusBadRequest:
		default:
			t.Errorf("a concurrent refresh answered %d — a client retries a server error, and that retry is a reuse", a.code)
		}
	}
	if len(issued) == 0 {
		t.Fatal("no concurrent refresh succeeded; the race resolved to nobody")
	}

	var live int
	f.factory.QueryRow(&live,
		`SELECT count(*) FROM refresh_tokens WHERE user_id = $1 AND NOT revoked AND replaced_by IS NULL AND expires_at > now()`,
		f.userID)
	if live != 1 {
		t.Errorf("the family has %d live tokens after the race, want exactly 1", live)
	}

	works := 0
	for _, token := range issued {
		if rec, _ := f.refreshWith(t, token); rec.Code == http.StatusOK {
			works++
			break // using it rotates it; the others are judged against that state
		}
	}
	if works != 1 {
		t.Errorf("none of the %d tokens handed out during the race still works", len(issued))
	}
}
