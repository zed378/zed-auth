//go:build integration

// The organization store against a real PostgreSQL (P1-16).
//
// Everything here is a database property: which transaction can see which row,
// what the SECURITY DEFINER functions can and cannot reach, whether a settings
// merge preserves the keys it did not mention, and whether an audit trail
// outlives the tenant it belongs to. None of it can be shown against a fake.
package organization

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type fixture struct {
	db      *postgres.DB
	store   *Store
	factory *testsupport.Factory
}

// openApp connects as the RUNTIME role, never the owner.
//
// P1-14 found what happens otherwise: a tenant-scoped table read as the owner
// returns nothing under RLS, every "no rows leaked" assertion passes, and the
// test proves nothing.
func openApp(t *testing.T, stack *testsupport.Stack) *postgres.DB {
	t.Helper()

	db, err := postgres.Open(context.Background(), config.PostgresConfig{
		DSN: stack.AppDSN, MaxOpenConns: 8, MaxIdleConns: 4, ConnMaxLifetime: time.Minute,
	}, discard())
	if err != nil {
		t.Fatalf("opening the app connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func setup(t *testing.T) fixture {
	t.Helper()

	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	db := openApp(t, stack)
	factory := testsupport.NewFactory(t, stack)
	factory.Instance() // organization_create finds the single instance itself

	return fixture{db: db, store: NewStore(), factory: factory}
}

func (f fixture) create(t *testing.T, name string, domain *string, settings string) Organization {
	t.Helper()

	var raw json.RawMessage
	if settings != "" {
		raw = json.RawMessage(settings)
	}
	o, err := f.store.Create(context.Background(), f.db,
		NewOrganization{Name: name, Domain: domain, Settings: raw})
	if err != nil {
		t.Fatalf("Create(%q): %v", name, err)
	}
	return o
}

// in runs fn scoped to one organization, which is how every single-organization
// operation runs in production.
func (f fixture) in(t *testing.T, orgID string, fn func(*postgres.Tx) error) error {
	t.Helper()
	return f.db.WithTenant(context.Background(), orgID, fn)
}

func ptr[T any](v T) *T { return &v }

// --- creating -------------------------------------------------------------------------

// A create runs before its tenant exists, so no scope can apply. It works
// anyway, which is the SECURITY DEFINER function doing its job.
func TestCreateWorksBeforeTheTenantExists(t *testing.T) {
	f := setup(t)

	o := f.create(t, "Acme Corp", ptr("acme.example"), "")

	if o.ID == "" {
		t.Fatal("no id returned")
	}
	if o.Name != "Acme Corp" {
		t.Errorf("name = %q", o.Name)
	}
	if o.Domain == nil || *o.Domain != "acme.example" {
		t.Errorf("domain = %v", o.Domain)
	}
	if o.Status != StatusActive {
		t.Errorf("status = %q, want active", o.Status)
	}
}

// **A new organization gets the instance's policy, not an empty document.**
//
// An organization created with no password policy at all is a tenant whose
// password rules are silently absent, which looks identical to a tenant whose
// rules are permissive.
func TestANewOrganizationGetsTheDefaultPolicy(t *testing.T) {
	f := setup(t)

	o := f.create(t, "Defaults", nil, "")

	var settings map[string]any
	if err := json.Unmarshal(o.Settings, &settings); err != nil {
		t.Fatalf("settings did not decode: %s", o.Settings)
	}
	for _, key := range []string{
		"password_policy", "mfa_required", "session_lifetime_hours", "allowed_login_methods",
	} {
		if _, ok := settings[key]; !ok {
			t.Errorf("%q is missing from a new organization's settings: %s", key, o.Settings)
		}
	}
}

// Settings supplied at creation are MERGED onto the default, not substituted
// for it. A caller who sets only mfa_required must not thereby delete the
// password policy.
func TestSettingsAtCreationMergeOntoTheDefault(t *testing.T) {
	f := setup(t)

	o := f.create(t, "Partial", nil, `{"mfa_required": true}`)

	var settings map[string]any
	if err := json.Unmarshal(o.Settings, &settings); err != nil {
		t.Fatalf("settings: %s", o.Settings)
	}
	if settings["mfa_required"] != true {
		t.Errorf("mfa_required = %v, want the value that was set", settings["mfa_required"])
	}
	if _, ok := settings["password_policy"]; !ok {
		t.Errorf("the password policy was replaced rather than merged: %s", o.Settings)
	}
}

// The domain is lowercased, or two tenants hold the same domain in different
// cases and tenant resolution is ambiguous — a cross-tenant access bug waiting
// to happen (P2-09).
func TestADomainIsStoredLowercased(t *testing.T) {
	f := setup(t)

	o := f.create(t, "Mixed", ptr("  ACME.Example  "), "")

	if o.Domain == nil || *o.Domain != "acme.example" {
		t.Errorf("domain = %v, want the normalised form", o.Domain)
	}
}

// And a second organization cannot take it, whatever case it uses.
func TestADuplicateDomainIsAConflict(t *testing.T) {
	f := setup(t)
	f.create(t, "First", ptr("acme.example"), "")

	_, err := f.store.Create(context.Background(), f.db,
		NewOrganization{Name: "Second", Domain: ptr("ACME.EXAMPLE")})
	if err == nil {
		t.Fatal("two organizations hold the same domain")
	}

	var fault management.Fault
	if !errors.As(err, &fault) || fault.Class != management.Conflict {
		t.Fatalf("err = %v, want a Conflict Fault", err)
	}
	// **The refusal must not say WHO holds it.** Which organization owns a
	// domain is not something a caller who does not administer it should learn
	// from a 409.
	if strings.Contains(fault.Message, "First") {
		t.Errorf("the refusal names the holder: %q", fault.Message)
	}
}

// --- reading one ----------------------------------------------------------------------

// Get takes no id: the row RLS lets the transaction see IS the target, so
// there is no parameter for a handler to pass wrongly.
func TestGetReturnsTheScopedOrganization(t *testing.T) {
	f := setup(t)
	a := f.create(t, "Alpha", nil, "")
	b := f.create(t, "Beta", nil, "")

	var got Organization
	if err := f.in(t, a.ID, func(tx *postgres.Tx) error {
		var err error
		got, err = f.store.Get(context.Background(), tx)
		return err
	}); err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.ID != a.ID {
		t.Fatalf("scoped to %s and read %s", a.ID, got.ID)
	}
	if got.ID == b.ID {
		t.Fatal("read the other organization")
	}
}

// --- listing --------------------------------------------------------------------------

// The list spans organizations, which is why it needs a function of its own —
// and this is the test that would have caught the empty-result trap.
func TestListReturnsEveryLiveOrganization(t *testing.T) {
	f := setup(t)
	for _, name := range []string{"One", "Two", "Three"} {
		f.create(t, name, nil, "")
	}

	got, err := f.store.List(context.Background(), f.db, management.Cursor{}, 20)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("listed %d organizations, want 3 — instance scope sees no rows under `id = current_org_id()`",
			len(got))
	}
}

// The extra row is fetched so Paginate can tell there is another page.
func TestListFetchesOneMoreThanThepage(t *testing.T) {
	f := setup(t)
	for i := range 5 {
		f.create(t, "Org "+string(rune('A'+i)), nil, "")
	}

	got, err := f.store.List(context.Background(), f.db, management.Cursor{}, 3)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 4 {
		t.Errorf("List(size 3) returned %d rows, want 4 — Paginate needs the lookahead", len(got))
	}
}

// Walking the pages returns every organization exactly once.
func TestWalkingTheListSeesEveryOrganizationOnce(t *testing.T) {
	f := setup(t)
	const total = 25
	for i := range total {
		f.create(t, "Org "+string(rune('A'+i%26))+string(rune('0'+i/26)), nil, "")
	}

	seen := map[string]int{}
	cursor := management.Cursor{}

	for range 20 {
		rows, err := f.store.List(context.Background(), f.db, cursor, 4)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		page, err := management.Paginate(rows, 4, Position)
		if err != nil {
			t.Fatalf("Paginate: %v", err)
		}
		for _, o := range page.Items {
			seen[o.ID]++
		}
		if page.NextPageToken == "" {
			break
		}
		if cursor, err = management.DecodeCursor(page.NextPageToken); err != nil {
			t.Fatalf("DecodeCursor: %v", err)
		}
	}

	if len(seen) != total {
		t.Errorf("saw %d of %d organizations", len(seen), total)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("%s appeared %d times", id, n)
		}
	}
}

