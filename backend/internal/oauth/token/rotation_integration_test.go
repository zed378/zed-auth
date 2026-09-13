//go:build integration

// Rotation and reuse detection against real Postgres (P3-06).
//
// `docs/PLAN/17`'s Phase 3 criterion is one sentence — a rotated refresh token
// cannot be reused, verified by automated test — and it is the first test here.
//
// Two of these assert behaviour that was WRONG before this task rather than
// merely missing, and both produced perfectly working refreshes:
// `TestRefreshingStaysInOneFamily` and
// `TestRefreshingCannotExtendTheAbsoluteLifetime`.
package token

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// refreshWith presents a token at the endpoint.
func (f fixture) refreshWith(t *testing.T, refresh string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()

	rec := f.post(t, url.Values{
		"grant_type":    {GrantRefreshToken},
		"refresh_token": {refresh},
	}, true)

	if rec.Code != http.StatusOK {
		return rec, nil
	}
	return rec, decode(t, rec)
}

// refreshAs presents a token while authenticating as a DIFFERENT client.
func (f fixture) refreshAs(
	t *testing.T, clientID, secret, refresh string,
) *httptest.ResponseRecorder {
	t.Helper()

	form := url.Values{
		"grant_type":    {GrantRefreshToken},
		"refresh_token": {refresh},
	}
	r := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.SetBasicAuth(url.QueryEscape(clientID), url.QueryEscape(secret))

	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, r)
	return rec
}

// firstRefresh runs a code exchange and returns the refresh token it yielded.
func (f fixture) firstRefresh(t *testing.T) string {
	t.Helper()

	body := decode(t, f.exchange(t, f.issueCode(t, nil)))
	refresh, _ := body["refresh_token"].(string)
	if refresh == "" {
		t.Fatal("the code exchange returned no refresh token")
	}
	return refresh
}

// --- the criterion docs/PLAN/17 names -------------------------------------------------

// A rotated refresh token cannot be reused.
func TestARotatedRefreshTokenCannotBeReused(t *testing.T) {
	f := setup(t)
	original := f.firstRefresh(t)

	// The legitimate refresh. It yields a successor.
	_, body := f.refreshWith(t, original)
	successor, _ := body["refresh_token"].(string)
	if successor == "" {
		t.Fatal("the refresh returned no successor; rotation is not happening")
	}
	if successor == original {
		t.Fatal("the successor is the same token; nothing rotated")
	}

	// Use the successor, so the original's re-presentation is not a
	// within-grace retry. This is the theft case: two parties hold tokens from
	// one family and both are refreshing.
	if _, body := f.refreshWith(t, successor); body == nil {
		t.Fatal("the successor did not work")
	}

	// The original, again.
	rec, _ := f.refreshWith(t, original)
	if rec.Code == http.StatusOK {
		t.Fatal("a rotated refresh token was accepted a second time")
	}
}

// Reuse kills the entire family, not only the token presented.
func TestReuseRevokesTheWholeFamily(t *testing.T) {
	f := setup(t)
	original := f.firstRefresh(t)

	_, body := f.refreshWith(t, original)
	successor, _ := body["refresh_token"].(string)

	// Spend the successor so the original is unambiguously reuse.
	_, body = f.refreshWith(t, successor)
	newest, _ := body["refresh_token"].(string)
	if newest == "" {
		t.Fatal("the second refresh returned no token")
	}

	// The thief presents the original.
	if rec, _ := f.refreshWith(t, original); rec.Code == http.StatusOK {
		t.Fatal("the reused token was accepted")
	}

	// And now the LEGITIMATE client's newest token is dead too. That logs out
	// the victim along with the thief, deliberately: the alternative is
	// deciding which of two identical presentations is genuine.
	if rec, _ := f.refreshWith(t, newest); rec.Code == http.StatusOK {
		t.Error("the family was not revoked; the thief's copy still has a live sibling")
	}

	var live int
	f.factory.QueryRow(&live,
		`SELECT count(*) FROM refresh_tokens WHERE user_id = $1 AND NOT revoked`, f.userID)
	if live != 0 {
		t.Errorf("%d refresh tokens are still live after the family was revoked", live)
	}
}

