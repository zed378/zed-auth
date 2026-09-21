//go:build integration

package saml

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

// The identifier a service provider knows a user by (P4-08 C-5).

func nameIDFor(t *testing.T, f *replayFixture, userID, spID string) string {
	t.Helper()
	var out string
	if err := f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
		var err error
		out, err = NewNameIDs().For(context.Background(), tx, f.orgID, userID, spID, time.Now())
		return err
	}); err != nil {
		t.Fatalf("minting a NameID: %v", err)
	}
	return out
}

func TestANameIDIsStableForOneUserAtOneServiceProvider(t *testing.T) {
	f := setupReplay(t)
	sp := seedProvider(t, f, "https://sp.example.test/stable")
	user := testsupport.NewFactory(t, testsupport.Start(t)).User(f.orgID, "stable@example.test")

	first := nameIDFor(t, f, user, sp)
	second := nameIDFor(t, f, user, sp)

	if first != second {
		t.Errorf("two sign-ins produced %q and %q — a service provider would see two users", first, second)
	}
	if len(first) < 16 {
		t.Errorf("NameID %q is too short to be unguessable", first)
	}
}

// C-5, the correlation half: two service providers must not be able to compare
// notes and discover they are talking about the same person.
func TestOneUserIsADifferentSubjectToEachServiceProvider(t *testing.T) {
	f := setupReplay(t)
	first := seedProvider(t, f, "https://one.example.test/sp")
	second := seedProvider(t, f, "https://two.example.test/sp")
	user := testsupport.NewFactory(t, testsupport.Start(t)).User(f.orgID, "correlate@example.test")

	a := nameIDFor(t, f, user, first)
	b := nameIDFor(t, f, user, second)

	if a == b {
		t.Error("the same identifier went to two service providers — they can correlate the user")
	}
}

// C-5, the reissued-address half: the identifier must not be derived from
// anything that can be reassigned.
func TestANameIDIsNeverAnAddress(t *testing.T) {
	f := setupReplay(t)
	sp := seedProvider(t, f, "https://sp.example.test/address")
	factory := testsupport.NewFactory(t, testsupport.Start(t))
	user := factory.User(f.orgID, "budi@company.test")

	got := nameIDFor(t, f, user, sp)

	if strings.Contains(got, "@") {
		t.Errorf("NameID %q contains an address — a reissued address inherits the account", got)
	}
	if strings.Contains(got, "budi") || strings.Contains(got, "company") {
		t.Errorf("NameID %q carries the user's address", got)
	}
	// And the database refuses one directly, so the rule holds for any writer.
	//
	// A DIFFERENT user, deliberately. Reusing the one above would collide with
	// the row already minted for it, and the insert would be refused by the
	// primary key while the CHECK was never consulted — a mutation removing the
	// CHECK left that version of this assertion green.
	other := factory.User(f.orgID, "other@company.test")
	var err error
	_ = f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
		_, err = tx.Exec(context.Background(), `
			INSERT INTO saml_name_ids (user_id, sp_id, org_id, name_id, created_at)
			VALUES ($1, $2, $3, 'someone@example.test', now())`, other, sp, f.orgID)
		return nil
	})
	if err == nil {
		t.Error("the database accepted an email address as a NameID")
	}
}

// Two sign-ins racing on a user's first visit must not issue two identities.
func TestConcurrentFirstSignInsMintOneIdentifier(t *testing.T) {
	f := setupReplay(t)
	sp := seedProvider(t, f, "https://sp.example.test/race")
	user := testsupport.NewFactory(t, testsupport.Start(t)).User(f.orgID, "race@example.test")

	const attempts = 8
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		seen = map[string]int{}
	)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var got string
			_ = f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
				var err error
				got, err = NewNameIDs().For(context.Background(), tx, f.orgID, user, sp, time.Now())
				return err
			})
			mu.Lock()
			defer mu.Unlock()
			if got != "" {
				seen[got]++
			}
		}()
	}
	wg.Wait()

	if len(seen) != 1 {
		t.Errorf("%d distinct identifiers were issued for one user at one service provider: %v", len(seen), seen)
	}
}

// Two users are never the same subject to one service provider.
func TestTwoUsersAreNeverTheSameSubject(t *testing.T) {
	f := setupReplay(t)
	sp := seedProvider(t, f, "https://sp.example.test/distinct")
	factory := testsupport.NewFactory(t, testsupport.Start(t))

	a := nameIDFor(t, f, factory.User(f.orgID, "a@example.test"), sp)
	b := nameIDFor(t, f, factory.User(f.orgID, "b@example.test"), sp)

	if a == b {
		t.Error("two users share one identifier at a service provider")
	}
}
