//go:build integration

// What an assertion says about a user, against real Postgres (P4-08 F-6, C-5).
package samlapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/saml"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }

type subjectFixture struct {
	db      *postgres.DB
	factory *testsupport.Factory
	orgID   string
	reg     saml.Registration
	userID  string
}

func setupSubjects(t *testing.T, release []string) *subjectFixture {
	t.Helper()
	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	orgID := factory.Organization(factory.Instance())

	var projectID, appID, spID string
	factory.QueryRow(&projectID, `INSERT INTO projects (org_id, name) VALUES ($1, 'sp-project') RETURNING id`, orgID)
	factory.QueryRow(&appID,
		`INSERT INTO applications (project_id, org_id, name, type) VALUES ($1, $2, 'sp-app', 'web') RETURNING id`,
		projectID, orgID)
	factory.QueryRow(&spID, `
		INSERT INTO saml_service_providers (application_id, org_id, entity_id, acs_url, attribute_release)
		VALUES ($1, $2, 'https://sp.example.test', 'https://sp.example.test/acs', $3)
		RETURNING id`, appID, orgID, pqArray(release))

	userID := factory.User(orgID, "budi@company.test")
	factory.Exec(`UPDATE users SET display_name = 'Budi Santoso', username = 'budi' WHERE id = $1`, userID)
	factory.Exec(`INSERT INTO roles (org_id, project_id, key, display_name) VALUES ($1, $2, 'cashier', 'cashier')`,
		orgID, projectID)
	factory.Exec(`INSERT INTO user_grants (user_id, org_id, project_id, role_keys) VALUES ($1, $2, $3, '{cashier}')`,
		userID, orgID, projectID)

	db, err := postgres.Open(context.Background(), config.PostgresConfig{
		DSN: stack.AppDSN, MaxOpenConns: 8, MaxIdleConns: 4, ConnMaxLifetime: time.Minute,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("opening: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return &subjectFixture{
		db: db, factory: factory, orgID: orgID, userID: userID,
		reg: saml.Registration{
			ServiceProvider: saml.ServiceProvider{
				EntityID: "https://sp.example.test",
				ACSURL:   "https://sp.example.test/acs",
				Release:  release,
			},
			ID: spID, OrgID: orgID, ApplicationID: appID,
		},
	}
}

func pqArray(values []string) string {
	if len(values) == 0 {
		return "{}"
	}
	return "{" + strings.Join(values, ",") + "}"
}

func (f *subjectFixture) subject(t *testing.T) (saml.Subject, error) {
	t.Helper()
	var (
		out saml.Subject
		err error
	)
	_ = f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
		out, err = NewSubjectStore().For(context.Background(), tx, f.userID, f.reg)
		return nil
	})
	return out, err
}

// F-6: the service provider receives what it was registered for, and nothing
// else. The user has an email, a display name, a username and a role.
func TestOnlyRegisteredAttributesLeaveTheDatabase(t *testing.T) {
	f := setupSubjects(t, []string{"email"})

	subject, err := f.subject(t)
	if err != nil {
		t.Fatalf("building the subject: %v", err)
	}

	if got := subject.Attributes["email"]; len(got) != 1 || got[0] != "budi@company.test" {
		t.Errorf("email is %v, want the address", got)
	}
	for _, withheld := range []string{"display_name", "username", "role_keys"} {
		if values, ok := subject.Attributes[withheld]; ok {
			t.Errorf("%s was released as %v — the registration did not ask for it", withheld, values)
		}
	}
}

// A service provider registered for nothing receives nothing, not everything.
func TestAnUnregisteredServiceProviderReceivesNoAttributes(t *testing.T) {
	f := setupSubjects(t, nil)

	subject, err := f.subject(t)
	if err != nil {
		t.Fatalf("building the subject: %v", err)
	}
	if len(subject.Attributes) != 0 {
		t.Errorf("an unregistered service provider received %v", subject.Attributes)
	}
	// It still gets a subject — the NameID is what an assertion is ABOUT, not
	// an attribute somebody opts into.
	if subject.NameID == "" {
		t.Error("no NameID was minted")
	}
}

// C-5: never the address, and the format says what the value is.
func TestTheSubjectIsPersistentAndNotAnAddress(t *testing.T) {
	f := setupSubjects(t, []string{"email"})

	subject, err := f.subject(t)
	if err != nil {
		t.Fatalf("building the subject: %v", err)
	}

	if strings.Contains(subject.NameID, "@") || strings.Contains(subject.NameID, "budi") {
		t.Errorf("NameID %q is derived from the address", subject.NameID)
	}
	if subject.NameIDFormat != saml.NameIDFormatPersistent {
		t.Errorf("NameIDFormat is %q — a format that said emailAddress while the value is a random "+
			"identifier would be a lie a service provider acts on", subject.NameIDFormat)
	}

	// Stable across sign-ins, while the attributes are re-read each time.
	again, err := f.subject(t)
	if err != nil {
		t.Fatalf("second sign-in: %v", err)
	}
	if again.NameID != subject.NameID {
		t.Error("the NameID changed between two sign-ins")
	}
}

// A session outlives a deactivation by design — revoking every one is not
// instant — so this is the check that stops a deactivated user from signing in
// to a service provider in the meantime.
func TestADeactivatedUserGetsNoAssertion(t *testing.T) {
	f := setupSubjects(t, []string{"email"})

	if _, err := f.subject(t); err != nil {
		t.Fatalf("the active user was refused: %v", err)
	}

	f.factory.Exec(`UPDATE users SET status = 'deactivated' WHERE id = $1`, f.userID)

	if _, err := f.subject(t); !errors.Is(err, ErrNoSuchUser) {
		t.Errorf("a deactivated user got an assertion (err %v)", err)
	}
}

// Roles are released only when registered, and they are the roles of THIS
// application's project rather than every role the user holds anywhere.
func TestReleasedRolesAreScopedToTheApplicationsProject(t *testing.T) {
	f := setupSubjects(t, []string{"role_keys"})

	// A role in a different project of the same organization, which this
	// service provider has no business hearing about.
	var otherProject string
	f.factory.QueryRow(&otherProject,
		`INSERT INTO projects (org_id, name) VALUES ($1, 'elsewhere') RETURNING id`, f.orgID)
	f.factory.Exec(`INSERT INTO roles (org_id, project_id, key, display_name) VALUES ($1, $2, 'auditor', 'auditor')`,
		f.orgID, otherProject)
	f.factory.Exec(`INSERT INTO user_grants (user_id, org_id, project_id, role_keys) VALUES ($1, $2, $3, '{auditor}')`,
		f.userID, f.orgID, otherProject)

	subject, err := f.subject(t)
	if err != nil {
		t.Fatalf("building the subject: %v", err)
	}

	roles := subject.Attributes["role_keys"]
	if len(roles) != 1 || roles[0] != "cashier" {
		t.Errorf("released roles are %v, want only this project's [cashier]", roles)
	}
}