// Reuse is audited, with the family named and no token in the payload.
func TestReuseIsAudited(t *testing.T) {
	f := setup(t)
	original := f.firstRefresh(t)

	_, body := f.refreshWith(t, original)
	successor, _ := body["refresh_token"].(string)
	_, _ = f.refreshWith(t, successor)
	_, _ = f.refreshWith(t, original)

	var count int
	f.factory.QueryRow(&count,
		`SELECT count(*) FROM events WHERE event_type = 'token.refresh.reuse_detected'`)
	if count != 1 {
		t.Fatalf("found %d reuse events, want 1 — an operator would not be paged", count)
	}

	var payload string
	f.factory.QueryRow(&payload,
		`SELECT payload::text FROM events WHERE event_type = 'token.refresh.reuse_detected'`)

	for _, secret := range []string{original, successor} {
		if holds(payload, secret) {
			t.Errorf("the audit payload contains a refresh token:\n%s", payload)
		}
	}
	if !holds(payload, "family_id") || !holds(payload, "tokens_revoked") {
		t.Errorf("the payload does not name the family or the kill count:\n%s", payload)
	}
}

// A second presentation of an already-dead token does not raise a second alert.
//
// An attacker retrying should not be able to generate one page per attempt.
func TestReuseAlertsOncePerFamily(t *testing.T) {
	f := setup(t)
	original := f.firstRefresh(t)

	_, body := f.refreshWith(t, original)
	successor, _ := body["refresh_token"].(string)
	_, _ = f.refreshWith(t, successor)

	for i := 0; i < 4; i++ {
		_, _ = f.refreshWith(t, original)
	}

	var count int
	f.factory.QueryRow(&count,
		`SELECT count(*) FROM events WHERE event_type = 'token.refresh.reuse_detected'`)
	if count != 1 {
		t.Errorf("four presentations of a dead token raised %d alerts, want 1", count)
	}
}

// --- the legitimate retry (card step 5) -------------------------------------------------

// A client that retries after a lost response is NOT logged out.
//
// This is the case that decides whether the whole feature survives contact
// with production: treat it as theft and real users are logged out until an
// operations team disables the protection.
func TestARetryAfterALostResponseIsNotReuse(t *testing.T) {
	f := setup(t)
	original := f.firstRefresh(t)

	// The first refresh succeeds server-side; imagine the response is lost.
	_, body := f.refreshWith(t, original)
	if body == nil {
		t.Fatal("the first refresh failed")
	}

	// The client retries with the only token it has. The successor is
	// untouched, because the client never received it.
	rec, retryBody := f.refreshWith(t, original)
	if rec.Code != http.StatusOK {
		t.Fatalf("a retry within the grace window was refused: %d %s", rec.Code, rec.Body.String())
	}
	if retryBody["refresh_token"] == nil {
		t.Error("the retry returned no token, so the client still has no way forward")
	}

	// And no alarm was raised — a retry is not a theft signal.
	var alerts int
	f.factory.QueryRow(&alerts,
		`SELECT count(*) FROM events WHERE event_type = 'token.refresh.reuse_detected'`)
	if alerts != 0 {
		t.Errorf("a legitimate retry raised %d reuse alerts", alerts)
	}
}

// But once the successor has been USED, the same presentation is theft.
//
// This is the pair to the test above, and the two together are what make the
// grace window a distinction rather than a hole.
func TestTheSamePresentationIsTheftOnceTheSuccessorIsUsed(t *testing.T) {
	f := setup(t)
	original := f.firstRefresh(t)

	_, body := f.refreshWith(t, original)
	successor, _ := body["refresh_token"].(string)

	// The client DID receive the successor and used it.
	if _, b := f.refreshWith(t, successor); b == nil {
		t.Fatal("the successor did not work")
	}

	// The same presentation that was a retry above is now reuse — and note
	// that it is well within the grace window, so the WINDOW is not what
	// distinguishes them.
	if rec, _ := f.refreshWith(t, original); rec.Code == http.StatusOK {
		t.Error("a rotated token was accepted after its successor had been used")
	}
}

// --- the two bugs this task found ----------------------------------------------------

// Refreshing stays inside one family.
//
// **This was wrong before P3-06.** `issue()` passed an empty family id on every
// path, so each refresh started a NEW family and `replaced_by` was never
// written. Reuse detection was not merely absent, it was impossible — and
// nothing looked wrong from outside, because every refresh worked.
func TestRefreshingStaysInOneFamily(t *testing.T) {
	f := setup(t)
	refresh := f.firstRefresh(t)

	for i := 0; i < 3; i++ {
		_, body := f.refreshWith(t, refresh)
		if body == nil {
			t.Fatalf("refresh %d failed", i)
		}
		refresh, _ = body["refresh_token"].(string)
	}

	var families int
	f.factory.QueryRow(&families,
		`SELECT count(DISTINCT family_id) FROM refresh_tokens WHERE user_id = $1`, f.userID)
	if families != 1 {
		t.Errorf("three refreshes produced %d families, want 1 — there is no lineage to detect reuse in",
			families)
	}

	var linked int
	f.factory.QueryRow(&linked,
		`SELECT count(*) FROM refresh_tokens WHERE user_id = $1 AND replaced_by IS NOT NULL`, f.userID)
	if linked != 3 {
		t.Errorf("%d tokens carry a replaced_by link, want 3", linked)
	}
}

