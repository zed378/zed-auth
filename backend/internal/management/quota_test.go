package management

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/ratelimit"
)

// countingCounter records what it was asked and answers what it was told.
//
// Scripted rather than a working counter: what a counter does under
// concurrency is Redis's job and is proved in internal/ratelimit. What is
// tested here is the middleware — the headers, the refusal, and the ordering
// against the handler.
type countingCounter struct {
	verdict ratelimit.Verdict
	asked   []string
}

func (c *countingCounter) Consume(_ context.Context, clientID string, _ time.Time) ratelimit.Verdict {
	c.asked = append(c.asked, clientID)
	return c.verdict
}

func allowed(remaining int) ratelimit.Verdict {
	return ratelimit.Verdict{
		Allowed: true, Limit: 600, Remaining: remaining,
		Reset: at(0).Add(time.Minute),
	}
}

func refused() ratelimit.Verdict {
	return ratelimit.Verdict{
		Allowed: false, Limit: 600, Remaining: 0,
		Reset: at(0).Add(time.Minute), RetryAfter: 42 * time.Second,
	}
}

func serveLimited(counter Counter, h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	rl := &RateLimit{Counter: counter, Now: func() time.Time { return at(0) }}
	w := httptest.NewRecorder()
	rl.Wrap(h).ServeHTTP(w, r)
	return w
}

// --- the headers ------------------------------------------------------------------------

