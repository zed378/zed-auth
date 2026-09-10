//go:build integration

// The organization endpoints end to end (P1-16).
//
// Through the real generated router, the real /v1 chain, real manager_roles and
// a real signing key — so a permission boundary asserted here is the one a
// caller meets, not one a test constructed.
package organization

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/application"
	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/httpserver"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/oauth/token"
	"github.com/zed378/zed-auth/backend/internal/project"
	"github.com/zed378/zed-auth/backend/internal/ratelimit"
	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

const issuer = "https://auth.example.test"

type endpoints struct {
	fixture

	handler  http.Handler
	signer   *signing.Signer
	instance string
	userID   string
	clientID string
	homeOrg  string
}

func setupEndpoints(t *testing.T) *endpoints {
	t.Helper()

	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	instance := factory.Instance()

	db := openApp(t, stack)

	// The caller's own organization, created directly so the endpoints under
	// test are not also the fixture.
	homeOrg := factory.Organization(instance)
	userID := factory.User(homeOrg)

	var projectID, clientID string
	factory.QueryRow(&projectID,
		`INSERT INTO projects (org_id, name) VALUES ($1, 'console') RETURNING id`, homeOrg)
	factory.QueryRow(&clientID,
		`INSERT INTO applications (project_id, org_id, name, type)
		 VALUES ($1, $2, 'console', 'web') RETURNING id`, projectID, homeOrg)

	keys := signingKeys(t, stack)
	rdb := redis.NewClient(&redis.Options{Addr: stack.RedisAddr})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.FlushAll(context.Background()).Err(); err != nil {
		t.Fatalf("flushing redis: %v", err)
	}

	auditor := audit.NewWriter(db, discard(), nil)

	chain := &management.Chain{
		Auth: &management.Middleware{
			Issuer: issuer, Verifier: signing.NewVerifier(keys),
			Grants: management.NewRoleStore(), DB: db, Log: discard(),
		},
		RateLimit: &management.RateLimit{
			Counter: ratelimit.NewQuotas(rdb, nil, discard()).
				WithQuota(ratelimit.Quota{Limit: 200, Window: time.Minute}),
		},
		Idempotency: &management.Idempotency{Claims: management.NewDBClaims(db), Log: discard()},
		Audit:       &management.AuditGuard{Log: discard()},
		BufferBody:  true,
	}

	srv := httpserver.New(config.HTTPConfig{
		Addr: "127.0.0.1:0", ReadTimeout: time.Second * 5, WriteTimeout: time.Second * 5,
		ReadHeaderTimeout: time.Second * 2, IdleTimeout: time.Second * 5,
	}, httpserver.Deps{
		Logger: discard(),
		Health: &httpserver.Health{},
		V1:     chain,
		Organizations: &Handler{
			Store: NewStore(), DB: db, Audit: auditor, Log: discard(),
		},
		// httpserver.New refuses to build a /v1 chain with any half of the
		// Management API missing (P1-17), so this harness carries the project
		// handler even though nothing here calls it. The refusal is right —
		// a nil handler behind a registered route is a panic on the first
		// request — and the cost is that every endpoint package's harness now
		// has to name every other one.
		ProjectAPI: &project.Handler{
			Store: project.NewStore(), DB: db, Audit: auditor, Log: discard(),
		},
		// Third of three, for the reason the comment above gives.
		ApplicationAPI: application.New(db, auditor, discard()),
	})

	return &endpoints{
		fixture:  fixture{db: db, store: NewStore(), factory: factory},
		handler:  srv.Handler(),
		signer:   signing.NewSigner(keys),
		instance: instance,
		userID:   userID,
		clientID: clientID,
		homeOrg:  homeOrg,
	}
}

// --- fixtures -------------------------------------------------------------------------

func signingKeys(t *testing.T, stack *testsupport.Stack) *signing.Cache {
	t.Helper()

	pair, err := signing.Generate(signing.RS256)
	if err != nil {
		t.Fatalf("generating a signing key: %v", err)
	}
	path := filepath.Join(t.TempDir(), pair.KID+".pem")
	if err := os.WriteFile(path, []byte(pair.PrivatePEM), 0o400); err != nil {
		t.Fatalf("writing the key: %v", err)
	}

	owner, err := sql.Open("pgx", stack.OwnerDSN)
	if err != nil {
		t.Fatalf("owner connection: %v", err)
	}
	t.Cleanup(func() { _ = owner.Close() })

	store := signing.NewStore(owner, config.NewSecretResolver(false), signing.PurposeOIDC)
	if err := store.Insert(context.Background(), pair, "file:"+path); err != nil {
		t.Fatalf("inserting the key: %v", err)
	}
	if _, err := store.Rotate(context.Background()); err != nil {
		t.Fatalf("rotating: %v", err)
	}

	return signing.NewCache(func() (*signing.KeySet, error) {
		return store.Load(context.Background())
	}, time.Minute)
}

