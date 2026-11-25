//go:build integration

package social

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
	"github.com/zed378/zed-auth/backend/migrations"
)

func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }

type identityFixture struct {
	db           *postgres.DB
	owner        *sql.DB
	factory      *testsupport.Factory
	orgA, orgB   string
	userA, userB string
}

func identitySetup(t *testing.T) *identityFixture {
	t.Helper()
	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)
	factory := testsupport.NewFactory(t, stack)
	orgA, orgB := factory.TwoOrganizations()
	db, err := postgres.Open(context.Background(), config.PostgresConfig{
		DSN: stack.AppDSN, MaxOpenConns: 4, MaxIdleConns: 2,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	owner, err := sql.Open("pgx", stack.OwnerDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	return &identityFixture{
		db: db, owner: owner, factory: factory,
		orgA: orgA, orgB: orgB, userA: factory.User(orgA), userB: factory.User(orgB),
	}
}

// Fixtures stand in for P4-11's authenticated linking flow, which is not yet
// exposed. Production sign-in gets only the read operation in Store.
func (f *identityFixture) link(t *testing.T, orgID, userID string, provider Provider, subject string) string {
	t.Helper()
	var id string
	err := f.db.WithTenant(context.Background(), orgID, func(tx *postgres.Tx) error {
		return tx.QueryRow(context.Background(), `INSERT INTO user_identities (user_id, provider, provider_subject)
			VALUES ($1, $2, $3) RETURNING id`, userID, string(provider), subject).Scan(&id)
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *identityFixture) lookup(orgID string, provider Provider, subject string) (Identity, error) {
	var identity Identity
	err := f.db.WithTenant(context.Background(), orgID, func(tx *postgres.Tx) error {
		var err error
		identity, err = NewStore().BySubject(context.Background(), tx, provider, subject)
		return err
	})
	return identity, err
}

func requireSQLState(t *testing.T, err error, code string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code {
		t.Fatalf("got %v, want SQLSTATE %s", err, code)
	}
}

func TestIdentityLookupUsesBothProviderAndSubject(t *testing.T) {
	f := identitySetup(t)
	for _, provider := range []Provider{Google, Microsoft, GitHub} {
		userID := f.factory.User(f.orgA)
		id := f.link(t, f.orgA, userID, provider, "same-subject")
		got, err := f.lookup(f.orgA, provider, "same-subject")
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != id || got.UserID != userID || got.Provider != provider || got.Subject != "same-subject" || got.LinkedAt.IsZero() {
			t.Fatalf("wrong identity: %+v", got)
		}
	}
}

func TestSubjectMatchingIsExactAndParameterized(t *testing.T) {
	f := identitySetup(t)
	for _, subject := range []string{"AbC", "abc", " Subject ", "Subject", "subject@example.test", "x' OR true --", strings.Repeat("x", MaxSubjectBytes)} {
		id := f.link(t, f.orgA, f.userA, Google, subject)
		got, err := f.lookup(f.orgA, Google, subject)
		if err != nil || got.ID != id || got.Subject != subject {
			t.Fatalf("lookup %q: identity=%+v, error=%v", subject, got, err)
		}
	}
	for _, lookup := range []struct {
		provider Provider
		subject  string
	}{{Google, "ABC"}, {Google, "subject"}, {GitHub, "AbC"}, {Google, "' OR 1=1 --"}} {
		if _, err := f.lookup(f.orgA, lookup.provider, lookup.subject); !errors.Is(err, ErrNotFound) {
			t.Fatalf("non-matching identity resolved: %v", err)
		}
	}
}

// P4-10 abuse case: provider email changes cannot redirect an identity to the
// owner of its new address. Email is never part of the identity lookup.
func TestEmailChangesNeverChangeTheLinkedUser(t *testing.T) {
	f := identitySetup(t)
	id := f.link(t, f.orgA, f.userA, Google, "stable-subject")
	var originalEmail, otherEmail string
	if err := f.owner.QueryRow(`SELECT email FROM users WHERE id = $1`, f.userA).Scan(&originalEmail); err != nil {
		t.Fatal(err)
	}
	if err := f.owner.QueryRow(`SELECT email FROM users WHERE id = $1`, f.userB).Scan(&otherEmail); err != nil {
		t.Fatal(err)
	}
	if _, err := f.owner.Exec(`UPDATE users SET email = $1 WHERE id = $2`, otherEmail, f.userA); err != nil {
		t.Fatal(err)
	}
	got, err := f.lookup(f.orgA, Google, "stable-subject")
	if err != nil || got.ID != id || got.UserID != f.userA {
		t.Fatalf("email change changed the identity: %+v, %v", got, err)
	}
	for _, email := range []string{originalEmail, otherEmail} {
		if _, err := f.lookup(f.orgA, Google, email); !errors.Is(err, ErrNotFound) {
			t.Fatalf("email was treated as an identity: %v", err)
		}
	}
	var users, identities int
	if err := f.owner.QueryRow(`SELECT (SELECT count(*) FROM users), (SELECT count(*) FROM user_identities)`).Scan(&users, &identities); err != nil {
		t.Fatal(err)
	}
	if users != 2 || identities != 1 {
		t.Fatalf("lookup provisioned records: %d users, %d identities", users, identities)
	}
}

func TestIdentityIsolationHoldsWithoutApplicationFiltering(t *testing.T) {
	f := identitySetup(t)
	f.link(t, f.orgA, f.userA, Google, "tenant-a")
	f.link(t, f.orgB, f.userB, Google, "tenant-b")
	for _, orgID := range []string{f.orgA, f.orgB} {
		var count int
		err := f.db.WithTenant(context.Background(), orgID, func(tx *postgres.Tx) error {
			return tx.QueryRow(context.Background(), `SELECT count(*) FROM user_identities`).Scan(&count)
		})
		if err != nil || count != 1 {
			t.Fatalf("tenant sees %d identities: %v", count, err)
		}
	}
	for _, subject := range []string{"tenant-b", "unknown"} {
		got, err := f.lookup(f.orgA, Google, subject)
		if !errors.Is(err, ErrNotFound) || got != (Identity{}) {
			t.Fatalf("foreign and unknown identities must be indistinguishable: %+v, %v", got, err)
		}
	}
	var ownerCount int
	if err := f.owner.QueryRow(`SELECT count(*) FROM user_identities`).Scan(&ownerCount); err != nil || ownerCount != 2 {
		t.Fatalf("isolation fixture is missing: count=%d, error=%v", ownerCount, err)
	}
}

func TestIdentitiesFailClosedWithoutTenantContext(t *testing.T) {
	f := identitySetup(t)
	f.link(t, f.orgA, f.userA, Google, "subject")
	var count int
	if err := f.db.SQL().QueryRow(`SELECT count(*) FROM user_identities`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unscoped connection sees %d identities: %v", count, err)
	}
	err := f.db.WithInstanceScope(context.Background(), "test social identity isolation", func(tx *postgres.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM user_identities`).Scan(&count)
	})
	if err != nil || count != 0 {
		t.Fatalf("instance scope sees %d identities: %v", count, err)
	}
}

func TestAnotherTenantCannotLinkOrDeleteAnIdentity(t *testing.T) {
	f := identitySetup(t)
	id := f.link(t, f.orgB, f.userB, Google, "foreign-subject")
	err := f.db.WithTenant(context.Background(), f.orgA, func(tx *postgres.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO user_identities (user_id, provider, provider_subject)
			VALUES ($1, 'github', 'attacker')`, f.userB)
		return err
	})
	requireSQLState(t, err, "42501")
	var deleted int64
	err = f.db.WithTenant(context.Background(), f.orgA, func(tx *postgres.Tx) error {
		result, err := tx.Exec(context.Background(), `DELETE FROM user_identities WHERE id = $1`, id)
		if err != nil {
			return err
		}
		deleted, err = result.RowsAffected()
		return err
	})
	if err != nil || deleted != 0 {
		t.Fatalf("foreign deletion affected %d rows: %v", deleted, err)
	}
	if got, err := f.lookup(f.orgB, Google, "foreign-subject"); err != nil || got.ID != id {
		t.Fatalf("foreign identity was altered: %+v, %v", got, err)
	}
}

func TestRuntimeRoleCannotReassignAnIdentity(t *testing.T) {
	f := identitySetup(t)
	id := f.link(t, f.orgA, f.userA, Google, "subject")
	otherUser := f.factory.User(f.orgA)
	err := f.db.WithTenant(context.Background(), f.orgA, func(tx *postgres.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE user_identities SET user_id = $1 WHERE id = $2`, otherUser, id)
		return err
	})
	requireSQLState(t, err, "42501")
	if got, err := f.lookup(f.orgA, Google, "subject"); err != nil || got.UserID != f.userA {
		t.Fatalf("binding changed: %+v, %v", got, err)
	}
}

func TestProviderSubjectCannotBeLinkedToASecondUser(t *testing.T) {
	f := identitySetup(t)
	f.link(t, f.orgA, f.userA, Google, "subject")
	for _, target := range []struct{ orgID, userID string }{
		{f.orgA, f.factory.User(f.orgA)}, {f.orgB, f.userB},
	} {
		err := f.db.WithTenant(context.Background(), target.orgID, func(tx *postgres.Tx) error {
			_, err := tx.Exec(context.Background(), `INSERT INTO user_identities (user_id, provider, provider_subject)
				VALUES ($1, 'google', 'subject')`, target.userID)
			return err
		})
		requireSQLState(t, err, "23505")
	}
}

func TestConcurrentLinkingHasExactlyOneWinner(t *testing.T) {
	f := identitySetup(t)
	otherUser := f.factory.User(f.orgA)
	const racers = 16
	results := make(chan error, racers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range racers {
		userID := f.userA
		if i%2 == 0 {
			userID = otherUser
		}
		wg.Go(func() {
			<-start
			results <- f.db.WithTenant(context.Background(), f.orgA, func(tx *postgres.Tx) error {
				_, err := tx.Exec(context.Background(), `INSERT INTO user_identities (user_id, provider, provider_subject)
					VALUES ($1, 'google', 'raced-subject')`, userID)
				return err
			})
		})
	}
	close(start)
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else {
			requireSQLState(t, err, "23505")
		}
	}
	if winners != 1 {
		t.Fatalf("%d attempts linked the same subject, want 1", winners)
	}
	if _, err := f.lookup(f.orgA, Google, "raced-subject"); err != nil {
		t.Fatal(err)
	}
}

func TestDatabaseRejectsInvalidProviderAndSubject(t *testing.T) {
	f := identitySetup(t)
	for _, input := range []struct{ provider, subject string }{
		{"password", "subject"}, {"Google", "subject"}, {"", "subject"},
		{"google", ""}, {"google", strings.Repeat("x", MaxSubjectBytes+1)},
		{"google", strings.Repeat("é", 128)},
	} {
		err := f.db.WithTenant(context.Background(), f.orgA, func(tx *postgres.Tx) error {
			_, err := tx.Exec(context.Background(), `INSERT INTO user_identities (user_id, provider, provider_subject)
				VALUES ($1, $2, $3)`, f.userA, input.provider, input.subject)
			return err
		})
		requireSQLState(t, err, "23514")
	}
}

func TestDeletingAUserDeletesItsIdentityLinks(t *testing.T) {
	f := identitySetup(t)
	f.link(t, f.orgA, f.userA, Google, "subject")
	if _, err := f.owner.Exec(`DELETE FROM users WHERE id = $1`, f.userA); err != nil {
		t.Fatal(err)
	}
	if _, err := f.lookup(f.orgA, Google, "subject"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted user still resolves: %v", err)
	}
	var count int
	if err := f.owner.QueryRow(`SELECT count(*) FROM user_identities`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("orphaned identities: %d, %v", count, err)
	}
}

func TestIdentityMigrationCanBeRolledBackAndReapplied(t *testing.T) {
	f := identitySetup(t)
	for _, direction := range []string{"down", "up"} {
		migration, err := migrations.FS.ReadFile("20261125000047_user_identities." + direction + ".sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.owner.Exec(string(migration)); err != nil {
			t.Fatalf("migration %s: %v", direction, err)
		}
	}
	id := f.link(t, f.orgA, f.userA, Microsoft, "after-rollback")
	got, err := f.lookup(f.orgA, Microsoft, "after-rollback")
	if err != nil || got.ID != id || got.LinkedAt.IsZero() {
		t.Fatalf("reapplied migration: %+v, %v", got, err)
	}
}
