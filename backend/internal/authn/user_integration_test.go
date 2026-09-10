//go:build integration

package authn

import (
	"context"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

// The credential lookup, against a real database and as the runtime role.
//
// P1-12 exercises this through the login page; what is here is what only this
// level can say — that RLS confines the lookup, that a NULL password_hash is
// not an error, and that a rehash does not disturb the expiry clock.

const (
	userPassword = "correct horse battery staple"
	userEmail    = "alice@example.test"
)

type userFixture struct {
	db      *postgres.DB
	factory *testsupport.Factory
	store   *UserStore
	orgID   string
	userID  string
}

func setupUsers(t *testing.T) userFixture {
	t.Helper()

	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	orgID := factory.Organization(factory.Instance())
	userID := factory.User(orgID, userEmail)

	hash, err := Hash(userPassword)
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}
	factory.Exec(`UPDATE users SET password_hash = $2 WHERE id = $1`, userID, hash)

	return userFixture{
		db: openApp(t, stack), factory: factory, store: NewUserStore(),
		orgID: orgID, userID: userID,
	}
}

func (f userFixture) authenticate(t *testing.T, orgID, email, password string) (User, bool) {
	t.Helper()

	var (
		user     User
		verified bool
	)
	err := f.db.WithTenant(context.Background(), orgID, func(tx *postgres.Tx) error {
		var err error
		user, verified, err = f.store.Authenticate(context.Background(), tx, email, password)
		return err
	})
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	return user, verified
}

func TestACorrectPasswordVerifies(t *testing.T) {
	f := setupUsers(t)

	user, verified := f.authenticate(t, f.orgID, userEmail, userPassword)

	if !verified {
		t.Fatal("the correct password did not verify")
	}
	if user.ID != f.userID || user.OrgID != f.orgID {
		t.Errorf("the wrong user came back: %s in %s", user.ID, user.OrgID)
	}
	if user.Status != StatusActive {
		t.Errorf("status = %q", user.Status)
	}
}

// The address is compared case-insensitively, matching the `lower(email)`
// unique index. Somebody who capitalises the first letter of their own address
// is not a different person.
func TestTheAddressIsComparedCaseInsensitively(t *testing.T) {
	f := setupUsers(t)

	if _, verified := f.authenticate(t, f.orgID, "  ALICE@Example.TEST ", userPassword); !verified {
		t.Error("a capitalised address did not match")
	}
}

func TestAWrongPasswordDoesNotVerify(t *testing.T) {
	f := setupUsers(t)

	user, verified := f.authenticate(t, f.orgID, userEmail, "not the password")

	if verified {
		t.Fatal("a wrong password verified")
	}
	// The user still comes back, so the caller can audit a reason class — the
	// audit log may distinguish what the response body must not.
	if user.ID != f.userID {
		t.Error("the user was not returned, so a failed login cannot be attributed")
	}
}

// Unknown is not an error. It is the same shape of answer as a wrong password,
// which is what lets the caller give one response to both without having to
// remember to.
func TestAnUnknownAddressIsNotAnError(t *testing.T) {
	f := setupUsers(t)

	user, verified := f.authenticate(t, f.orgID, "nobody@example.test", userPassword)

	if verified {
		t.Fatal("an unknown address verified")
	}
	if user.ID != "" {
		t.Errorf("a user came back for an unknown address: %s", user.ID)
	}
}

// A federated user, or one invited and not yet set up. docs/PLAN/04 makes
// password_hash nullable precisely so this row is honest rather than carrying
// a fake hash — and it must be refused the same way a wrong password is, at
// the same cost.
func TestAUserWithNoPasswordIsRefused(t *testing.T) {
	f := setupUsers(t)
	f.factory.Exec(`UPDATE users SET password_hash = NULL WHERE id = $1`, f.userID)

	user, verified := f.authenticate(t, f.orgID, userEmail, userPassword)

	if verified {
		t.Fatal("a user with no password verified")
	}
	if user.ID != f.userID {
		t.Error("the user was not returned")
	}
}