func (e *endpoints) token(t *testing.T) string {
	t.Helper()

	claims, err := token.AccessTokenClaims(token.Subject{
		Issuer: issuer, Audience: issuer, ClientID: e.clientID,
		OrgID: e.homeOrg, UserID: e.userID, Scope: []string{"openid"},
	}, time.Now())
	if err != nil {
		t.Fatalf("claims: %v", err)
	}
	payload, err := claims.Encode()
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	signed, err := e.signer.SignWithType(payload, signing.TypeAccessToken)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	return signed
}

func (e *endpoints) grant(role management.Role, scopeID string) {
	e.factory.Exec(`INSERT INTO manager_roles (user_id, role, scope_id) VALUES ($1, $2, $3)`,
		e.userID, string(role), scopeID)
}

func (e *endpoints) call(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()

	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}

	r := httptest.NewRequest(method, target, reader)
	r.Header.Set("Authorization", "Bearer "+e.token(t))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}

	w := httptest.NewRecorder()
	e.handler.ServeHTTP(w, r)
	return w
}

func decodeOrg(t *testing.T, w *httptest.ResponseRecorder) api.Organization {
	t.Helper()

	var o api.Organization
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatalf("the response is not an Organization: %s", w.Body.String())
	}
	return o
}

func envelopeCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()

	var e struct {
		Error struct {
			Code    string            `json:"code"`
			Message string            `json:"message"`
			Details []api.ErrorDetail `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("the response is not docs/PLAN/05's envelope: %s", w.Body.String())
	}
	return e.Error.Code
}

// --- permissions -------------------------------------------------------------------------

// **DoD 1: every operation enforces INSTANCE_OWNER where the hierarchy requires
// it.**
//
// The table is the permission surface, checked against a caller who holds
// ORG_OWNER over their own organization — the most powerful role that is still
// not an instance owner.
func TestOnlyAnInstanceOwnerMayCreateListOrDelete(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.OrgOwner, e.homeOrg)

	cases := []struct {
		name, method, target, body string
		want                       int
	}{
		{"list", http.MethodGet, "/v1/organizations", "", http.StatusForbidden},
		{"create", http.MethodPost, "/v1/organizations", `{"name":"New"}`, http.StatusForbidden},
		{"read own", http.MethodGet, "/v1/organizations/" + e.homeOrg, "", http.StatusOK},
		{"rename own", http.MethodPatch, "/v1/organizations/" + e.homeOrg, `{"name":"Renamed"}`, http.StatusOK},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := e.call(t, c.method, c.target, c.body)
			if w.Code != c.want {
				t.Errorf("status = %d, want %d: %s", w.Code, c.want, w.Body.String())
			}
		})
	}

	// Delete is instance-scoped, so an ORG_OWNER gets 403 rather than 404 —
	// they can see their own organization perfectly well.
	w := e.call(t, http.MethodDelete,
		"/v1/organizations/"+e.homeOrg+"?confirm_name="+url.QueryEscape("Renamed"), "")
	if w.Code != http.StatusForbidden {
		t.Errorf("delete = %d, want 403: %s", w.Code, w.Body.String())
	}
}

// **Abuse case A-2: an ORG_OWNER cannot suspend their own organization.**
//
// It locks out every user in it, and an organization owner doing that to
// themselves — or to an investigation — is not a capability anybody asked for.
// The route requires ORG_OWNER, so this is a FIELD-level check inside the
// handler, and it is the one that route-level authorization cannot express.
func TestAnOrgOwnerCannotChangeStatus(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.OrgOwner, e.homeOrg)

	// The control: the same caller CAN change the name through the same
	// endpoint, so the refusal is about the field rather than the route.
	if w := e.call(t, http.MethodPatch, "/v1/organizations/"+e.homeOrg, `{"name":"Fine"}`); w.Code != http.StatusOK {
		t.Fatalf("renaming = %d, want 200: %s", w.Code, w.Body.String())
	}

	w := e.call(t, http.MethodPatch, "/v1/organizations/"+e.homeOrg, `{"status":"suspended"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("suspending = %d, want 403: %s", w.Code, w.Body.String())
	}
	if code := envelopeCode(t, w); code != "PERMISSION_DENIED" {
		t.Errorf("code = %q", code)
	}

	// And nothing changed.
	var status string
	e.factory.QueryRow(&status, `SELECT status FROM organizations WHERE id = $1`, e.homeOrg)
	if status != StatusActive {
		t.Errorf("the organization is %q despite the refusal", status)
	}
}

// An INSTANCE_OWNER may.
func TestAnInstanceOwnerMayChangeStatus(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.InstanceOwner, e.instance)

	w := e.call(t, http.MethodPatch, "/v1/organizations/"+e.homeOrg, `{"status":"suspended"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if got := decodeOrg(t, w); string(got.Status) != StatusSuspended {
		t.Errorf("status = %q", got.Status)
	}
}

// **Abuse case A-3: another organization answers 404, not 403.**
func TestAnotherOrganizationIsNotFound(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.OrgOwner, e.homeOrg)
	other := e.factory.Organization(e.instance)

	w := e.call(t, http.MethodGet, "/v1/organizations/"+other, "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
	}
	if code := envelopeCode(t, w); code != "NOT_FOUND" {
		t.Errorf("code = %q", code)
	}
}

// --- creating ---------------------------------------------------------------------------

func TestAnInstanceOwnerCreatesAnOrganization(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.InstanceOwner, e.instance)

	w := e.call(t, http.MethodPost, "/v1/organizations",
		`{"name":"Acme Corp","domain":"acme.example"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	created := decodeOrg(t, w)
	if created.Name != "Acme Corp" {
		t.Errorf("name = %q", created.Name)
	}
	if created.Id.String() == "" || created.Id.String() == "00000000-0000-0000-0000-000000000000" {
		t.Errorf("id = %q", created.Id)
	}

	// **DoD 4: the lifecycle event is written.**
	var events int
	e.factory.QueryRow(&events, `SELECT count(*) FROM events WHERE event_type = $1`,
		string(audit.EventOrganizationCreated))
	if events != 1 {
		t.Errorf("%d organization.created events, want 1", events)
	}
}

// **Abuse case A-4: mass assignment.**
//
// `id`, `status` and `created_at` are server-set. The generated request type
// has no field for them, so a body carrying them has nothing to land on — this
// asserts that rather than trusting it.
func TestMassAssignmentHasNowhereToLand(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.InstanceOwner, e.instance)

	forged := `{
		"name": "Acme",
		"id": "00000000-0000-0000-0000-0000000000ff",
		"status": "suspended",
		"created_at": "1999-01-01T00:00:00Z",
		"instance_id": "00000000-0000-0000-0000-0000000000ee"
	}`
	w := e.call(t, http.MethodPost, "/v1/organizations", forged)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}

	created := decodeOrg(t, w)
	if created.Id.String() == "00000000-0000-0000-0000-0000000000ff" {
		t.Error("the request body set the id")
	}
	if string(created.Status) != StatusActive {
		t.Errorf("the request body set the status to %q", created.Status)
	}
	if created.CreatedAt.Year() == 1999 {
		t.Error("the request body set created_at")
	}
}

