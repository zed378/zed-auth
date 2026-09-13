//go:build integration

// The history and the audit trail against a real database (P3-08).
//
// Through the service's own role, not the owner's: `sessions` and `events` are
// behind row-level security, and a history query that worked only as the owner
// would read nothing in production — every login would look like a first login,
// and nothing would ever be detected.
package anomaly

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

func TestMain(m *testing.M) { os.Exit(testsupport.RunTests(m)) }

type historyFixture struct {
	db      *postgres.DB
	factory *testsupport.Factory
	orgID   string
	userID  string
}

func historySetup(t *testing.T) *historyFixture {
	t.Helper()

	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	orgID := factory.Organization(factory.Instance())

	db, err := postgres.Open(context.Background(), config.PostgresConfig{
		DSN: stack.AppDSN, MaxOpenConns: 4, MaxIdleConns: 2, ConnMaxLifetime: time.Minute,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("opening the application pool: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return &historyFixture{db: db, factory: factory, orgID: orgID, userID: factory.User(orgID)}
}

// session writes one login as the owner and returns its id.
func (f *historyFixture) session(t *testing.T, orgID, userID, ip, agent string, at time.Time, revoked bool) string {
	t.Helper()

	var id string
	f.factory.QueryRow(&id, `
		INSERT INTO sessions (user_id, org_id, auth_methods, ip, user_agent, token_hash,
		                      created_at, last_seen_at, expires_at, revoked_at)
		VALUES ($1, $2, '{pwd}', $3::inet, $4, md5(random()::text), $5, $5, $5::timestamptz + interval '12 hours',
		        CASE WHEN $6 THEN $5::timestamptz END)
		RETURNING id`, userID, orgID, ip, agent, at, revoked)
	return id
}

func TestHistoryIsMostRecentFirstExcludingTheCurrentSession(t *testing.T) {
	f := historySetup(t)
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	f.session(t, f.orgID, f.userID, "198.51.100.1", "agent-oldest", base, false)
	f.session(t, f.orgID, f.userID, "198.51.100.2", "agent-revoked", base.Add(time.Hour), true)
	f.session(t, f.orgID, f.userID, "198.51.100.3", "agent-newest", base.Add(2*time.Hour), false)
	current := f.session(t, f.orgID, f.userID, "198.51.100.4", "agent-current", base.Add(3*time.Hour), false)

	past, err := (&PostgresHistory{DB: f.db}).Recent(context.Background(), f.orgID, f.userID, current, 20)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}

	var agents []string
	for _, p := range past {
		agents = append(agents, p.UserAgent)
	}
	got := strings.Join(agents, ",")

	// The revoked one is included: "sign out everywhere" must not make every
	// device new again.
	if want := "agent-newest,agent-revoked,agent-oldest"; got != want {
		t.Fatalf("history = %s, want %s", got, want)
	}
	if past[0].IP != "198.51.100.3" {
		t.Errorf("IP = %q, want the bare address with no prefix length", past[0].IP)
	}
	if !past[0].At.Equal(base.Add(2 * time.Hour)) {
		t.Errorf("At = %v, want %v", past[0].At, base.Add(2*time.Hour))
	}
}

func TestHistoryIsBoundedByTheLimit(t *testing.T) {
	f := historySetup(t)
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		f.session(t, f.orgID, f.userID, "198.51.100.1", "agent", base.Add(time.Duration(i)*time.Minute), false)
	}

	past, err := (&PostgresHistory{DB: f.db}).Recent(context.Background(), f.orgID, f.userID, "", 3)
	if err != nil {
		t.Fatalf("Recent with no exclusion: %v", err)
	}
	if len(past) != 3 {
		t.Errorf("returned %d logins with a limit of 3", len(past))
	}
}

// Another user's logins are not this user's history, and another tenant's are
// not visible at all.
func TestHistoryIsOneUsersOwnWithinOneTenant(t *testing.T) {
	f := historySetup(t)
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	colleague := f.factory.User(f.orgID, "colleague@example.test")
	f.session(t, f.orgID, colleague, "198.51.100.9", "colleague-agent", base, false)

	otherOrg := f.factory.Organization(f.factory.Instance("second"), "Other")
	otherUser := f.factory.User(otherOrg, "other@example.test")
	f.session(t, otherOrg, otherUser, "198.51.100.8", "other-tenant-agent", base, false)

	past, err := (&PostgresHistory{DB: f.db}).Recent(context.Background(), f.orgID, f.userID, "", 20)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(past) != 0 {
		t.Errorf("a user with no logins has %d in their history", len(past))
	}

	// The other tenant's user, read with THIS tenant's scope: RLS must hide it.
	leaked, err := (&PostgresHistory{DB: f.db}).Recent(context.Background(), f.orgID, otherUser, "", 20)
	if err != nil {
		t.Fatalf("Recent across tenants: %v", err)
	}
	if len(leaked) != 0 {
		t.Errorf("read %d sessions belonging to another organization", len(leaked))
	}

	// The control: under its own tenant, that user's history is visible — so
	// the empty answer above is RLS, not a broken query.
	own, err := (&PostgresHistory{DB: f.db}).Recent(context.Background(), otherOrg, otherUser, "", 20)
	if err != nil || len(own) != 1 {
		t.Fatalf("the other tenant's own history read %d rows (err %v); the isolation check proves nothing", len(own), err)
	}
}

func TestTheAuditRowCarriesSignalsAndPlaceButNoAddress(t *testing.T) {
	f := historySetup(t)

	recorder := &AuditRecorder{DB: f.db, Audit: audit.NewWriter(f.db, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)}
	err := recorder.RecordAnomaly(context.Background(), Finding{
		OrgID: f.orgID, UserID: f.userID, SessionID: "11111111-1111-1111-1111-111111111111",
		At: time.Now(), Signals: []Signal{SignalNewLocation, SignalImpossibleTravel}, Where: "London, GB",
	})
	if err != nil {
		t.Fatalf("RecordAnomaly: %v", err)
	}

	var raw string
	f.factory.QueryRow(&raw, `
		SELECT json_build_object('actor', actor_user_id, 'payload', payload, 'ip', host(ip))::text
		  FROM events WHERE event_type = $1`, string(audit.EventLoginAnomaly))

	var row struct {
		Actor   string         `json:"actor"`
		Payload map[string]any `json:"payload"`
		IP      *string        `json:"ip"`
	}
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		t.Fatalf("decoding the event: %v (%s)", err, raw)
	}

	if row.Actor != f.userID {
		t.Errorf("actor = %q, want the user", row.Actor)
	}
	if row.IP != nil {
		t.Errorf("the anomaly event stores an IP (%s) beside a location", *row.IP)
	}
	if row.Payload["location"] != "London, GB" {
		t.Errorf("location = %v", row.Payload["location"])
	}
	signals, _ := row.Payload["signals"].([]any)
	if len(signals) != 2 || signals[0] != "new_location" || signals[1] != "impossible_travel" {
		t.Errorf("signals = %v", row.Payload["signals"])
	}
}

// An unknown place is left out rather than written as an empty string.
func TestAnUnknownPlaceIsOmitted(t *testing.T) {
	f := historySetup(t)

	recorder := &AuditRecorder{DB: f.db, Audit: audit.NewWriter(f.db, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)}
	if err := recorder.RecordAnomaly(context.Background(), Finding{
		OrgID: f.orgID, UserID: f.userID, SessionID: "11111111-1111-1111-1111-111111111111",
		At: time.Now(), Signals: []Signal{SignalNewDevice},
	}); err != nil {
		t.Fatalf("RecordAnomaly: %v", err)
	}

	var hasLocation bool
	f.factory.QueryRow(&hasLocation,
		`SELECT payload ? 'location' FROM events WHERE event_type = $1`, string(audit.EventLoginAnomaly))
	if hasLocation {
		t.Error("a finding with no location wrote a location key")
	}
}
