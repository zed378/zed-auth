//go:build integration

package authn

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }

func discard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// openApp connects as the runtime role, not the owner.
//
// The service reads policy as auth_app, so the test does too. A test that
// reads it as the owner would pass against a schema where the runtime role
// cannot SELECT organizations at all — and the first symptom in production
// would be every password change failing.
func openApp(t *testing.T, stack *testsupport.Stack) *postgres.DB {
	t.Helper()

	db, err := postgres.Open(context.Background(), config.PostgresConfig{
		DSN:             stack.AppDSN,
		MaxOpenConns:    4,
		MaxIdleConns:    2,
		ConnMaxLifetime: time.Minute,
	}, discard())
	if err != nil {
		t.Fatalf("opening the app connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// P1-02 DoD item 5: changing organizations.settings.password_policy changes
// enforcement with no code change.
//
// The assertion that matters is that the SAME binary, with no restart and no
// redeploy, enforces a different rule after an UPDATE. Anything less — a test
// that constructs two Policy values by hand — proves only that the evaluator
// takes parameters, which was never in doubt.
func TestPolicyChangeInTheDatabaseChangesEnforcement(t *testing.T) {
	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	orgID := factory.Organization(factory.Instance())

	db := openApp(t, stack)
	store := NewPolicyStore(discard())

	// A password that satisfies the shipped default: 12 characters with an
	// uppercase letter.
	const password = "Abcdefghijkl"

	read := func() Policy {
		t.Helper()
		var policy Policy
		err := db.WithTenant(context.Background(), orgID, func(tx *postgres.Tx) error {
			var err error
			policy, err = store.Policy(context.Background(), tx, orgID)
			return err
		})
		if err != nil {
			t.Fatalf("reading policy: %v", err)
		}
		return policy
	}

	// The migration default, unmodified.
	underDefault := read()
	if underDefault != (Policy{MinLength: 12, RequireUppercase: true, MaxAgeDays: 90}) {
		t.Fatalf("the column default is not what P0-07's migration writes: %+v", underDefault)
	}
	if got := Evaluate(password, underDefault); len(got) != 0 {
		t.Fatalf("the password was rejected under the default policy: %v", rules(got))
	}

	// An administrator raises the minimum. No deploy, no restart.
	factory.Exec(`
		UPDATE organizations
		   SET settings = jsonb_set(settings, '{password_policy,min_length}', '20'::jsonb)
		 WHERE id = $1`, orgID)

	underStricter := read()
	if underStricter.MinLength != 20 {
		t.Fatalf("min_length = %d after the update, want 20 — the policy is not being read from the row",
			underStricter.MinLength)
	}
	if got := rules(Evaluate(password, underStricter)); !equal(got, []string{RuleMinLength}) {
		t.Errorf("under the raised policy = %v, want a length violation", got)
	}

	// And relaxing a different rule takes effect too, so the read is not
	// picking up one field by luck.
	factory.Exec(`
		UPDATE organizations
		   SET settings = jsonb_set(settings, '{password_policy,require_uppercase}', 'false'::jsonb)
		 WHERE id = $1`, orgID)

	relaxed := read()
	if relaxed.RequireUppercase {
		t.Error("require_uppercase is still true after being set to false in the row")
	}
	if got := rules(Evaluate("abcdefghijklmnopqrst", relaxed)); len(got) != 0 {
		t.Errorf("a 20-character lowercase password was rejected after the rule was turned off: %v", got)
	}
}

// A settings document an administrator has broken must not become no policy.
//
// The column has a jsonb_typeof CHECK, so it cannot hold invalid JSON — the
// realistic corruption is a valid document with the wrong shape, which is what
// this writes.
func TestABrokenPolicyDocumentFallsBackToTheDefault(t *testing.T) {
	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	orgID := factory.Organization(factory.Instance())

	db := openApp(t, stack)
	store := NewPolicyStore(discard())

	cases := []struct {
		name     string
		settings string
		want     Policy
	}{
		{"password_policy removed entirely", `{"mfa_required": true}`, DefaultPolicy},
		{"password_policy is not an object", `{"password_policy": "strict"}`, DefaultPolicy},
		{"settings is an empty object", `{}`, DefaultPolicy},
		{
			// The case that matters most: an administrator disabling the
			// control by configuring it away.
			name:     "min_length configured below the floor",
			settings: `{"password_policy": {"min_length": 1}}`,
			want:     Policy{MinLength: MinLengthFloor, RequireUppercase: true, MaxAgeDays: 90},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			factory.Exec(`UPDATE organizations SET settings = $2::jsonb WHERE id = $1`, orgID, tc.settings)

			var policy Policy
			err := db.WithTenant(context.Background(), orgID, func(tx *postgres.Tx) error {
				var err error
				policy, err = store.Policy(context.Background(), tx, orgID)
				return err
			})
			if err != nil {
				t.Fatalf("reading policy: %v", err)
			}
			if policy != tc.want {
				t.Errorf("policy = %+v, want %+v", policy, tc.want)
			}

			// And a weak password is still refused, which is the point of the
			// fallback rather than the value of the struct.
			if got := Evaluate("abc", policy); len(got) == 0 {
				t.Error(`"abc" was accepted; a broken settings document became no policy`)
			}
		})
	}
}

// Another tenant's policy is not readable, so it cannot be enforced by mistake.
//
// RLS (P0-08) makes this not-found rather than forbidden — a "forbidden" would
// confirm the row exists.
func TestPolicyIsNotReadableAcrossTenants(t *testing.T) {
	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	orgA, orgB := factory.TwoOrganizations()

	factory.Exec(`
		UPDATE organizations
		   SET settings = jsonb_set(settings, '{password_policy,min_length}', '30'::jsonb)
		 WHERE id = $1`, orgB)

	db := openApp(t, stack)
	store := NewPolicyStore(discard())

	err := db.WithTenant(context.Background(), orgA, func(tx *postgres.Tx) error {
		_, err := store.Policy(context.Background(), tx, orgB)
		return err
	})
	if err == nil {
		t.Fatal("org A read org B's password policy; RLS is not scoping this query")
	}

	// A control: the same read inside B's own scope works, so the assertion
	// above failed because of isolation and not because the row is missing or
	// the query is broken.
	err = db.WithTenant(context.Background(), orgB, func(tx *postgres.Tx) error {
		policy, err := store.Policy(context.Background(), tx, orgB)
		if err != nil {
			return err
		}
		if policy.MinLength != 30 {
			t.Errorf("min_length = %d in org B's own scope, want 30", policy.MinLength)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("org B could not read its own policy: %v", err)
	}
}

// PG-13: the column exists, defaults to NULL, and NULL is not expired.
//
// The migration is only useful if the column can actually be written and read
// back through the runtime role — an ALTER TABLE that lands without the
// corresponding grant is a change that passes CI and fails in production.
func TestPasswordChangedAtRoundTrips(t *testing.T) {
	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	orgID := factory.Organization(factory.Instance())
	userID := factory.User(orgID)

	db := openApp(t, stack)
	ninety := Policy{MinLength: 12, MaxAgeDays: 90}

	read := func() *time.Time {
		t.Helper()
		var at *time.Time
		err := db.WithTenant(context.Background(), orgID, func(tx *postgres.Tx) error {
			return tx.QueryRow(context.Background(),
				`SELECT password_changed_at FROM users WHERE id = $1`, userID).Scan(&at)
		})
		if err != nil {
			t.Fatalf("reading password_changed_at: %v", err)
		}
		return at
	}

	// A user created before the column meant anything.
	if at := read(); at != nil {
		t.Fatalf("a new user has password_changed_at = %v, want NULL", at)
	}
	if Expired(read(), ninety, time.Now()) {
		t.Error("a user with no recorded change time was reported expired; " +
			"deploying this migration would have locked out every existing user")
	}

	// The runtime role can write it, which is what the login and change flows
	// will need.
	old := time.Now().AddDate(0, 0, -100)
	err := db.WithTenant(context.Background(), orgID, func(tx *postgres.Tx) error {
		_, err := tx.Exec(context.Background(),
			`UPDATE users SET password_changed_at = $2 WHERE id = $1`, userID, old)
		return err
	})
	if err != nil {
		t.Fatalf("the runtime role cannot write password_changed_at: %v", err)
	}

	at := read()
	if at == nil {
		t.Fatal("password_changed_at read back as NULL after being written")
	}
	if !Expired(at, ninety, time.Now()) {
		t.Error("a password changed 100 days ago is not expired under a 90-day policy")
	}
}