// **DoD 2: invalid settings are rejected with per-field errors.**
func TestInvalidSettingsAreRejectedPerField(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.InstanceOwner, e.instance)

	w := e.call(t, http.MethodPost, "/v1/organizations", `{
		"name": "Bad",
		"settings": {"mfa_requried": true, "session_lifetime_hours": 0}
	}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}

	var envelope struct {
		Error struct {
			Code    string            `json:"code"`
			Details []api.ErrorDetail `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("not the envelope: %s", w.Body.String())
	}
	if envelope.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q", envelope.Error.Code)
	}

	fields := map[string]bool{}
	for _, d := range envelope.Error.Details {
		fields[d.Field] = true
	}
	for _, want := range []string{"settings.mfa_requried", "settings.session_lifetime_hours"} {
		if !fields[want] {
			t.Errorf("no detail for %q; got %v", want, fields)
		}
	}

	// And nothing was created.
	var count int
	e.factory.QueryRow(&count, `SELECT count(*) FROM organizations WHERE name = 'Bad'`)
	if count != 0 {
		t.Error("an organization was created despite the invalid settings")
	}
}

// **The unknown-key check needs the RAW body**, because the generated struct
// silently drops what it does not recognise. This is the test that fails if
// BufferBody is removed from the chain.
func TestAMisspelledSettingIsRefusedRatherThanDropped(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.InstanceOwner, e.instance)

	w := e.call(t, http.MethodPost, "/v1/organizations",
		`{"name":"Typo","settings":{"mfa_requried":true}}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("a misspelled setting was accepted (%d): %s — the caller would believe MFA was on",
			w.Code, w.Body.String())
	}
}

// --- updating -----------------------------------------------------------------------------

// A PATCH naming one setting leaves the others alone, end to end.
func TestAPatchMergesSettings(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.OrgOwner, e.homeOrg)

	w := e.call(t, http.MethodPatch, "/v1/organizations/"+e.homeOrg,
		`{"settings":{"session_lifetime_hours":4}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}

	got := decodeOrg(t, w)
	if got.Settings.SessionLifetimeHours == nil || *got.Settings.SessionLifetimeHours != 4 {
		t.Errorf("session_lifetime_hours = %v", got.Settings.SessionLifetimeHours)
	}
	if got.Settings.PasswordPolicy == nil {
		t.Error("the password policy was lost by a PATCH that did not mention it")
	}
}