// The absolute family lifetime cannot be extended by refreshing.
//
// **This was wrong before P3-06 too.** `Issue()` recomputed
// `family_expires_at` as `now + FamilyLifetime` on every issuance, so a client
// refreshing continuously held a session that never aged out — card step 6's
// abuse case, live since Phase 1.
func TestRefreshingCannotExtendTheAbsoluteLifetime(t *testing.T) {
	f := setup(t)
	refresh := f.firstRefresh(t)

	for i := 0; i < 3; i++ {
		_, body := f.refreshWith(t, refresh)
		if body == nil {
			t.Fatalf("refresh %d failed", i)
		}
		refresh, _ = body["refresh_token"].(string)
	}

	// EXACT equality across the family, not a tolerance.
	//
	// An earlier version compared the newest expiry against the first with a
	// one-second slack, and a mutation run showed it proved nothing:
	// `FamilyLifetime` is ninety days, so a recomputed `now + FamilyLifetime`
	// lands microseconds from the inherited one when the refreshes happen in
	// the same second. The bug this test exists for would have passed straight
	// through it.
	//
	// Inheritance means every row in the family carries the SAME value, which
	// is a claim no clock skew can satisfy by accident.
	var distinct int
	f.factory.QueryRow(&distinct,
		`SELECT count(DISTINCT family_expires_at) FROM refresh_tokens WHERE user_id = $1`, f.userID)
	if distinct != 1 {
		t.Errorf("the family has %d distinct absolute expiries, want 1 — "+
			"refreshing is extending the session", distinct)
	}

	var rows int
	f.factory.QueryRow(&rows,
		`SELECT count(*) FROM refresh_tokens WHERE user_id = $1`, f.userID)
	if rows != 4 {
		t.Fatalf("the family has %d tokens, want 4 — the check above is vacuous if rotation "+
			"is not producing rows", rows)
	}
}

// --- what does not change --------------------------------------------------------------

// A token this service never issued is refused, and is NOT reuse.
//
// Answering otherwise would let anybody destroy a family by guessing.
func TestAnUnknownTokenIsRefusedWithoutAnAlert(t *testing.T) {
	f := setup(t)
	f.firstRefresh(t)

	if rec, _ := f.refreshWith(t, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"); rec.Code == http.StatusOK {
		t.Fatal("an invented refresh token was accepted")
	}

	var alerts int
	f.factory.QueryRow(&alerts,
		`SELECT count(*) FROM events WHERE event_type = 'token.refresh.reuse_detected'`)
	if alerts != 0 {
		t.Errorf("an unknown token raised %d reuse alerts; a family could be killed by guessing", alerts)
	}
}

// Only hashes are stored — asserted here because rotation writes new rows and a
// regression would be invisible.
func TestRotationStoresOnlyHashes(t *testing.T) {
	f := setup(t)
	original := f.firstRefresh(t)

	_, body := f.refreshWith(t, original)
	successor, _ := body["refresh_token"].(string)

	var dump string
	f.factory.QueryRow(&dump, `SELECT string_agg(token_hash, '|') FROM refresh_tokens`)

	for _, plaintext := range []string{original, successor} {
		if holds(dump, plaintext) {
			t.Error("a refresh token is stored in the clear")
		}
	}
	if !holds(dump, HashRefresh(successor)) {
		t.Error("the successor's hash is not stored, so it could never be looked up")
	}
}

// A refresh token is bound to its client (F-7, abuse case A-3).
func TestARefreshTokenIsBoundToItsClient(t *testing.T) {
	f := setup(t)
	refresh := f.firstRefresh(t)

	other, otherSecret := f.secondClient(t)

	rec := f.refreshAs(t, other.ID, otherSecret.Reveal(), refresh)
	if rec.Code == http.StatusOK {
		t.Fatal("another client used this client's refresh token")
	}

	// The positive control: it still works for the client it belongs to, so the
	// refusal above is the binding rather than an already-dead token.
	if rec, _ := f.refreshWith(t, refresh); rec.Code != http.StatusOK {
		t.Errorf("the token does not work for its own client either: %d", rec.Code)
	}
}

// holds is strings.Contains with an empty needle treated as "no", so an
// assertion against a value the test failed to capture reads as a failure
// rather than as a pass.
func holds(haystack, needle string) bool {
	return needle != "" && strings.Contains(haystack, needle)
}
