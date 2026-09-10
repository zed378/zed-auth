//go:build integration

package authorize

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }

func store(t *testing.T) *Store {
	t.Helper()

	stack := testsupport.Start(t)
	rdb := redis.NewClient(&redis.Options{Addr: stack.RedisAddr})
	t.Cleanup(func() { _ = rdb.Close() })

	if err := rdb.FlushAll(context.Background()).Err(); err != nil {
		t.Fatalf("flushing redis: %v", err)
	}
	return NewStore(rdb)
}

func aCode() Code {
	return Code{
		ClientID:      "11111111-1111-1111-1111-111111111111",
		RedirectURI:   "https://app.example.com/callback",
		UserID:        "44444444-4444-4444-4444-444444444444",
		OrgID:         "22222222-2222-2222-2222-222222222222",
		SessionID:     "33333333-3333-3333-3333-333333333333",
		Scope:         []string{"openid", "profile"},
		CodeChallenge: "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		AuthMethods:   []string{"pwd"},
		AuthTime:      time.Now().Add(-time.Minute),
		IssuedAt:      time.Now(),
	}
}

// P1-06 DoD item 6: codes are single-use.
func TestACodeRedeemsExactlyOnce(t *testing.T) {
	s := store(t)
	ctx := context.Background()

	code, err := s.IssueCode(ctx, aCode(), CodeTTL)
	if err != nil {
		t.Fatalf("IssueCode: %v", err)
	}

	first, err := s.RedeemCode(ctx, code)
	if err != nil {
		t.Fatalf("first redemption failed: %v", err)
	}
	if first.ClientID != aCode().ClientID || first.CodeChallenge != aCode().CodeChallenge {
		t.Errorf("the redeemed code lost what it bound: %+v", first)
	}

	if _, err := s.RedeemCode(ctx, code); !errors.Is(err, ErrCodeNotFound) {
		t.Errorf("second redemption = %v, want ErrCodeNotFound", err)
	}
}

// The requirement docs/PLAN/04 states explicitly: "two concurrent redemptions of one
// code must yield exactly one success".
//
// This is the test that distinguishes GETDEL from GET-then-DEL. The two-call
// version passes every sequential test in this file and loses this one, which
// is exactly the shape of bug that reaches production.
func TestConcurrentRedemptionYieldsExactlyOneSuccess(t *testing.T) {
	s := store(t)
	ctx := context.Background()

	const racers = 32

	// Repeated, because a race that fires one time in ten would otherwise pass
	// on a lucky run.
	for round := range 25 {
		code, err := s.IssueCode(ctx, aCode(), CodeTTL)
		if err != nil {
			t.Fatalf("IssueCode: %v", err)
		}

		var (
			wg        sync.WaitGroup
			mu        sync.Mutex
			successes int
			notFound  int
			other     []error
		)

		start := make(chan struct{})
		for range racers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start // release them together

				_, err := s.RedeemCode(ctx, code)

				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					successes++
				case errors.Is(err, ErrCodeNotFound):
					notFound++
				default:
					other = append(other, err)
				}
			}()
		}
		close(start)
		wg.Wait()

		if len(other) > 0 {
			t.Fatalf("round %d: unexpected errors: %v", round, other)
		}
		if successes != 1 {
			t.Fatalf("round %d: %d concurrent redemptions succeeded, want exactly 1. "+
				"A GET-then-DEL implementation passes every sequential test and fails here",
				round, successes)
		}
		if notFound != racers-1 {
			t.Errorf("round %d: %d not-found, want %d", round, notFound, racers-1)
		}
	}
}

// An unknown code, an expired one and a redeemed one are one answer.
// Distinguishing them would say whether a code ever existed.
func TestUnknownCodesAreIndistinguishable(t *testing.T) {
	s := store(t)
	ctx := context.Background()

	issued, err := s.IssueCode(ctx, aCode(), CodeTTL)
	if err != nil {
		t.Fatalf("IssueCode: %v", err)
	}
	if _, err := s.RedeemCode(ctx, issued); err != nil {
		t.Fatalf("redeeming: %v", err)
	}

	for _, name := range []string{"redeemed", "never existed", "empty"} {
		code := issued
		switch name {
		case "never existed":
			code = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		case "empty":
			code = ""
		}

		if _, err := s.RedeemCode(ctx, code); !errors.Is(err, ErrCodeNotFound) {
			t.Errorf("%s: err = %v, want ErrCodeNotFound", name, err)
		}
	}
}

