package social

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type memoryStateRedis struct {
	mu     sync.Mutex
	values map[string]string
	ttl    time.Duration
	err    error
}

func (m *memoryStateRedis) SetNX(ctx context.Context, key string, value interface{}, ttl time.Duration) *redis.BoolCmd {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return redis.NewBoolResult(false, m.err)
	}
	if _, ok := m.values[key]; ok {
		return redis.NewBoolResult(false, nil)
	}
	m.values[key] = string(value.([]byte))
	m.ttl = ttl
	return redis.NewBoolResult(true, nil)
}

func (m *memoryStateRedis) GetDel(ctx context.Context, key string) *redis.StringCmd {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return redis.NewStringResult("", m.err)
	}
	value, ok := m.values[key]
	delete(m.values, key)
	if !ok {
		return redis.NewStringResult("", redis.Nil)
	}
	return redis.NewStringResult(value, nil)
}

func testStateBinding() StateBinding {
	return StateBinding{
		OrgID: "c0aa2914-c4c7-4401-bab4-145c513b6613", Provider: Google,
		Intent: SignInIntent, PendingID: "server-pending-request",
		RedirectURI:  "https://auth.example.test/login/social/google/callback",
		BrowserToken: strings.Repeat("b", 43),
	}
}

func memoryStateStore() (*StateStore, *memoryStateRedis) {
	client := &memoryStateRedis{values: make(map[string]string)}
	return &StateStore{client: client, now: time.Now}, client
}

func TestStateIssuesIndependentPKCEAndNonce(t *testing.T) {
	store, client := memoryStateStore()
	ctx := context.Background()
	for _, provider := range []Provider{Google, Microsoft, GitHub} {
		binding := testStateBinding()
		binding.Provider = provider
		binding.RedirectURI = "https://auth.example.test/login/social/" + string(provider) + "/callback"
		issued, err := store.Issue(ctx, binding)
		if err != nil {
			t.Fatal(err)
		}
		if !validStateToken(issued.State) || !validStateToken(issued.PKCEVerifier) || issued.State == issued.PKCEVerifier {
			t.Fatal("state and verifier must be separate 256-bit tokens")
		}
		if provider == GitHub && issued.Nonce != "" || provider != GitHub && (!validStateToken(issued.Nonce) || issued.Nonce == issued.State || issued.Nonce == issued.PKCEVerifier) {
			t.Fatal("incorrect provider nonce")
		}
		challenge := sha256.Sum256([]byte(issued.PKCEVerifier))
		if issued.PKCEChallenge != base64.RawURLEncoding.EncodeToString(challenge[:]) || client.ttl != 10*time.Minute {
			t.Fatal("incorrect S256 challenge or TTL")
		}
		if strings.Contains(client.values[stateKey(issued.State)], binding.BrowserToken) || issued.Binding.BrowserToken != "" {
			t.Fatal("browser token escaped its binding digest")
		}
		consumed, err := store.Consume(ctx, issued.State, binding)
		if err != nil || consumed.PKCEVerifier != issued.PKCEVerifier || consumed.Nonce != issued.Nonce || consumed.Binding.OrgID != binding.OrgID {
			t.Fatalf("consuming issued state: %v", err)
		}
		if _, err := store.Consume(ctx, issued.State, binding); !errors.Is(err, ErrInvalidState) {
			t.Fatal("state replay accepted")
		}
	}
}

func TestStateRejectsEveryBindingMismatchAndSpendsAttempt(t *testing.T) {
	mutations := map[string]func(*StateBinding){
		"organization": func(b *StateBinding) { b.OrgID = "176f9d11-a695-4aa6-be2a-90bcda0cb915" },
		"provider": func(b *StateBinding) {
			b.Provider = Microsoft
			b.RedirectURI = "https://auth.example.test/login/social/microsoft/callback"
		},
		"pending":  func(b *StateBinding) { b.PendingID += "-different" },
		"redirect": func(b *StateBinding) { b.RedirectURI = "https://other.example.test/login/social/google/callback" },
		"browser":  func(b *StateBinding) { b.BrowserToken = strings.Repeat("c", 43) },
		"intent":   func(b *StateBinding) { b.Intent = LinkIntent; b.UserID = "user"; b.SessionID = "session" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			store, _ := memoryStateStore()
			binding := testStateBinding()
			issued, err := store.Issue(context.Background(), binding)
			if err != nil {
				t.Fatal(err)
			}
			other := binding
			mutate(&other)
			if _, err := store.Consume(context.Background(), issued.State, other); !errors.Is(err, ErrInvalidState) {
				t.Fatalf("mismatch accepted: %v", err)
			}
			if _, err := store.Consume(context.Background(), issued.State, binding); !errors.Is(err, ErrInvalidState) {
				t.Fatal("mismatched callback was not spent")
			}
		})
	}
}

