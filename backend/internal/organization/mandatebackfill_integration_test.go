//go:build integration

package organization

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/authn"
	"github.com/zed378/zed-auth/backend/migrations"
)

// Migration 034's backfill (P3-13), run from the same embedded file the
// migrator applies. The suite's schema has already run it once against no
// rows, so this seeds the states it exists for and runs it again — which is
// also the proof it is idempotent.
func TestTheBackfillStampsOnlyUnstampedMandates(t *testing.T) {
	e := setupEndpoints(t)
	instance := e.instance

	unstamped := e.factory.Organization(instance, "Phase 2 mandate")
	e.factory.Exec(`UPDATE organizations SET settings = '{"mfa_required": true}' WHERE id = $1`, unstamped)

	stamped := e.factory.Organization(instance, "Already stamped")
	e.factory.Exec(`UPDATE organizations SET settings = '{"mfa_required": true, "mfa_required_since": "2026-09-01T00:00:00Z"}' WHERE id = $1`, stamped)

	off := e.factory.Organization(instance, "No mandate")
	e.factory.Exec(`UPDATE organizations SET settings = '{"mfa_required": false}' WHERE id = $1`, off)

	sql, err := migrations.FS.ReadFile("20260914000034_stamp_unstamped_mfa_mandates.up.sql")
	if err != nil {
		t.Fatalf("reading the migration: %v", err)
	}
	before := time.Now().Add(-time.Minute)
	e.factory.Exec(string(sql))
	e.factory.Exec(string(sql)) // twice: a re-run must not move a deadline

	policyOf := func(id string) authn.LoginPolicy {
		t.Helper()
		var raw string
		e.factory.QueryRow(&raw, `SELECT settings::text FROM organizations WHERE id = $1`, id)
		policy, err := authn.ParseLoginPolicy(json.RawMessage(raw))
		if err != nil {
			t.Fatalf("the service cannot parse %s: %v — an unparseable stamp is no stamp", raw, err)
		}
		return policy
	}

	got := policyOf(unstamped)
	if got.MFARequiredSince.Before(before) {
		t.Errorf("unstamped mandate: since = %s, want the migration's clock", got.MFARequiredSince)
	}
	if outcome := authn.RequireMFA(got, false, got.MFARequiredSince.Add(authn.MFAGracePeriod)); outcome != authn.MFAEnrolmentRequired {
		t.Errorf("after the backfill the grace still never ends: RequireMFA = %v", outcome)
	}

	if since := policyOf(stamped).MFARequiredSince; !since.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("an existing stamp was moved to %s — that hands everybody a fresh grace", since)
	}
	if since := policyOf(off).MFARequiredSince; !since.IsZero() {
		t.Errorf("an organization with no mandate was stamped %s", since)
	}
}