// On EVERY response, not only on a refusal. A client that learns its remaining
// allowance only once it has run out cannot pace itself, which is the
// difference between a limit and a trap.
func TestTheQuotaHeadersAreOnASuccessfulResponse(t *testing.T) {
	counter := &countingCounter{verdict: allowed(599)}
	handler := &ran{response: `{"ok":true}`}

	w := serveLimited(counter, handler.handler(), post("", `{}`))

	if handler.reached != 1 {
		t.Fatalf("the handler ran %d times", handler.reached)
	}
	for header, want := range map[string]string{
		"X-RateLimit-Limit":     "600",
		"X-RateLimit-Remaining": "599",
		"X-RateLimit-Reset":     strconv.FormatInt(at(0).Add(time.Minute).Unix(), 10),
	} {
		if got := w.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

// The headers must be set BEFORE the handler writes. A handler that has already
// called WriteHeader has sent them, and anything added afterwards is silently
// dropped — leaving the limit invisible on exactly the successful responses a
// client paces itself against.
func TestTheQuotaHeadersSurviveAHandlerThatWritesImmediately(t *testing.T) {
	counter := &countingCounter{verdict: allowed(1)}

	// Writes a status and a body straight away, which is what every real
	// handler does.
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"x"}`))
	})

	w := serveLimited(counter, handler, post("", `{}`))

	// Result().Header, NOT Header(). The recorder's Header() map stays live
	// after the response is written, so a header set too late still appears
	// there — a test asserting on it passes even when the real client would
	// never receive it. Result() returns the snapshot taken at WriteHeader,
	// which is what actually goes on the wire.
	if got := w.Result().Header.Get("X-RateLimit-Remaining"); got != "1" {
		t.Errorf("X-RateLimit-Remaining = %q — the header was written too late to be sent", got)
	}
}

// --- the refusal --------------------------------------------------------------------------

// Over the bound is 429, the handler does not run, and Retry-After says when.
func TestOverTheBoundTheHandlerDoesNotRun(t *testing.T) {
	counter := &countingCounter{verdict: refused()}
	handler := &ran{response: `{"ok":true}`}

	w := serveLimited(counter, handler.handler(), post("", `{}`))

	if handler.reached != 0 {
		t.Fatalf("a refused request reached the handler %d times", handler.reached)
	}
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", w.Code)
	}
	if got := w.Header().Get("Retry-After"); got != "42" {
		t.Errorf("Retry-After = %q, want 42", got)
	}
	if got := w.Header().Get("X-RateLimit-Remaining"); got != "0" {
		t.Errorf("X-RateLimit-Remaining = %q on a refusal", got)
	}
	if !strings.Contains(w.Body.String(), "RATE_LIMITED") {
		t.Errorf("body = %s, want the RATE_LIMITED envelope", w.Body.String())
	}
}

// The refusal does not say which client, which bound, or what the count was.
// A caller holding a stolen credential must not be able to profile the limits
// by reading the responses.
func TestARefusalDoesNotDescribeTheBound(t *testing.T) {
	counter := &countingCounter{verdict: refused()}

	w := serveLimited(counter, (&ran{}).handler(), post("", `{}`))

	body := w.Body.String()
	for _, leak := range []string{testClient, "quota", "client_id"} {
		if strings.Contains(body, leak) {
			t.Errorf("the refusal mentions %q: %s", leak, body)
		}
	}
}

// --- what is counted ----------------------------------------------------------------------

// The bound is per CLIENT, so that is what is counted. Counting the user would
// let one compromised service account spread its calls across every user it
// can act as; counting the organization would let one client exhaust every
// other client in the tenant.
func TestTheCountIsKeyedOnTheClient(t *testing.T) {
	counter := &countingCounter{verdict: allowed(5)}

	serveLimited(counter, (&ran{}).handler(), post("", `{}`))

	if len(counter.asked) != 1 {
		t.Fatalf("the counter was consulted %d times, want 1", len(counter.asked))
	}
	if counter.asked[0] != testClient {
		t.Errorf("counted against %q, want the client id %q", counter.asked[0], testClient)
	}
}

// Counted regardless of what the handler answers. Counting only successes would
// let an enumeration run — which is mostly 404s — proceed unbounded, and that
// is precisely the shape a stolen credential takes.
func TestAFailingRequestIsStillCounted(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			counter := &countingCounter{verdict: allowed(5)}
			handler := &ran{status: status, response: `{}`}

			serveLimited(counter, handler.handler(), post("", `{}`))

			if len(counter.asked) != 1 {
				t.Errorf("a %d response was counted %d times, want 1", status, len(counter.asked))
			}
		})
	}
}

// Without a caller the request is refused, not counted and not run. Running
// would mean an unauthenticated request reached a handler; counting it would
// mean charging an empty client id.
func TestWithoutACallerTheRequestIsNotCounted(t *testing.T) {
	counter := &countingCounter{verdict: allowed(5)}
	handler := &ran{}
	r := httptest.NewRequest(http.MethodPost, "/v1/users", strings.NewReader(`{}`))

	w := serveLimited(counter, handler.handler(), r)

	if handler.reached != 0 {
		t.Error("a request with no caller reached the handler")
	}
	if len(counter.asked) != 0 {
		t.Errorf("the counter was consulted for a request with no caller: %v", counter.asked)
	}
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

// --- the order against idempotency ----------------------------------------------------------

// A replay is still a request, and is still counted. Serving one from the
// record for free would leave a client able to hammer a single key without
// limit — and "free" is the property a bound exists to remove.
func TestAReplayIsCountedToo(t *testing.T) {
	counter := &countingCounter{verdict: allowed(5)}
	claims := &scriptedClaims{
		replay: &Replay{Status: 201, Response: []byte(`{"id":"abc"}`)},
	}
	handler := &ran{response: `{"id":"SECOND"}`}

	// The production order: rate limit outside, idempotency inside.
	idem := &Idempotency{Claims: claims, Now: func() time.Time { return at(0) }}
	w := serveLimited(counter, idem.Wrap(handler.handler()), post("key-1", `{}`))

	if handler.reached != 0 {
		t.Fatal("the replay ran the handler")
	}
	if w.Code != 201 {
		t.Errorf("status = %d, want the stored 201", w.Code)
	}
	if len(counter.asked) != 1 {
		t.Errorf("the replay was counted %d times, want 1", len(counter.asked))
	}
}

// And a request refused by the bound never reaches idempotency, so a 429 does
// not claim a key the caller will want to reuse once the window turns over.
func TestARefusedRequestDoesNotClaimAKey(t *testing.T) {
	counter := &countingCounter{verdict: refused()}
	claims := &scriptedClaims{}
	handler := &ran{}

	idem := &Idempotency{Claims: claims, Now: func() time.Time { return at(0) }}
	serveLimited(counter, idem.Wrap(handler.handler()), post("key-1", `{}`))

	if claims.beginWith.key != "" {
		t.Errorf("a 429 claimed the key %q, which the caller can no longer reuse",
			claims.beginWith.key)
	}
	if len(claims.released) != 0 {
		t.Error("a 429 released a claim it never made")
	}
}