// P1-06 DoD item 6, and docs/PLAN/04's bound: under 60 seconds.
func TestCodesExpire(t *testing.T) {
	s := store(t)
	ctx := context.Background()

	if CodeTTL >= time.Minute {
		t.Fatalf("CodeTTL is %s; docs/PLAN/04 requires under a minute", CodeTTL)
	}

	// A short TTL rather than waiting out the real one.
	code, err := s.IssueCode(ctx, aCode(), 300*time.Millisecond)
	if err != nil {
		t.Fatalf("IssueCode: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	if _, err := s.RedeemCode(ctx, code); !errors.Is(err, ErrCodeNotFound) {
		t.Errorf("an expired code redeemed: %v", err)
	}
}

// The TTL bound is enforced here rather than trusted to callers: a code that
// outlives a minute is a credential sitting in a URL.
func TestIssueRefusesAnOverlongTTL(t *testing.T) {
	s := store(t)

	for _, ttl := range []time.Duration{0, -time.Second, 2 * time.Minute} {
		if _, err := s.IssueCode(context.Background(), aCode(), ttl); err == nil {
			t.Errorf("IssueCode accepted a TTL of %s", ttl)
		}
	}
}

// Two codes are never the same value.
func TestCodesAreUnique(t *testing.T) {
	s := store(t)
	ctx := context.Background()

	seen := map[string]bool{}
	for range 200 {
		code, err := s.IssueCode(ctx, aCode(), CodeTTL)
		if err != nil {
			t.Fatalf("IssueCode: %v", err)
		}
		if seen[code] {
			t.Fatal("IssueCode returned a duplicate")
		}
		seen[code] = true
	}
}

// --- pending requests -------------------------------------------------------------

// A resumable-twice request would let one login satisfy two authorization
// flows — a code issued for something nobody re-authorised.
func TestAPendingRequestResumesOnlyOnce(t *testing.T) {
	s := store(t)
	ctx := context.Background()

	req := Request{
		ClientID:      "11111111-1111-1111-1111-111111111111",
		RedirectURI:   "https://app.example.com/callback",
		Scope:         []string{"openid"},
		State:         "xyz",
		CodeChallenge: "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
	}

	id, err := s.SavePending(ctx, req, PendingTTL)
	if err != nil {
		t.Fatalf("SavePending: %v", err)
	}

	got, err := s.LoadPending(ctx, id)
	if err != nil {
		t.Fatalf("LoadPending: %v", err)
	}
	if got.ClientID != req.ClientID || got.RedirectURI != req.RedirectURI ||
		got.State != req.State || got.CodeChallenge != req.CodeChallenge {
		t.Errorf("the resumed request lost detail: %+v", got)
	}

	if _, err := s.LoadPending(ctx, id); !errors.Is(err, ErrPendingNotFound) {
		t.Errorf("a pending request resumed twice: %v", err)
	}
}

func TestUnknownPendingRequests(t *testing.T) {
	s := store(t)

	for _, id := range []string{"", "nope"} {
		if _, err := s.LoadPending(context.Background(), id); !errors.Is(err, ErrPendingNotFound) {
			t.Errorf("LoadPending(%q) = %v, want ErrPendingNotFound", id, err)
		}
	}
}

// PeekPending reads without consuming; LoadPending consumes. They are two
// methods rather than one with a flag, and this is the test that they behave
// differently — a PeekPending implemented as GETDEL would give every account
// exactly one attempt at its password.
func TestPeekPendingDoesNotConsume(t *testing.T) {
	s := store(t)
	ctx := context.Background()

	id, err := s.SavePending(ctx, Request{
		ClientID: "client", RedirectURI: "https://app.example/cb", State: "xyz",
	}, PendingTTL)
	if err != nil {
		t.Fatalf("SavePending: %v", err)
	}

	for i := range 3 {
		got, err := s.PeekPending(ctx, id)
		if err != nil {
			t.Fatalf("peek %d: %v", i+1, err)
		}
		if got.State != "xyz" {
			t.Errorf("peek %d lost detail: %+v", i+1, got)
		}
	}

	// And it is still there for the one call that is meant to spend it.
	if _, err := s.LoadPending(ctx, id); err != nil {
		t.Fatalf("LoadPending after three peeks: %v", err)
	}
	if _, err := s.PeekPending(ctx, id); !errors.Is(err, ErrPendingNotFound) {
		t.Errorf("the request survived LoadPending: %v", err)
	}
}

func TestPeekPendingOnUnknownRequests(t *testing.T) {
	s := store(t)

	for _, id := range []string{"", "nope"} {
		if _, err := s.PeekPending(context.Background(), id); !errors.Is(err, ErrPendingNotFound) {
			t.Errorf("PeekPending(%q) = %v, want ErrPendingNotFound", id, err)
		}
	}
}