// **Abuse case A-6: a domain cannot be stolen, and the refusal names nobody.**
func TestADuplicateDomainIsRefusedWithoutNamingTheHolder(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.InstanceOwner, e.instance)

	if w := e.call(t, http.MethodPost, "/v1/organizations",
		`{"name":"Holder","domain":"taken.example"}`); w.Code != http.StatusCreated {
		t.Fatalf("the first create = %d: %s", w.Code, w.Body.String())
	}

	w := e.call(t, http.MethodPost, "/v1/organizations",
		`{"name":"Thief","domain":"taken.example"}`)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "Holder") {
		t.Errorf("the refusal names the holder: %s", w.Body.String())
	}
}

// --- deleting -----------------------------------------------------------------------------

// **The confirmation is enforced by the server.** A UI affordance is not a
// control: a script that deletes the wrong organization never goes near the
// console.
func TestDeleteWithoutAMatchingConfirmationChangesNothing(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.InstanceOwner, e.instance)

	created := decodeOrg(t, e.call(t, http.MethodPost, "/v1/organizations", `{"name":"Doomed"}`))
	target := "/v1/organizations/" + created.Id.String()

	for name, confirm := range map[string]string{
		"the wrong name": "Something Else",
		"empty":          "",
		"wrong case":     "doomed",
	} {
		t.Run(name, func(t *testing.T) {
			w := e.call(t, http.MethodDelete, target+"?confirm_name="+url.QueryEscape(confirm), "")
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400: %s", w.Code, w.Body.String())
			}
		})
	}

	var deleted sql.NullTime
	e.factory.QueryRow(&deleted, `SELECT deleted_at FROM organizations WHERE id = $1`, created.Id.String())
	if deleted.Valid {
		t.Error("the organization was deleted despite the confirmation not matching")
	}
}

// **DoD 3: deleting an organization never destroys its audit history.**
//
// Asserted end to end and from both ends: the events are there before, and they
// are there after — with the deletion's own event added.
func TestDeletingKeepsTheAuditHistory(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.InstanceOwner, e.instance)

	created := decodeOrg(t, e.call(t, http.MethodPost, "/v1/organizations", `{"name":"Doomed"}`))
	id := created.Id.String()

	e.factory.Exec(`INSERT INTO events (org_id, event_type, payload) VALUES ($1, 'user.login.success', '{}')`, id)

	var before int
	e.factory.QueryRow(&before, `SELECT count(*) FROM events WHERE org_id = $1`, id)
	if before < 2 {
		t.Fatalf("%d events before the delete; the test would prove nothing", before)
	}

	w := e.call(t, http.MethodDelete,
		"/v1/organizations/"+id+"?confirm_name="+url.QueryEscape("Doomed"), "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete = %d: %s", w.Code, w.Body.String())
	}

	var after int
	e.factory.QueryRow(&after, `SELECT count(*) FROM events WHERE org_id = $1`, id)
	if after < before {
		t.Errorf("%d events survived, want at least %d", after, before)
	}

	var deletedEvents int
	e.factory.QueryRow(&deletedEvents, `SELECT count(*) FROM events WHERE org_id = $1 AND event_type = $2`,
		id, string(audit.EventOrganizationDeleted))
	if deletedEvents != 1 {
		t.Errorf("%d organization.deleted events, want 1", deletedEvents)
	}
}

