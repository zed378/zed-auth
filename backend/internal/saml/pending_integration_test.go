//go:build integration

package saml

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

// One AuthnRequest, one answer (P4-08 C-3, A-3).

type pendingFixture struct {
	*replayFixture
	spID    string
	factory *testsupport.Factory
}

func setupPending(t *testing.T) *pendingFixture {
	t.Helper()
	f := setupReplay(t)

	stack := testsupport.Start(t)
	factory := testsupport.NewFactory(t, stack)

	// A service provider to hang the requests off. Seeded directly: registering
	// one is P4-09's console work, and this test is about the request store.
	var projectID, appID, spID string
	factory.QueryRow(&projectID,
		`INSERT INTO projects (org_id, name) VALUES ($1, 'saml-test') RETURNING id`, f.orgID)
	factory.QueryRow(&appID,
		`INSERT INTO applications (project_id, org_id, name, type) VALUES ($1, $2, 'sp', 'web') RETURNING id`,
		projectID, f.orgID)
	factory.QueryRow(&spID, `
		INSERT INTO saml_service_providers (application_id, org_id, entity_id, acs_url)
		VALUES ($1, $2, $3, 'https://sp.example.test/acs') RETURNING id`,
		appID, f.orgID, "https://sp.example.test/"+t.Name())

	return &pendingFixture{replayFixture: f, spID: spID, factory: factory}
}

func (f *pendingFixture) pending(id, relay string, now time.Time) Pending {
	return Pending{
		ID:         id,
		SPID:       f.spID,
		RelayState: relay,
		CreatedAt:  now,
		ExpiresAt:  now.Add(PendingLifetime),
	}
}

func (f *pendingFixture) record(t *testing.T, p Pending) error {
	t.Helper()
	var result error
	if err := f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
		result = NewRequests().Record(context.Background(), tx, f.orgID, p)
		if errors.Is(result, ErrAlreadyAnswered) {
			return nil
		}
		return result
	}); err != nil && !errors.Is(err, ErrAlreadyAnswered) {
		t.Fatalf("recording %q: %v", p.ID, err)
	}
	return result
}

func (f *pendingFixture) consume(t *testing.T, id string, now time.Time) (Pending, error) {
	t.Helper()
	var (
		out    Pending
		result error
	)
	_ = f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
		out, result = NewRequests().Consume(context.Background(), tx, id, now)
		return nil
	})
	return out, result
}

func TestAnAuthnRequestIsAnsweredOnce(t *testing.T) {
	f := setupPending(t)
	now := time.Now()

	if err := f.record(t, f.pending("_req1", "/dashboard", now)); err != nil {
		t.Fatalf("recording: %v", err)
	}

	got, err := f.consume(t, "_req1", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("the first answer was refused: %v", err)
	}
	if got.RelayState != "/dashboard" {
		t.Errorf("RelayState is %q, want /dashboard — it is the SP's own state", got.RelayState)
	}
	if got.SPID != f.spID {
		t.Errorf("the request came back against SP %q, want %q", got.SPID, f.spID)
	}

	if _, err := f.consume(t, "_req1", now.Add(2*time.Minute)); !errors.Is(err, ErrAlreadyAnswered) {
		t.Errorf("the second answer gave %v, want ErrAlreadyAnswered", err)
	}
}

// A-3, and the race a SELECT-then-UPDATE loses. An attacker replaying a captured
// request chooses when to arrive, so "the window is small" is not a defence.
func TestConcurrentAnswersToOneRequestSucceedExactlyOnce(t *testing.T) {
	f := setupPending(t)
	now := time.Now()

	if err := f.record(t, f.pending("_raced", "", now)); err != nil {
		t.Fatalf("recording: %v", err)
	}

	const attempts = 10
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		succeeded int
		refused   int
	)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.consume(t, "_raced", now.Add(time.Minute))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				succeeded++
			case errors.Is(err, ErrAlreadyAnswered), errors.Is(err, ErrNoSuchRequest):
				refused++
			}
		}()
	}
	wg.Wait()

	if succeeded != 1 {
		t.Errorf("%d of %d concurrent answers succeeded, want exactly 1", succeeded, attempts)
	}
	if succeeded+refused != attempts {
		t.Errorf("%d succeeded + %d refused = %d, want %d — the rest failed for another reason",
			succeeded, refused, succeeded+refused, attempts)
	}
}