// A blank or absurd address is answered without a query. It must produce the
// same not-found the caller already handles, including its Argon2 computation
// — a shortcut that skipped the work would be a timing difference of its own.
func TestAnAbsurdAddressIsRefusedWithoutAQuery(t *testing.T) {
	f := setupUsers(t)

	for _, address := range []string{"", "   ", string(make([]byte, 0, 400)) + longAddress()} {
		user, verified := f.authenticate(t, f.orgID, address, userPassword)
		if verified || user.ID != "" {
			t.Errorf("address of length %d was accepted", len(address))
		}
	}
}

func longAddress() string {
	out := make([]byte, 400)
	for i := range out {
		out[i] = 'a'
	}
	return string(out) + "@example.test"
}

// RLS is the enforcement, not a predicate in the query. The organization comes
// from the client the authorization request named, and a user with the same
// address in another tenant is simply not visible.
func TestAnotherTenantsUserIsNotVisible(t *testing.T) {
	f := setupUsers(t)

	other := f.factory.Organization(f.factory.Instance())
	otherUser := f.factory.User(other, "bob@example.test")
	hash, err := Hash(userPassword)
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}
	f.factory.Exec(`UPDATE users SET password_hash = $2 WHERE id = $1`, otherUser, hash)

	// Visible in its own organization — the control, without which the
	// assertion below would pass against a store that finds nobody at all.
	if _, verified := f.authenticate(t, other, "bob@example.test", userPassword); !verified {
		t.Fatal("the user is not visible in its own organization")
	}

	if user, verified := f.authenticate(t, f.orgID, "bob@example.test", userPassword); verified || user.ID != "" {
		t.Error("a user from another organization was authenticated")
	}
}

// PG-13's column reaches the caller, because expiry is evaluated against it
// and a value that never arrives makes max_age_days unenforceable again.
func TestThePasswordChangeTimeIsRead(t *testing.T) {
	f := setupUsers(t)

	if user, _ := f.authenticate(t, f.orgID, userEmail, userPassword); user.PasswordChangedAt != nil {
		t.Errorf("password_changed_at = %v for a row that never recorded one", user.PasswordChangedAt)
	}

	changed := time.Now().Add(-30 * 24 * time.Hour).UTC().Truncate(time.Second)
	f.factory.Exec(`UPDATE users SET password_changed_at = $2 WHERE id = $1`, f.userID, changed)

	user, _ := f.authenticate(t, f.orgID, userEmail, userPassword)
	if user.PasswordChangedAt == nil {
		t.Fatal("password_changed_at was not read")
	}
	if !user.PasswordChangedAt.UTC().Truncate(time.Second).Equal(changed) {
		t.Errorf("password_changed_at = %v, want %v", user.PasswordChangedAt, changed)
	}
}

// --- rehash on login ---------------------------------------------------------------

// Rehashing needs the plaintext, so login is the only moment it can happen.
// What must not happen with it is a change to password_changed_at: the
// password did not change, only the cost of storing it did, and moving that
// column would reset every user's expiry clock on the day the parameters were
// raised.
func TestARehashDoesNotResetTheExpiryClock(t *testing.T) {
	f := setupUsers(t)

	changed := time.Now().Add(-30 * 24 * time.Hour).UTC().Truncate(time.Second)
	f.factory.Exec(`UPDATE users SET password_changed_at = $2 WHERE id = $1`, f.userID, changed)

	var before string
	f.factory.QueryRow(&before, `SELECT password_hash FROM users WHERE id = $1`, f.userID)

	if err := f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
		return f.store.RecordRehash(context.Background(), tx, f.userID, userPassword)
	}); err != nil {
		t.Fatalf("RecordRehash: %v", err)
	}

	var after string
	f.factory.QueryRow(&after, `SELECT password_hash FROM users WHERE id = $1`, f.userID)
	if after == before {
		t.Error("the stored hash did not change; a rehash that writes the same bytes is not one")
	}

	// The new hash still verifies the same password — a rehash that broke
	// login would be a very expensive way to lock everybody out.
	if _, verified := f.authenticate(t, f.orgID, userEmail, userPassword); !verified {
		t.Error("the password no longer verifies after a rehash")
	}

	var stillChanged time.Time
	f.factory.QueryRow(&stillChanged,
		`SELECT password_changed_at FROM users WHERE id = $1`, f.userID)
	if !stillChanged.UTC().Truncate(time.Second).Equal(changed) {
		t.Errorf("password_changed_at moved to %v; a rehash reset the expiry clock", stillChanged)
	}
}