// --- updating -------------------------------------------------------------------------

// **A settings update merges.** Read-modify-write in Go would lose a
// concurrent change to a different key, which for a policy document means a
// control somebody turned on quietly turning itself off again.
func TestUpdatingOneSettingLeavesTheOthersAlone(t *testing.T) {
	f := setup(t)
	o := f.create(t, "Merge", nil, `{"mfa_required": true, "session_lifetime_hours": 8}`)

	var updated Organization
	if err := f.in(t, o.ID, func(tx *postgres.Tx) error {
		var err error
		updated, err = f.store.Update(context.Background(), tx,
			Changes{Settings: json.RawMessage(`{"session_lifetime_hours": 4}`)})
		return err
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(updated.Settings, &settings); err != nil {
		t.Fatalf("settings: %s", updated.Settings)
	}
	if settings["session_lifetime_hours"] != float64(4) {
		t.Errorf("session_lifetime_hours = %v, want 4", settings["session_lifetime_hours"])
	}
	if settings["mfa_required"] != true {
		t.Errorf("mfa_required = %v — an unmentioned setting was lost", settings["mfa_required"])
	}
}

// A change to one field leaves the rest as they were.
func TestUpdatingTheNameLeavesEverythingElse(t *testing.T) {
	f := setup(t)
	o := f.create(t, "Before", ptr("keep.example"), `{"mfa_required": true}`)

	var updated Organization
	if err := f.in(t, o.ID, func(tx *postgres.Tx) error {
		var err error
		updated, err = f.store.Update(context.Background(), tx, Changes{Name: ptr("After")})
		return err
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if updated.Name != "After" {
		t.Errorf("name = %q", updated.Name)
	}
	if updated.Domain == nil || *updated.Domain != "keep.example" {
		t.Errorf("the domain changed: %v", updated.Domain)
	}
	if updated.Status != StatusActive {
		t.Errorf("the status changed: %q", updated.Status)
	}
}

// Clearing a domain is distinguishable from not mentioning it — the reason
// Changes.Domain is a pointer to a pointer.
func TestADomainCanBeClearedAndNotMentioningItLeavesIt(t *testing.T) {
	f := setup(t)
	o := f.create(t, "Domain", ptr("clear.example"), "")

	// Not mentioned.
	var kept Organization
	if err := f.in(t, o.ID, func(tx *postgres.Tx) error {
		var err error
		kept, err = f.store.Update(context.Background(), tx, Changes{Name: ptr("Renamed")})
		return err
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if kept.Domain == nil {
		t.Fatal("an unmentioned domain was cleared")
	}

	// Explicitly cleared.
	var cleared Organization
	var none *string
	if err := f.in(t, o.ID, func(tx *postgres.Tx) error {
		var err error
		cleared, err = f.store.Update(context.Background(), tx, Changes{Domain: &none})
		return err
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if cleared.Domain != nil {
		t.Errorf("the domain was not cleared: %v", cleared.Domain)
	}
}

// An update cannot reach another organization: the transaction is scoped to
// one row, so the UPDATE's own predicate is not what confines it.
func TestAnUpdateCannotReachAnotherOrganization(t *testing.T) {
	f := setup(t)
	a := f.create(t, "Alpha", nil, "")
	b := f.create(t, "Beta", nil, "")

	if err := f.in(t, a.ID, func(tx *postgres.Tx) error {
		_, err := f.store.Update(context.Background(), tx, Changes{Name: ptr("Renamed")})
		return err
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	var name string
	f.factory.QueryRow(&name, `SELECT name FROM organizations WHERE id = $1`, b.ID)
	if name != "Beta" {
		t.Errorf("the other organization was renamed to %q", name)
	}
}

// --- deleting -------------------------------------------------------------------------

// A deleted organization is invisible, including to the scope that owns it.
func TestADeletedOrganizationIsNotFound(t *testing.T) {
	f := setup(t)
	o := f.create(t, "Doomed", nil, "")

	if err := f.in(t, o.ID, func(tx *postgres.Tx) error {
		return f.store.SoftDelete(context.Background(), tx, time.Now())
	}); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	err := f.in(t, o.ID, func(tx *postgres.Tx) error {
		_, err := f.store.Get(context.Background(), tx)
		return err
	})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after delete = %v, want ErrNotFound", err)
	}
}

// And gone from the list.
func TestADeletedOrganizationIsNotListed(t *testing.T) {
	f := setup(t)
	live := f.create(t, "Live", nil, "")
	doomed := f.create(t, "Doomed", nil, "")

	if err := f.in(t, doomed.ID, func(tx *postgres.Tx) error {
		return f.store.SoftDelete(context.Background(), tx, time.Now())
	}); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	got, err := f.store.List(context.Background(), f.db, management.Cursor{}, 20)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ID != live.ID {
		t.Errorf("the list returned %d organizations, want only the live one", len(got))
	}
}

// **Deleting releases the domain, so it can be claimed again.**
//
// The domain routes login traffic to a tenant. Left claimed by a deleted
// organization it could never be reused — and reusing it is the ordinary case:
// a customer leaves and comes back, or a domain changes hands.
func TestDeletingReleasesTheDomain(t *testing.T) {
	f := setup(t)
	doomed := f.create(t, "Doomed", ptr("shared.example"), "")

	if err := f.in(t, doomed.ID, func(tx *postgres.Tx) error {
		return f.store.SoftDelete(context.Background(), tx, time.Now())
	}); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	if _, err := f.store.Create(context.Background(), f.db,
		NewOrganization{Name: "Successor", Domain: ptr("shared.example")}); err != nil {
		t.Fatalf("the domain of a deleted organization is still claimed: %v", err)
	}
}

// **The card's Definition of Done: deleting an organization never destroys its
// audit history.**
//
// `events` has no foreign key on org_id, deliberately (P0-12), so this is
// asserted rather than assumed — and asserted by reading the rows back after
// the delete, not by inspecting the schema.
func TestDeletingAnOrganizationKeepsItsAuditHistory(t *testing.T) {
	f := setup(t)
	o := f.create(t, "Doomed", nil, "")

	f.factory.Exec(
		`INSERT INTO events (org_id, event_type, payload) VALUES ($1, $2, '{}'::jsonb)`,
		o.ID, "organization.created")
	f.factory.Exec(
		`INSERT INTO events (org_id, event_type, payload) VALUES ($1, $2, '{}'::jsonb)`,
		o.ID, "user.login.success")

	var before int
	f.factory.QueryRow(&before, `SELECT count(*) FROM events WHERE org_id = $1`, o.ID)
	if before != 2 {
		t.Fatalf("%d events before the delete, want 2 — the test would prove nothing", before)
	}

	if err := f.in(t, o.ID, func(tx *postgres.Tx) error {
		return f.store.SoftDelete(context.Background(), tx, time.Now())
	}); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	var after int
	f.factory.QueryRow(&after, `SELECT count(*) FROM events WHERE org_id = $1`, o.ID)
	if after != before {
		t.Errorf("%d events survived the delete, want %d", after, before)
	}
}

// Deleting twice is not an error the second time being silently different: the
// second call reports not-found, which the handler answers as 404.
func TestDeletingTwiceReportsNotFound(t *testing.T) {
	f := setup(t)
	o := f.create(t, "Doomed", nil, "")

	if err := f.in(t, o.ID, func(tx *postgres.Tx) error {
		return f.store.SoftDelete(context.Background(), tx, time.Now())
	}); err != nil {
		t.Fatalf("the first delete: %v", err)
	}

	err := f.in(t, o.ID, func(tx *postgres.Tx) error {
		return f.store.SoftDelete(context.Background(), tx, time.Now())
	})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("the second delete = %v, want ErrNotFound", err)
	}
}

// The database refuses a deleted organization that still holds a domain.
//
// SoftDelete clears it, so this constraint is the braces to that belt — and
// nothing was testing it. A mutation that deleted the CHECK left every other
// test passing, because the application happens to do the right thing. The
// constraint is what makes it a RULE rather than a habit, and a rule nothing
// asserts is a comment.
//
// Attempted as the OWNER and by direct SQL, deliberately: the point is what the
// database refuses, not what the store declines to ask for.
func TestTheDatabaseRefusesADeletedOrganizationWithADomain(t *testing.T) {
	f := setup(t)
	o := f.create(t, "Doomed", ptr("held.example"), "")

	err := f.db.WithTenant(context.Background(), o.ID, func(tx *postgres.Tx) error {
		_, err := tx.Exec(context.Background(),
			`UPDATE organizations SET deleted_at = now()`)
		return err
	})
	if err == nil {
		t.Fatal("a deleted organization kept its domain, which can then never be reused")
	}
	if !strings.Contains(err.Error(), "organizations_deleted_has_no_domain") {
		t.Errorf("refused by %v, not by the constraint", err)
	}

	// The control: clearing the domain in the same statement is accepted, so
	// the constraint refuses the combination rather than deletion itself.
	if err := f.db.WithTenant(context.Background(), o.ID, func(tx *postgres.Tx) error {
		_, err := tx.Exec(context.Background(),
			`UPDATE organizations SET deleted_at = now(), domain = NULL`)
		return err
	}); err != nil {
		t.Errorf("deleting with the domain cleared was refused: %v", err)
	}
}

// --- what the definer functions cannot do -------------------------------------------------

// Neither SECURITY DEFINER function takes an organization id, so neither is a
// way to reach a specific tenant's row by guessing one. This asserts the
// signature rather than trusting the comment: a future edit that adds an id
// parameter breaks it.
func TestTheDefinerFunctionsTakeNoOrganizationId(t *testing.T) {
	f := setup(t)

	for _, fn := range []string{"organizations_page", "organization_create"} {
		var args string
		f.factory.QueryRow(&args,
			`SELECT pg_get_function_arguments(p.oid)
			   FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
			  WHERE p.proname = $1 AND n.nspname = 'public'`, fn)

		if strings.Contains(args, "org_id") || strings.Contains(args, "organization_id") {
			t.Errorf("%s takes an organization id: %s", fn, args)
		}
	}
}

// Both are SECURITY DEFINER with a pinned search_path. Without the pin, a
// caller who can create objects in a schema earlier on the path could shadow a
// table the function reads — which is the specific hazard P0-12 documented.
func TestTheDefinerFunctionsPinTheirSearchPath(t *testing.T) {
	f := setup(t)

	for _, fn := range []string{"organizations_page", "organization_create"} {
		var config string
		f.factory.QueryRow(&config,
			`SELECT COALESCE(array_to_string(p.proconfig, ','), '')
			   FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
			  WHERE p.proname = $1 AND n.nspname = 'public'`, fn)

		if !strings.Contains(config, "search_path=") {
			t.Errorf("%s does not pin its search_path: %q", fn, config)
		}
	}
}
