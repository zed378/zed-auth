//go:build integration

// The idempotency store against a real PostgreSQL (P1-15, PG-21).
//
// Every property here is a DATABASE property and cannot be shown against a
// fake. The claim is a primary-key race; the isolation between clients and
// between tenants is a key shape plus RLS; the expiry is a timestamp the
// database compares. A unit test asserting these against an in-memory map
// would be asserting the map.
//
// Run with:
//
//	go test -tags=integration ./internal/management/...
package management

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// openApp connects as the RUNTIME role, not the owner.
//
// P1-14 found what happens otherwise: a tenant-scoped table read as the owner
// returns nothing under RLS, every assertion about "no rows leaked" passes, and
// the test proves nothing. The service runs as auth_app, so the test does too.
func openApp(t *testing.T, stack *testsupport.Stack) *postgres.DB {
	t.Helper()

	db, err := postgres.Open(context.Background(), config.PostgresConfig{
		DSN:             stack.AppDSN,
		MaxOpenConns:    8,
		MaxIdleConns:    4,
		ConnMaxLifetime: time.Minute,
	}, discard())
	if err != nil {
		t.Fatalf("opening the app connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

type idemFixture struct {
	db      *postgres.DB
	claims  *DBClaims
	factory *testsupport.Factory

	orgA, orgB        string
	clientA, clientA2 string
	clientB           string
}

func setupIdempotency(t *testing.T) idemFixture {
	t.Helper()

	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	instance := factory.Instance()
	orgA := factory.Organization(instance)
	orgB := factory.Organization(instance)

	app := func(orgID, name string) string {
		var projectID string
		factory.QueryRow(&projectID,
			`INSERT INTO projects (org_id, name) VALUES ($1, $2) RETURNING id`,
			orgID, name+"-project")

		var id string
		factory.QueryRow(&id,
			`INSERT INTO applications (project_id, org_id, name, type)
			 VALUES ($1, $2, $3, 'web') RETURNING id`,
			projectID, orgID, name)
		return id
	}

	return idemFixture{
		db:       openApp(t, stack),
		claims:   NewDBClaims(openApp(t, stack)),
		factory:  factory,
		orgA:     orgA,
		orgB:     orgB,
		clientA:  app(orgA, "a-one"),
		clientA2: app(orgA, "a-two"),
		clientB:  app(orgB, "b-one"),
	}
}

func now() time.Time { return time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC) }

// begin is the fixture's shorthand: the org's own client, a POST to /v1/users.
func (f idemFixture) begin(t *testing.T, org, client, key, body string) (*Replay, error) {
	t.Helper()
	return f.claims.Begin(context.Background(), org, client, key,
		"POST", "/v1/users", []byte(body), now())
}

func (f idemFixture) complete(t *testing.T, org, client, key string, status int, body string) {
	t.Helper()
	if err := f.claims.Complete(context.Background(), org, client, key, status, []byte(body)); err != nil {
		t.Fatalf("Complete: %v", err)
	}
}

// --- the three outcomes -------------------------------------------------------------

// The sequence the header exists for: claim, answer, replay.
func TestASecondIdenticalRequestReplaysAndDoesNotClaim(t *testing.T) {
	f := setupIdempotency(t)
	const key, body = "key-1", `{"email":"a@b.c"}`

	replay, err := f.begin(t, f.orgA, f.clientA, key, body)
	if err != nil {
		t.Fatalf("the first Begin: %v", err)
	}
	if replay != nil {
		t.Fatal("the first request was answered from a record that cannot exist")
	}

	f.complete(t, f.orgA, f.clientA, key, 201, `{"id":"user-1"}`)

	replay, err = f.begin(t, f.orgA, f.clientA, key, body)
	if err != nil {
		t.Fatalf("the second Begin: %v", err)
	}
	if replay == nil {
		t.Fatal("an identical retry was told to run again — the header did nothing")
	}
	if replay.Status != 201 || string(replay.Response) != `{"id":"user-1"}` {
		t.Errorf("replayed %d %q", replay.Status, replay.Response)
	}
}

// A replay returns the ORIGINAL BYTES, not an equivalent document.
//
// This is why `response` is text and not jsonb. Stored as jsonb, the body below
// comes back with its keys reordered, its whitespace collapsed and its
// duplicate dropped — still valid JSON, still "equal" to a human, and different
// to any client comparing a hash, an ETag or a signature. The first version of
// this table used jsonb and this test is what found it.
func TestAReplayReturnsTheOriginalBytes(t *testing.T) {
	f := setupIdempotency(t)
	const key = "key-1"

	// Deliberately awkward: insignificant whitespace, keys out of any natural
	// order, and a long key before a short one.
	const original = "{\n  \"zebra\": 1,\n  \"a\":    \"  spaced  \",\n  \"nested\": {\"b\":[1,2,3]}\n}"

	if _, err := f.begin(t, f.orgA, f.clientA, key, `{"a":1}`); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	f.complete(t, f.orgA, f.clientA, key, 200, original)

	replay, err := f.begin(t, f.orgA, f.clientA, key, `{"a":1}`)
	if err != nil {
		t.Fatalf("Begin again: %v", err)
	}
	if replay == nil {
		t.Fatal("no replay")
	}
	if string(replay.Response) != original {
		t.Errorf("the replay differs from what was sent:\n  sent     %q\n  replayed %q",
			original, replay.Response)
	}
}

// An empty body is a real answer — a 204 — and replays as one. Substituting
// `null` would invent content the first request never sent.
func TestAnEmptyResponseReplaysAsEmpty(t *testing.T) {
	f := setupIdempotency(t)
	const key = "key-1"

	if _, err := f.begin(t, f.orgA, f.clientA, key, `{"a":1}`); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	f.complete(t, f.orgA, f.clientA, key, 204, "")

	replay, err := f.begin(t, f.orgA, f.clientA, key, `{"a":1}`)
	if err != nil {
		t.Fatalf("Begin again: %v", err)
	}
	if replay == nil {
		t.Fatal("a 204 was not stored at all")
	}
	if replay.Status != 204 || len(replay.Response) != 0 {
		t.Errorf("replayed %d %q, want 204 with an empty body", replay.Status, replay.Response)
	}
}

// The same key with a DIFFERENT body is a conflict, not a replay. Returning the
// first answer would make the caller's second, different intention disappear
// with no way for them to notice.
func TestTheSameKeyWithADifferentRequestIsAConflict(t *testing.T) {
	f := setupIdempotency(t)
	const key = "key-1"

	if _, err := f.begin(t, f.orgA, f.clientA, key, `{"email":"a@b.c"}`); err != nil {
		t.Fatalf("the first Begin: %v", err)
	}
	f.complete(t, f.orgA, f.clientA, key, 201, `{"id":"user-1"}`)

	for name, second := range map[string]func() (*Replay, error){
		"a different body": func() (*Replay, error) {
			return f.begin(t, f.orgA, f.clientA, key, `{"email":"SOMEBODY-ELSE@b.c"}`)
		},
		"a different path": func() (*Replay, error) {
			return f.claims.Begin(context.Background(), f.orgA, f.clientA, key,
				"POST", "/v1/organizations", []byte(`{"email":"a@b.c"}`), now())
		},
		"a different method": func() (*Replay, error) {
			return f.claims.Begin(context.Background(), f.orgA, f.clientA, key,
				"PATCH", "/v1/users", []byte(`{"email":"a@b.c"}`), now())
		},
	} {
		t.Run(name, func(t *testing.T) {
			replay, err := second()
			if replay != nil {
				t.Fatal("the first request's answer was replayed to a DIFFERENT request")
			}
			var fault Fault
			if !errors.As(err, &fault) || fault.Class != Conflict {
				t.Fatalf("err = %v, want a Conflict Fault", err)
			}
		})
	}
}

// A duplicate arriving while the first is still running is refused, not queued
// and not run. Waiting would hold a connection for as long as the first takes;
// running would defeat the header entirely.
func TestADuplicateOfAnUnfinishedRequestIsRefused(t *testing.T) {
	f := setupIdempotency(t)
	const key, body = "key-1", `{"email":"a@b.c"}`

	if _, err := f.begin(t, f.orgA, f.clientA, key, body); err != nil {
		t.Fatalf("the first Begin: %v", err)
	}
	// Deliberately no Complete: the first request is still in flight.

	replay, err := f.begin(t, f.orgA, f.clientA, key, body)
	if replay != nil {
		t.Fatal("a record with no stored answer was replayed")
	}
	var fault Fault
	if !errors.As(err, &fault) || fault.Class != Conflict {
		t.Fatalf("err = %v, want a Conflict Fault", err)
	}
	if !strings.Contains(fault.Reason, "in flight") {
		t.Errorf("reason = %q, which does not distinguish this from a hash mismatch", fault.Reason)
	}
}

// --- the race -------------------------------------------------------------------------

// The property the whole design rests on: under genuine concurrency, exactly
// ONE caller gets the claim.
//
// A check-then-insert implementation passes every sequential test above and
// fails this one, which is the precise reason the claim is an INSERT.
func TestOnlyOneOfManyConcurrentDuplicatesClaimsTheKey(t *testing.T) {
	f := setupIdempotency(t)
	const key, body, racers = "key-race", `{"email":"a@b.c"}`, 8

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		claimed   int
		conflicts int
		other     []error
	)

	start := make(chan struct{})
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			replay, err := f.begin(t, f.orgA, f.clientA, key, body)

			mu.Lock()
			defer mu.Unlock()
			var fault Fault
			switch {
			case err == nil && replay == nil:
				claimed++
			case errors.As(err, &fault) && fault.Class == Conflict:
				conflicts++
			default:
				other = append(other, err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if len(other) > 0 {
		t.Fatalf("unexpected errors from the race: %v", other)
	}
	if claimed != 1 {
		t.Fatalf("%d of %d concurrent duplicates claimed the key — the handler would run %d times",
			claimed, racers, claimed)
	}
	if conflicts != racers-1 {
		t.Errorf("%d conflicts, want %d", conflicts, racers-1)
	}

	// And exactly one row exists, not one per racer.
	var rows int
	f.factory.QueryRow(&rows, `SELECT count(*) FROM idempotency_records WHERE key = $1`, key)
	if rows != 1 {
		t.Errorf("%d rows for one key", rows)
	}
}

// --- who a record belongs to ----------------------------------------------------------

// A key is namespaced by CLIENT. Two clients choosing "key-1" — which they
// will, because callers pick keys like "1" — must not collide, and neither must
// be able to read the other's stored answer by guessing.
func TestAKeyIsPrivateToTheClientThatUsedIt(t *testing.T) {
	f := setupIdempotency(t)
	const key, body = "key-1", `{"email":"a@b.c"}`

	if _, err := f.begin(t, f.orgA, f.clientA, key, body); err != nil {
		t.Fatalf("Begin as the first client: %v", err)
	}
	f.complete(t, f.orgA, f.clientA, key, 201, `{"id":"THE-FIRST-CLIENTS-USER"}`)

	// The same organization, a different client, the same key and body.
	replay, err := f.begin(t, f.orgA, f.clientA2, key, body)
	if err != nil {
		t.Fatalf("Begin as the second client: %v", err)
	}
	if replay != nil {
		t.Fatalf("a second client read the first's stored answer: %q", replay.Response)
	}
}

// The same, across tenants. This one is RLS's job rather than the key's, and it
// is asserted from both ends: the second organization gets its own claim, AND
// an unfiltered read under that organization cannot see the first's row.
func TestARecordIsConfinedToItsTenant(t *testing.T) {
	f := setupIdempotency(t)
	const key, body = "key-1", `{"email":"a@b.c"}`

	if _, err := f.begin(t, f.orgA, f.clientA, key, body); err != nil {
		t.Fatalf("Begin in org A: %v", err)
	}
	f.complete(t, f.orgA, f.clientA, key, 201, `{"id":"ORG-A-USER"}`)

	replay, err := f.begin(t, f.orgB, f.clientB, key, body)
	if err != nil {
		t.Fatalf("Begin in org B: %v", err)
	}
	if replay != nil {
		t.Fatalf("another tenant read org A's stored answer: %q", replay.Response)
	}

	// The unfiltered read: no WHERE org_id, exactly as docs/PLAN/08 Part B asks
	// isolation to survive.
	var visible int
	err = f.db.WithTenant(context.Background(), f.orgB, func(tx *postgres.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT count(*) FROM idempotency_records`).Scan(&visible)
	})
	if err != nil {
		t.Fatalf("counting under org B: %v", err)
	}
	if visible != 1 {
		t.Errorf("org B sees %d records; it wrote 1, so the rest leaked across the tenant boundary", visible)
	}
}

// --- what is written ------------------------------------------------------------------

// The request body is hashed and never stored. A user-creation call carries a
// password, and this table would otherwise be a durable copy of it.
func TestTheRequestBodyNeverReachesTheTable(t *testing.T) {
	f := setupIdempotency(t)
	const secret = "correct-horse-battery-staple"

	body := fmt.Sprintf(`{"email":"a@b.c","password":%q}`, secret)
	if _, err := f.begin(t, f.orgA, f.clientA, "key-1", body); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	f.complete(t, f.orgA, f.clientA, "key-1", 201, `{"id":"user-1"}`)

	// The whole row, as text, read as the owner so nothing is hidden by RLS.
	var dump string
	f.factory.QueryRow(&dump,
		`SELECT idempotency_records::text FROM idempotency_records WHERE key = $1`, "key-1")

	if strings.Contains(dump, secret) {
		t.Fatalf("the request body is in the table: %s", dump)
	}
	// And the hash IS there, or the comparison this table exists for is absent.
	if !strings.Contains(dump, HashRequest("POST", "/v1/users", []byte(body))) {
		t.Errorf("the request hash is not stored: %s", dump)
	}
}

// --- releasing ---------------------------------------------------------------------

// Release drops an unfinished claim, so a failed request stays retryable.
func TestReleaseMakesAFailedRequestRetryable(t *testing.T) {
	f := setupIdempotency(t)
	const key, body = "key-1", `{"email":"a@b.c"}`

	if _, err := f.begin(t, f.orgA, f.clientA, key, body); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := f.claims.Release(f.orgA, f.clientA, key); err != nil {
		t.Fatalf("Release: %v", err)
	}

	replay, err := f.begin(t, f.orgA, f.clientA, key, body)
	if err != nil {
		t.Fatalf("Begin after Release: %v", err)
	}
	if replay != nil {
		t.Fatal("a released claim still replayed an answer")
	}
}

// Release must NOT drop a completed record. Without the `status IS NULL`
// predicate it would, and a caller able to make one request fail after a
// sibling succeeded could erase the sibling's stored answer.
func TestReleaseCannotEraseACompletedRecord(t *testing.T) {
	f := setupIdempotency(t)
	const key, body = "key-1", `{"email":"a@b.c"}`

	if _, err := f.begin(t, f.orgA, f.clientA, key, body); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	f.complete(t, f.orgA, f.clientA, key, 201, `{"id":"user-1"}`)

	if err := f.claims.Release(f.orgA, f.clientA, key); err != nil {
		t.Fatalf("Release: %v", err)
	}

	replay, err := f.begin(t, f.orgA, f.clientA, key, body)
	if err != nil {
		t.Fatalf("Begin after Release: %v", err)
	}
	if replay == nil {
		t.Fatal("Release erased a completed record — the retry now runs a second time")
	}
}

// --- expiry -------------------------------------------------------------------------

// An expired record is re-claimable rather than a permanent conflict. A key a
// caller reuses daily must not be refused forever because they used it once.
func TestAnExpiredRecordIsReclaimed(t *testing.T) {
	f := setupIdempotency(t)
	const key = "key-1"

	if _, err := f.begin(t, f.orgA, f.clientA, key, `{"a":1}`); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	f.complete(t, f.orgA, f.clientA, key, 201, `{"id":"old"}`)

	// A DIFFERENT body, which would be a conflict were the record still live.
	after := now().Add(IdempotencyTTL + time.Minute)
	replay, err := f.claims.Begin(context.Background(), f.orgA, f.clientA, key,
		"POST", "/v1/users", []byte(`{"a":2}`), after)
	if err != nil {
		t.Fatalf("Begin after expiry: %v", err)
	}
	if replay != nil {
		t.Fatalf("an expired record was replayed: %q", replay.Response)
	}

	// And the reclaimed row is the NEW request, not the old one left in place.
	var status *int
	f.factory.QueryRow(&status, `SELECT status FROM idempotency_records WHERE key = $1`, key)
	if status != nil {
		t.Errorf("the reclaimed row still carries the old answer's status %d", *status)
	}
}

// The record is honoured right up to its expiry, and not a moment past.
func TestARecordIsHonouredUntilItExpires(t *testing.T) {
	f := setupIdempotency(t)
	const key, body = "key-1", `{"a":1}`

	if _, err := f.begin(t, f.orgA, f.clientA, key, body); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	f.complete(t, f.orgA, f.clientA, key, 201, `{"id":"x"}`)

	justInside := now().Add(IdempotencyTTL - time.Second)
	replay, err := f.claims.Begin(context.Background(), f.orgA, f.clientA, key,
		"POST", "/v1/users", []byte(body), justInside)
	if err != nil {
		t.Fatalf("Begin just inside the TTL: %v", err)
	}
	if replay == nil {
		t.Error("a record inside its TTL was not honoured")
	}
}

// --- the sweep -----------------------------------------------------------------------

// PostgreSQL has no TTL, so expiry is only real if something deletes. P0-20's
// finding was a cleanup job that silently did nothing for days, which is why
// Sweep returns a count rather than only an error.
func TestSweepDeletesExpiredRecordsAndLeavesLiveOnes(t *testing.T) {
	f := setupIdempotency(t)

	for _, key := range []string{"old-1", "old-2"} {
		if _, err := f.begin(t, f.orgA, f.clientA, key, `{"a":1}`); err != nil {
			t.Fatalf("Begin %s: %v", key, err)
		}
	}
	live := now().Add(IdempotencyTTL / 2)
	if _, err := f.claims.Begin(context.Background(), f.orgA, f.clientA, "fresh",
		"POST", "/v1/users", []byte(`{"a":1}`), live); err != nil {
		t.Fatalf("Begin fresh: %v", err)
	}

	// Between the two expiries.
	sweepAt := now().Add(IdempotencyTTL + time.Minute)
	deleted, err := NewIdempotencyStore().Sweep(context.Background(), f.db, sweepAt, 100)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if deleted != 2 {
		t.Errorf("Sweep reported %d deletions, want 2", deleted)
	}

	rows := 0
	f.factory.QueryRow(&rows, `SELECT count(*) FROM idempotency_records`)
	if rows != 1 {
		var keys string
		f.factory.QueryRow(&keys,
			`SELECT string_agg(key, ',') FROM idempotency_records`)
		t.Fatalf("%d records survived the sweep (%s), want only the live one", rows, keys)
	}
	var survivor string
	f.factory.QueryRow(&survivor, `SELECT key FROM idempotency_records`)
	if survivor != "fresh" {
		t.Errorf("the sweep kept %q and deleted the live record", survivor)
	}
}

// The sweep is bounded, so it cannot become a single statement that locks the
// table for the length of a backlog.
func TestSweepRespectsItsLimit(t *testing.T) {
	f := setupIdempotency(t)

	for i := range 5 {
		if _, err := f.begin(t, f.orgA, f.clientA, fmt.Sprintf("k-%d", i), `{"a":1}`); err != nil {
			t.Fatalf("Begin: %v", err)
		}
	}

	sweepAt := now().Add(IdempotencyTTL + time.Minute)
	deleted, err := NewIdempotencyStore().Sweep(context.Background(), f.db, sweepAt, 2)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if deleted != 2 {
		t.Errorf("Sweep deleted %d with a limit of 2", deleted)
	}
}

// --- the schema's own guarantees --------------------------------------------------------

// A half-written record — a status with no body, or a body with no status —
// cannot be replayed and would be worse than none. The CHECK constraint is what
// makes that unrepresentable rather than merely unlikely.
func TestAHalfWrittenRecordIsRejectedByTheDatabase(t *testing.T) {
	f := setupIdempotency(t)

	if _, err := f.begin(t, f.orgA, f.clientA, "key-1", `{"a":1}`); err != nil {
		t.Fatalf("Begin: %v", err)
	}

	err := f.db.WithTenant(context.Background(), f.orgA, func(tx *postgres.Tx) error {
		_, err := tx.Exec(context.Background(),
			`UPDATE idempotency_records SET status = 201 WHERE key = $1`, "key-1")
		return err
	})
	if err == nil {
		t.Fatal("a status with no response was accepted")
	}
	if !strings.Contains(err.Error(), "idempotency_response_complete") {
		t.Errorf("refused by %v, not by the constraint", err)
	}
}