// A deleted organization is 404 afterwards, including to the INSTANCE_OWNER who
// deleted it.
func TestADeletedOrganizationIsGone(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.InstanceOwner, e.instance)

	created := decodeOrg(t, e.call(t, http.MethodPost, "/v1/organizations", `{"name":"Doomed"}`))
	id := created.Id.String()

	if w := e.call(t, http.MethodDelete,
		"/v1/organizations/"+id+"?confirm_name=Doomed", ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete = %d: %s", w.Code, w.Body.String())
	}

	if w := e.call(t, http.MethodGet, "/v1/organizations/"+id, ""); w.Code != http.StatusNotFound {
		t.Errorf("reading a deleted organization = %d, want 404", w.Code)
	}
}

// --- idempotency, end to end -----------------------------------------------------------------

// **Replaying a POST creates one organization, not two.**
func TestAReplayedCreateMakesOneOrganization(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.InstanceOwner, e.instance)

	body := `{"name":"Once"}`
	send := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/v1/organizations", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+e.token(t))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "create-once")
		w := httptest.NewRecorder()
		e.handler.ServeHTTP(w, r)
		return w
	}

	first := send()
	if first.Code != http.StatusCreated {
		t.Fatalf("the first create = %d: %s", first.Code, first.Body.String())
	}
	second := send()

	if second.Code != http.StatusCreated {
		t.Fatalf("the replay = %d: %s", second.Code, second.Body.String())
	}
	if second.Body.String() != first.Body.String() {
		t.Errorf("the replay differs:\n  %s\n  %s", first.Body.String(), second.Body.String())
	}

	var count int
	e.factory.QueryRow(&count, `SELECT count(*) FROM organizations WHERE name = 'Once'`)
	if count != 1 {
		t.Errorf("%d organizations named Once, want 1", count)
	}
}

// --- listing --------------------------------------------------------------------------------

func TestAnInstanceOwnerListsOrganizations(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.InstanceOwner, e.instance)

	for i := range 3 {
		if w := e.call(t, http.MethodPost, "/v1/organizations",
			fmt.Sprintf(`{"name":"Org %d"}`, i)); w.Code != http.StatusCreated {
			t.Fatalf("create %d = %d: %s", i, w.Code, w.Body.String())
		}
	}

	w := e.call(t, http.MethodGet, "/v1/organizations?page_size=2", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}

	var list api.OrganizationList
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("not a list: %s", w.Body.String())
	}
	if len(list.Organizations) != 2 {
		t.Errorf("%d organizations on a page of 2", len(list.Organizations))
	}
	if list.PageInfo == nil || list.PageInfo.NextPageToken == nil {
		t.Fatal("no next page token, but there are four organizations")
	}

	next := e.call(t, http.MethodGet,
		"/v1/organizations?page_size=2&page_token="+url.QueryEscape(*list.PageInfo.NextPageToken), "")
	if next.Code != http.StatusOK {
		t.Fatalf("the second page = %d: %s", next.Code, next.Body.String())
	}
}

// --- the envelope ----------------------------------------------------------------------------

// **DoD 5: every error response matches docs/PLAN/05's schema.**
func TestEveryErrorUsesTheEnvelope(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.OrgOwner, e.homeOrg)
	other := e.factory.Organization(e.instance)

	cases := map[string]struct {
		method, target, body string
		want                 int
	}{
		"403": {http.MethodGet, "/v1/organizations", "", http.StatusForbidden},
		"404": {http.MethodGet, "/v1/organizations/" + other, "", http.StatusNotFound},
		"400": {http.MethodPatch, "/v1/organizations/" + e.homeOrg, `{"name":""}`, http.StatusBadRequest},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := e.call(t, c.method, c.target, c.body)

			if w.Code != c.want {
				t.Errorf("status = %d, want %d: %s", w.Code, c.want, w.Body.String())
			}
			if code := envelopeCode(t, w); code == "" {
				t.Errorf("no code in the envelope: %s", w.Body.String())
			}
			if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("Content-Type = %q", ct)
			}
			if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", cc)
			}
		})
	}
}

// A malformed organization id is rejected by the generated wrapper before
// anything runs, and still answers with the project's envelope rather than the
// wrapper's plain text.
func TestAMalformedIdAnswersTheEnvelope(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.InstanceOwner, e.instance)

	w := e.call(t, http.MethodGet, "/v1/organizations/not-a-uuid", "")

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
	if code := envelopeCode(t, w); code != "VALIDATION_ERROR" {
		t.Errorf("code = %q — the wrapper's own error handler answered", code)
	}
}
