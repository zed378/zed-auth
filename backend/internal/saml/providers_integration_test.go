//go:build integration

package saml

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

// Resolving a service provider before the tenant is known (P4-08 C-1).
//
// The lookup runs outside any tenant scope, which is exactly the property worth
// testing: a query against the RLS-protected table with no `current_org_id()`
// returns nothing at all, so a lookup that forgot to go through the bounded
// function would fail silently — every login refused, and the reason invisible.

// seeded counts calls so each registration gets its own project and
// application. Two registrations sharing an entity id — which one test needs —
// still need distinct names, because projects are unique per organization.
var seeded int

func seedProvider(t *testing.T, f *replayFixture, entityID string) (spID string) {
	t.Helper()
	stack := testsupport.Start(t)
	factory := testsupport.NewFactory(t, stack)

	seeded++
	name := fmt.Sprintf("%s-%d", t.Name(), seeded)

	var projectID, appID string
	factory.QueryRow(&projectID,
		`INSERT INTO projects (org_id, name) VALUES ($1, $2) RETURNING id`, f.orgID, "p-"+name)
	factory.QueryRow(&appID,
		`INSERT INTO applications (project_id, org_id, name, type) VALUES ($1, $2, $3, 'web') RETURNING id`,
		projectID, f.orgID, "a-"+name)
	factory.QueryRow(&spID, `
		INSERT INTO saml_service_providers
		       (application_id, org_id, entity_id, acs_url, attribute_release)
		VALUES ($1, $2, $3, 'https://sp.example.test/acs', ARRAY['email'])
		RETURNING id`, appID, f.orgID, entityID)
	return spID
}

func TestAServiceProviderResolvesWithNoTenantScope(t *testing.T) {
	f := setupReplay(t)
	entityID := "https://sp.example.test/resolve"
	spID := seedProvider(t, f, entityID)

	store := NewProviderStore()

	// The lookup the SSO endpoint does: no transaction, no tenant.
	reg, err := store.ByEntityID(context.Background(), f.db.SQL(), entityID)
	if err != nil {
		t.Fatalf("resolving by entity id with no tenant scope: %v", err)
	}
	if reg.OrgID != f.orgID {
		t.Errorf("resolved org %q, want %q — the entity id is what determines the tenant", reg.OrgID, f.orgID)
	}
	if reg.ID != spID {
		t.Errorf("resolved registration %q, want %q", reg.ID, spID)
	}
	if reg.ACSURL != "https://sp.example.test/acs" {
		t.Errorf("ACS URL is %q — it comes from the registration, never the request", reg.ACSURL)
	}
	if len(reg.Release) != 1 || reg.Release[0] != "email" {
		t.Errorf("attribute release is %v, want [email]", reg.Release)
	}

	// And by row id, which is how a pending request is answered.
	byID, err := store.ByID(context.Background(), f.db.SQL(), spID)
	if err != nil {
		t.Fatalf("resolving by id with no tenant scope: %v", err)
	}
	if byID.EntityID != entityID {
		t.Errorf("resolved %q by id, want %q", byID.EntityID, entityID)
	}
}

// The property that makes the bounded function necessary rather than tidy: a
// direct read of the table with no tenant returns nothing, so a lookup that
// skipped the function would refuse every login for a reason nobody could see.
func TestADirectReadWithNoTenantSeesNothing(t *testing.T) {
	f := setupReplay(t)
	entityID := "https://sp.example.test/direct"
	seedProvider(t, f, entityID)

	var count int
	if err := f.db.SQL().QueryRowContext(context.Background(),
		`SELECT count(*) FROM saml_service_providers WHERE entity_id = $1`, entityID).Scan(&count); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if count != 0 {
		t.Errorf("a direct read with no tenant saw %d rows — row-level security is not applying, "+
			"and the bounded lookup function is not what it is for", count)
	}
}

func TestAnUnregisteredEntityIDResolvesToNothing(t *testing.T) {
	f := setupReplay(t)
	store := NewProviderStore()

	for name, id := range map[string]string{
		"never registered": "https://stranger.example.test",
		"empty":            "",
	} {
		if _, err := store.ByEntityID(context.Background(), f.db.SQL(), id); !errors.Is(err, ErrNoSuchProvider) {
			t.Errorf("%s: gave %v, want ErrNoSuchProvider", name, err)
		}
	}
}

// A revoked registration keeps its row and loses its ability to start a login.
func TestARevokedServiceProviderCannotStartALogin(t *testing.T) {
	f := setupReplay(t)
	entityID := "https://sp.example.test/revoked"
	spID := seedProvider(t, f, entityID)

	stack := testsupport.Start(t)
	testsupport.NewFactory(t, stack).Exec(
		`UPDATE saml_service_providers SET revoked_at = now() WHERE id = $1`, spID)

	store := NewProviderStore()
	if _, err := store.ByEntityID(context.Background(), f.db.SQL(), entityID); !errors.Is(err, ErrNoSuchProvider) {
		t.Errorf("a revoked registration still resolves by entity id: %v", err)
	}
	if _, err := store.ByID(context.Background(), f.db.SQL(), spID); !errors.Is(err, ErrNoSuchProvider) {
		t.Errorf("a revoked registration still resolves by id: %v", err)
	}
}

// An entity id is a name; the registration is the thing. A service provider
// revoked and re-registered under the same entity id is a different row, and a
// login started against the old one must not complete against the new.
func TestAReRegisteredEntityIDIsADifferentRegistration(t *testing.T) {
	f := setupReplay(t)
	entityID := "https://sp.example.test/rereg"
	first := seedProvider(t, f, entityID)

	stack := testsupport.Start(t)
	factory := testsupport.NewFactory(t, stack)
	factory.Exec(`UPDATE saml_service_providers SET revoked_at = now() WHERE id = $1`, first)

	second := seedProvider(t, f, entityID)
	if first == second {
		t.Fatal("re-registration reused the row")
	}

	store := NewProviderStore()

	// The name resolves to the live one.
	reg, err := store.ByEntityID(context.Background(), f.db.SQL(), entityID)
	if err != nil {
		t.Fatalf("resolving after re-registration: %v", err)
	}
	if reg.ID != second {
		t.Errorf("the entity id resolved to %q, want the live registration %q", reg.ID, second)
	}

	// The old row does not, so a pending request recorded against it cannot be
	// answered by the new registration.
	if _, err := store.ByID(context.Background(), f.db.SQL(), first); !errors.Is(err, ErrNoSuchProvider) {
		t.Error("a login started against the revoked registration can still be completed")
	}
}