func TestLinkStateBindsAuthenticatedUserAndSession(t *testing.T) {
	for _, field := range []string{"user", "session"} {
		t.Run(field, func(t *testing.T) {
			store, _ := memoryStateStore()
			binding := testStateBinding()
			binding.Intent, binding.PendingID = LinkIntent, ""
			binding.UserID, binding.SessionID = "authenticated-user", "authenticated-session"
			issued, err := store.Issue(context.Background(), binding)
			if err != nil {
				t.Fatal(err)
			}
			if field == "user" {
				binding.UserID += "-other"
			} else {
				binding.SessionID += "-other"
			}
			if _, err := store.Consume(context.Background(), issued.State, binding); !errors.Is(err, ErrInvalidState) {
				t.Fatal("cross-account link state accepted")
			}
		})
	}
}

func TestStateRejectsInvalidInputsBeforeRedis(t *testing.T) {
	for name, mutate := range map[string]func(*StateBinding){
		"org":                     func(b *StateBinding) { b.OrgID = "" },
		"nil org":                 func(b *StateBinding) { b.OrgID = "00000000-0000-0000-0000-000000000000" },
		"provider":                func(b *StateBinding) { b.Provider = "unknown" },
		"intent":                  func(b *StateBinding) { b.Intent = "unknown" },
		"pending":                 func(b *StateBinding) { b.PendingID = "" },
		"pending bound":           func(b *StateBinding) { b.PendingID = strings.Repeat("x", 257) },
		"browser":                 func(b *StateBinding) { b.BrowserToken = "short" },
		"redirect tls":            func(b *StateBinding) { b.RedirectURI = "http://auth.example.test/callback" },
		"redirect fragment":       func(b *StateBinding) { b.RedirectURI += "#untrusted" },
		"redirect empty fragment": func(b *StateBinding) { b.RedirectURI += "#" },
		"redirect query":          func(b *StateBinding) { b.RedirectURI += "?untrusted=1" },
		"redirect empty query":    func(b *StateBinding) { b.RedirectURI += "?" },
		"redirect path":           func(b *StateBinding) { b.RedirectURI = "https://auth.example.test/login/social/microsoft/callback" },
		"redirect encoded path":   func(b *StateBinding) { b.RedirectURI = "https://auth.example.test/login/social/google/%63allback" },
		"redirect opaque":         func(b *StateBinding) { b.RedirectURI = "https:auth.example.test/login/social/google/callback" },
		"redirect userinfo":       func(b *StateBinding) { b.RedirectURI = "https://user@auth.example.test/callback" },
		"signin user":             func(b *StateBinding) { b.UserID = "unexpected" },
		"link session":            func(b *StateBinding) { b.Intent = LinkIntent; b.UserID = "user" },
	} {
		t.Run(name, func(t *testing.T) {
			store, client := memoryStateStore()
			binding := testStateBinding()
			mutate(&binding)
			if _, err := store.Issue(context.Background(), binding); !errors.Is(err, ErrInvalidStateBinding) {
				t.Fatal("invalid binding accepted")
			}
			if len(client.values) != 0 {
				t.Fatal("invalid binding reached Redis")
			}
		})
	}
	store, _ := memoryStateStore()
	for _, token := range []string{"", "short", strings.Repeat("x", 100000), strings.Repeat("=", 43)} {
		if _, err := store.Consume(context.Background(), token, testStateBinding()); !errors.Is(err, ErrInvalidState) {
			t.Fatal("invalid state accepted")
		}
	}
}

func TestStateExpiresAndRefusesCorruptStoredCredentials(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		store, client := memoryStateStore()
		binding := testStateBinding()
		now := time.Now().UTC()
		store.now = func() time.Time { return now }
		issued, err := store.Issue(context.Background(), binding)
		if err != nil {
			t.Fatal(err)
		}
		if corrupt {
			client.values[stateKey(issued.State)] = `{"verifier":"corrupt"}`
		} else {
			now = now.Add(StateLifetime)
		}
		if _, err := store.Consume(context.Background(), issued.State, binding); !errors.Is(err, ErrInvalidState) {
			t.Fatal("expired or corrupt state accepted")
		}
	}
}

func TestStateStorageOutageFailsClosedWithoutLeakingDetails(t *testing.T) {
	store, client := memoryStateStore()
	binding := testStateBinding()
	issued, err := store.Issue(context.Background(), binding)
	if err != nil {
		t.Fatal(err)
	}
	client.err = errors.New("raw Redis error may contain credentials")
	if _, err := store.Issue(context.Background(), binding); err != ErrStateUnavailable {
		t.Fatalf("issue outage: %v", err)
	}
	if _, err := store.Consume(context.Background(), issued.State, binding); err != ErrStateUnavailable {
		t.Fatalf("consume outage: %v", err)
	}
}

func TestStateConcurrentRedemptionHasExactlyOneWinner(t *testing.T) {
	store, _ := memoryStateStore()
	assertSingleStateConsumer(t, store)
}

func assertSingleStateConsumer(t *testing.T, store *StateStore) {
	t.Helper()
	binding := testStateBinding()
	issued, err := store.Issue(context.Background(), binding)
	if err != nil {
		t.Fatal(err)
	}
	var successes atomic.Int32
	var group sync.WaitGroup
	for range 24 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := store.Consume(context.Background(), issued.State, binding)
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrInvalidState) {
				t.Errorf("consume: %v", err)
			}
		}()
	}
	group.Wait()
	if successes.Load() != 1 {
		t.Fatalf("callback winners: %d", successes.Load())
	}
}