// An expired request is not answered, and is not silently consumed either — a
// consume that marked it answered before noticing would turn a stale login into
// an unexplained "already answered" next time.
func TestAnExpiredAuthnRequestIsRefusedAndNotConsumed(t *testing.T) {
	f := setupPending(t)
	now := time.Now()

	if err := f.record(t, f.pending("_stale", "", now)); err != nil {
		t.Fatalf("recording: %v", err)
	}

	late := now.Add(PendingLifetime + time.Minute)
	if _, err := f.consume(t, "_stale", late); !errors.Is(err, ErrNoSuchRequest) {
		t.Errorf("an expired request gave %v, want ErrNoSuchRequest with the expiry named", err)
	}

	// Still unconsumed: the refusal above must not have claimed it.
	var consumed int
	f.factory.QueryRow(&consumed,
		`SELECT count(*) FROM saml_authn_requests WHERE id = '_stale' AND consumed_at IS NOT NULL`)
	if consumed != 0 {
		t.Error("an expired request was marked answered by the refusal")
	}
}

func TestAnUnknownAuthnRequestIsRefused(t *testing.T) {
	f := setupPending(t)
	if _, err := f.consume(t, "_never-existed", time.Now()); !errors.Is(err, ErrNoSuchRequest) {
		t.Errorf("an unknown request gave %v, want ErrNoSuchRequest", err)
	}
}

// A service provider reusing an id is broken or replaying. Accepting the second
// would give a replayed request a fresh window.
func TestARepeatedAuthnRequestIDIsRefused(t *testing.T) {
	f := setupPending(t)
	now := time.Now()

	if err := f.record(t, f.pending("_dup", "", now)); err != nil {
		t.Fatalf("recording: %v", err)
	}
	if err := f.record(t, f.pending("_dup", "", now.Add(time.Minute))); !errors.Is(err, ErrAlreadyAnswered) {
		t.Errorf("a repeated request id gave %v, want a refusal", err)
	}
}

// The RelayState bound lives in the schema, so it holds for any writer.
func TestAnOversizedRelayStateIsRefusedByTheDatabase(t *testing.T) {
	f := setupPending(t)
	now := time.Now()

	p := f.pending("_big", strings.Repeat("x", 9000), now)

	var err error
	_ = f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
		err = NewRequests().Record(context.Background(), tx, f.orgID, p)
		return nil
	})
	if err == nil {
		t.Error("a 9 KiB RelayState was stored — the bound is not enforced")
	}
}

func TestPruningRemovesOnlyExpiredAuthnRequests(t *testing.T) {
	f := setupPending(t)
	now := time.Now()

	old := f.pending("_old", "", now.Add(-2*PendingLifetime))
	if err := f.record(t, old); err != nil {
		t.Fatalf("recording the old request: %v", err)
	}
	if err := f.record(t, f.pending("_live", "", now)); err != nil {
		t.Fatalf("recording the live request: %v", err)
	}

	var removed int64
	if err := f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
		var err error
		removed, err = NewRequests().Prune(context.Background(), tx, now)
		return err
	}); err != nil {
		t.Fatalf("pruning: %v", err)
	}
	if removed != 1 {
		t.Errorf("pruned %d rows, want 1", removed)
	}

	if _, err := f.consume(t, "_live", now.Add(time.Minute)); err != nil {
		t.Errorf("a live request was pruned: %v", err)
	}
}
