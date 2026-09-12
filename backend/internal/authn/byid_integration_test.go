//go:build integration

package authn

import (
	"context"
	"testing"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// `UserStore.ByID`, against a real database and as the runtime role (P3-03).
//
// It exists for one caller: the MFA challenge step, where a password was proven
// five minutes ago and the only remaining question is whether this account may
// STILL sign in. So the properties worth asserting are the ones that make it
// safe to complete a login on its answer — that it is confined by RLS like
// every other read, that it reports the account's state NOW rather than the
// state the challenge was issued under, and that it hands back no credential.

func (f userFixture) byID(t *testing.T, orgID, userID string) (User, error) {
	t.Helper()

	var user User
	err := f.db.WithTenant(context.Background(), orgID, func(tx *postgres.Tx) error {
		var err error
		user, err = f.store.ByID(context.Background(), tx, userID)
		return err
	})
	return user, err
}

func TestByIDReadsTheUserItIsAskedFor(t *testing.T) {
	f := setupUsers(t)

	user, err := f.byID(t, f.orgID, f.userID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}

	if user.ID != f.userID {
		t.Errorf("ID = %q, want %q", user.ID, f.userID)
	}
	if user.OrgID != f.orgID {
		t.Errorf("OrgID = %q, want %q", user.OrgID, f.orgID)
	}
	if user.Email != userEmail {
		t.Errorf("Email = %q, want %q", user.Email, userEmail)
	}
	if !user.CanSignIn() {
		t.Error("an active user cannot sign in")
	}
}

// The state NOW, not the state the challenge was issued under.
//
// This is the reason the challenge step reloads the user at all rather than
// carrying one through five minutes of Redis: somebody deactivated or locked
// while they were reaching for their phone must not complete the login.
func TestByIDReportsTheAccountStateNow(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status string
	}{
		{"deactivated", string(StatusDeactivated)},
		{"locked", string(StatusLocked)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupUsers(t)
			f.factory.Exec(`UPDATE users SET status = $2 WHERE id = $1`, f.userID, tc.status)

			user, err := f.byID(t, f.orgID, f.userID)
			if err != nil {
				t.Fatalf("ByID: %v", err)
			}
			if user.CanSignIn() {
				t.Errorf("a %s user may still sign in; a challenge would complete for them", tc.name)
			}
		})
	}
}

// RLS confines it like every other read. A user id from another tenant is not
// found, rather than found and returned across the boundary.
func TestByIDIsConfinedToItsTenant(t *testing.T) {
	f := setupUsers(t)

	other := f.factory.Organization(f.factory.Instance())
	stranger := f.factory.User(other, "bob@example.test")

	if _, err := f.byID(t, f.orgID, stranger); err == nil {
		t.Error("a user from another organization was returned; RLS did not confine the read")
	}

	// The positive control: the same id IS readable inside its own tenant, so
	// the test above is proving isolation rather than a broken query.
	if _, err := f.byID(t, other, stranger); err != nil {
		t.Errorf("the user is unreadable even in their own organization: %v", err)
	}
}

// An id nothing holds is an error, not a zero-valued user.
//
// A zero User has an empty Status, which CanSignIn would refuse — so a silent
// zero value would be safe by accident here. It is still wrong: the caller
// holds an id that came from server-side challenge state, so "no such user" is
// an internal inconsistency and must be reported as one rather than presented
// to somebody as a failed sign-in.
func TestByIDReportsAMissingUserAsAnError(t *testing.T) {
	f := setupUsers(t)

	_, err := f.byID(t, f.orgID, "00000000-0000-0000-0000-000000000000")
	if err == nil {
		t.Error("a user id nothing holds was not reported as an error")
	}
}

// It hands back no credential. There is no password hash on User, so this is a
// claim about the QUERY: nothing selects `password_hash`, and `Authenticate`
// remains the only path that reads one.
func TestByIDTakesNoPasswordAndReturnsNoHash(t *testing.T) {
	f := setupUsers(t)

	user, err := f.byID(t, f.orgID, f.userID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}

	// NeedsRehash is derived from a hash this path never reads, so it must not
	// be asserted by it — a caller acting on it would rehash against nothing.
	if user.NeedsRehash {
		t.Error("ByID reported NeedsRehash, but it reads no hash to judge that from")
	}
}
