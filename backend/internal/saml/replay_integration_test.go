//go:build integration

// Replay protection against real Postgres (P4-07 A-4).
//
// These run here rather than against a fake, because the protection IS the
// database: the primary key is what makes a concurrent replay impossible, and a
// map with a mutex would test a property this code does not rely on.
package saml

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"io"
	"log/slog"

	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }

type replayFixture struct {
	db    *postgres.DB
	orgID string
}

func setupReplay(t *testing.T) *replayFixture {
	t.Helper()
	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	orgID := factory.Organization(factory.Instance())

	db, err := postgres.Open(context.Background(), config.PostgresConfig{
		DSN: stack.AppDSN, MaxOpenConns: 8, MaxIdleConns: 4, ConnMaxLifetime: time.Minute,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("opening the app connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return &replayFixture{db: db, orgID: orgID}
}

func (f *replayFixture) record(t *testing.T, id string, issued, expires time.Time) error {
	t.Helper()
	var result error
	err := f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
		result = NewReplay().Record(context.Background(), tx, f.orgID, id, issued, expires)
		// The replay refusal is an ANSWER, not a transaction failure: returning
		// it here would roll back a transaction that did exactly what it should.
		if errors.Is(result, ErrReplayed) {
			return nil
		}
		return result
	})
	if err != nil && !errors.Is(err, ErrReplayed) {
		t.Fatalf("recording %q: %v", id, err)
	}
	return result
}

func TestAnAssertionIsAcceptedOnceAndRefusedAfterwards(t *testing.T) {
	f := setupReplay(t)
	now := time.Now()

	if err := f.record(t, "_first", now, now.Add(5*time.Minute)); err != nil {
		t.Fatalf("the first presentation was refused: %v", err)
	}
	if err := f.record(t, "_first", now, now.Add(5*time.Minute)); !errors.Is(err, ErrReplayed) {
		t.Errorf("the second presentation gave %v, want ErrReplayed", err)
	}

	// A different assertion is unaffected — without this, a function that
	// refuses everything after the first call would pass the test above.
	if err := f.record(t, "_second", now, now.Add(5*time.Minute)); err != nil {
		t.Errorf("a different assertion was refused: %v", err)
	}
}

// The race the SELECT-then-INSERT shape loses.
//
// An attacker replaying a captured assertion controls the timing exactly, so
// "the window is small" is not a defence. Ten concurrent presentations of the
// same ID must produce exactly one acceptance.
func TestConcurrentPresentationsOfTheSameAssertionAcceptExactlyOne(t *testing.T) {
	f := setupReplay(t)
	now := time.Now()

	const attempts = 10
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		accepted int
		refused  int
	)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var err error
			_ = f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
				err = NewReplay().Record(context.Background(), tx, f.orgID, "_raced", now, now.Add(time.Minute))
				if errors.Is(err, ErrReplayed) {
					return nil
				}
				return err
			})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				accepted++
			case errors.Is(err, ErrReplayed):
				refused++
			}
		}()
	}
	wg.Wait()

	if accepted != 1 {
		t.Errorf("%d of %d concurrent presentations were accepted, want exactly 1", accepted, attempts)
	}
	if accepted+refused != attempts {
		t.Errorf("%d accepted + %d refused = %d, want %d — the rest failed for another reason",
			accepted, refused, accepted+refused, attempts)
	}
}

// The defence is the primary key, and this asserts it directly.
//
// A mutation replacing the atomic upsert with SELECT-then-INSERT did NOT fail
// the concurrency test above, and the reason is worth recording rather than
// papering over: the primary key stops the second acceptance in BOTH shapes.
// What the upsert buys is a clean answer — the naive shape occasionally returns
// a raw constraint violation instead of ErrReplayed, which is a worse error and
// not a weaker guarantee.
//
// So the security property belongs to the schema, and it is tested where it
// lives. A migration that dropped the key would leave every test above green.
func TestTheDatabaseItselfRefusesADuplicateAssertionID(t *testing.T) {
	f := setupReplay(t)
	now := time.Now()

	if err := f.record(t, "_pk", now, now.Add(time.Minute)); err != nil {
		t.Fatalf("recording: %v", err)
	}

	// Straight past the application logic.
	var err error
	_ = f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
		_, err = tx.Exec(context.Background(), `
			INSERT INTO saml_assertion_ids (assertion_id, org_id, issued_at, expires_at)
			VALUES ($1, $2, $3, $4)`, "_pk", f.orgID, now, now.Add(time.Minute))
		return nil
	})
	if err == nil {
		t.Error("the database accepted a duplicate assertion id — the primary key is the replay defence")
	}
}

// An assertion with no ID cannot be protected, so it is refused rather than
// recorded as one that can be used forever.
func TestAnAssertionWithNoIDIsRefused(t *testing.T) {
	f := setupReplay(t)
	now := time.Now()

	for _, id := range []string{"", "   "} {
		if err := f.record(t, id, now, now.Add(time.Minute)); !errors.Is(err, ErrReplayed) {
			t.Errorf("an assertion with ID %q gave %v, want a refusal", id, err)
		}
	}
}

func TestPruningRemovesOnlyExpiredAssertionIDs(t *testing.T) {
	f := setupReplay(t)
	now := time.Now()

	if err := f.record(t, "_expired", now.Add(-time.Hour), now.Add(-time.Minute)); err != nil {
		t.Fatalf("seeding the expired id: %v", err)
	}
	if err := f.record(t, "_live", now, now.Add(time.Hour)); err != nil {
		t.Fatalf("seeding the live id: %v", err)
	}

	var removed int64
	if err := f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
		var err error
		removed, err = NewReplay().Prune(context.Background(), tx, now)
		return err
	}); err != nil {
		t.Fatalf("pruning: %v", err)
	}
	if removed != 1 {
		t.Errorf("pruned %d rows, want 1", removed)
	}

	// The live one is still protected, and the expired one is now free to be
	// recorded again — which is correct, because CheckConditions refuses it on
	// its window regardless.
	if err := f.record(t, "_live", now, now.Add(time.Hour)); !errors.Is(err, ErrReplayed) {
		t.Errorf("a live assertion id was pruned: %v", err)
	}
	if err := f.record(t, "_expired", now, now.Add(time.Hour)); err != nil {
		t.Errorf("an expired id could not be recorded again after pruning: %v", err)
	}
}

// A-7 adjacent: one organization's assertion ids are not another's.
func TestAssertionIDsAreTenantScoped(t *testing.T) {
	f := setupReplay(t)
	factory := testsupport.NewFactory(t, testsupport.Start(t))
	other := factory.Organization(factory.Instance())
	now := time.Now()

	if err := f.record(t, "_shared", now, now.Add(time.Minute)); err != nil {
		t.Fatalf("recording under the first organization: %v", err)
	}

	// The same ID under a different tenant. The primary key is global, so this
	// IS refused — and that is the correct, conservative answer: an assertion
	// ID collision across tenants is either a generator problem or an attack,
	// and accepting it would need the key to include the organization.
	var err error
	_ = f.db.WithTenant(context.Background(), other, func(tx *postgres.Tx) error {
		err = NewReplay().Record(context.Background(), tx, other, "_shared", now, now.Add(time.Minute))
		if errors.Is(err, ErrReplayed) {
			return nil
		}
		return err
	})
	if !errors.Is(err, ErrReplayed) {
		t.Errorf("the same assertion id under another tenant gave %v, want ErrReplayed", err)
	}
}
