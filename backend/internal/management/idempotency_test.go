package management

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The split between this file and the integration test is deliberate.
//
// Here, Claims is SCRIPTED: each test says what Begin answers and then checks
// what the middleware does about it. That is the only way to reach a panicking
// handler, an oversized response or a failing Complete, and it keeps the
// middleware's behaviour from being asserted through a fake that quietly
// implements the semantics itself.
//
// The semantics — that a second identical request really does replay, that a
// different body really is a conflict, that a concurrent duplicate really is
// refused — are proved against PostgreSQL in idempotency_pg_test.go. Proving
// them here against an in-memory imitation would prove the imitation.

type call struct {
	orgID, clientID, key string
	status               int
	body                 []byte
}

type scriptedClaims struct {
	replay    *Replay
	beginErr  error
	beginWith call

	completed []call
	completeE error

	released []call
	releaseE error
}

func (s *scriptedClaims) Begin(
	_ context.Context, orgID, clientID, key, _, _ string, body []byte, _ time.Time,
) (*Replay, error) {
	s.beginWith = call{orgID: orgID, clientID: clientID, key: key, body: body}
	return s.replay, s.beginErr
}

func (s *scriptedClaims) Complete(_ context.Context, orgID, clientID, key string, status int, body []byte) error {
	s.completed = append(s.completed, call{orgID, clientID, key, status, body})
	return s.completeE
}

func (s *scriptedClaims) Release(orgID, clientID, key string) error {
	s.released = append(s.released, call{orgID: orgID, clientID: clientID, key: key})
	return s.releaseE
}

// --- fixtures -----------------------------------------------------------------------

const (
	testOrg    = "11111111-1111-1111-1111-111111111111"
	testClient = "22222222-2222-2222-2222-222222222222"
)

func withCaller(r *http.Request, c Caller) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), callerKey{}, c))
}

func post(key, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/v1/users", strings.NewReader(body))
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	return withCaller(r, Caller{UserID: "u", ClientID: testClient, OrgID: testOrg})
}

// ran records whether the wrapped handler was reached, which is the property
// every one of these tests is really about.
type ran struct {
	reached  int
	body     string
	status   int
	response string
	panics   bool
}

func (h *ran) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.reached++

		read := make([]byte, 4096)
		n, _ := r.Body.Read(read)
		h.body = string(read[:n])

		if h.panics {
			panic("the handler blew up")
		}
		if h.status != 0 {
			w.WriteHeader(h.status)
		}
		_, _ = w.Write([]byte(h.response))
	})
}

func serve(t *testing.T, claims Claims, h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	mw := &Idempotency{Claims: claims, Now: func() time.Time { return at(0) }}
	w := httptest.NewRecorder()
	mw.Wrap(h).ServeHTTP(w, r)
	return w
}

// --- when the header is absent ------------------------------------------------------

// No key, no promise, and no row. A middleware that claimed a key for every
// request would put a row in the table for every write in the estate.
func TestWithoutAKeyNothingIsRecorded(t *testing.T) {
	claims := &scriptedClaims{}
	handler := &ran{response: `{"ok":true}`}

	w := serve(t, claims, handler.handler(), post("", `{"a":1}`))

	if handler.reached != 1 {
		t.Errorf("the handler ran %d times, want 1", handler.reached)
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d", w.Code)
	}
	if claims.beginWith.key != "" || len(claims.completed) != 0 {
		t.Error("a request with no Idempotency-Key touched the store")
	}
}

// A key on a GET is ignored rather than honoured. Honouring it would cache a
// permission-dependent read under a name the caller chose — and the caller
// choosing the name is what makes that dangerous.
func TestAKeyOnAReadIsIgnored(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			claims := &scriptedClaims{
				replay: &Replay{Status: 200, Response: json.RawMessage(`{"stale":true}`)},
			}
			handler := &ran{response: `{"fresh":true}`}

			r := httptest.NewRequest(method, "/v1/users", nil)
			r.Header.Set("Idempotency-Key", "k")
			r = withCaller(r, Caller{ClientID: testClient, OrgID: testOrg})

			w := serve(t, claims, handler.handler(), r)

			if handler.reached != 1 {
				t.Errorf("the handler ran %d times, want 1", handler.reached)
			}
			if strings.Contains(w.Body.String(), "stale") {
				t.Error("a read was answered from an idempotency record")
			}
		})
	}
}

// --- the first request --------------------------------------------------------------

func TestAFirstRequestRunsAndIsStored(t *testing.T) {
	claims := &scriptedClaims{}
	handler := &ran{status: http.StatusCreated, response: `{"id":"abc"}`}

	w := serve(t, claims, handler.handler(), post("key-1", `{"email":"a@b.c"}`))

	if handler.reached != 1 {
		t.Fatalf("the handler ran %d times, want 1", handler.reached)
	}
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", w.Code)
	}

	if len(claims.completed) != 1 {
		t.Fatalf("%d responses stored, want 1", len(claims.completed))
	}
	got := claims.completed[0]
	if got.status != http.StatusCreated || string(got.body) != `{"id":"abc"}` {
		t.Errorf("stored %d %q", got.status, got.body)
	}
	// Keyed by the CALLER's organization and client, not by the key alone —
	// which is what stops one client reading another's stored response.
	if got.orgID != testOrg || got.clientID != testClient || got.key != "key-1" {
		t.Errorf("stored against %+v", got)
	}
	if len(claims.released) != 0 {
		t.Error("a stored claim was also released")
	}
}

// The handler must still be able to read the body the middleware consumed.
// This is the failure that turns replay protection into "every keyed request
// sees an empty body", and it is invisible until a handler needs a field.
func TestTheHandlerStillSeesTheBody(t *testing.T) {
	handler := &ran{response: `{}`}

	serve(t, &scriptedClaims{}, handler.handler(), post("key-1", `{"email":"a@b.c"}`))

	if handler.body != `{"email":"a@b.c"}` {
		t.Errorf("the handler read %q", handler.body)
	}
}

// --- the replay -----------------------------------------------------------------------

func TestAReplayReturnsTheStoredAnswerAndRunsNothing(t *testing.T) {
	claims := &scriptedClaims{
		replay: &Replay{Status: http.StatusCreated, Response: json.RawMessage(`{"id":"abc"}`)},
	}
	handler := &ran{response: `{"id":"SECOND"}`}

	w := serve(t, claims, handler.handler(), post("key-1", `{"email":"a@b.c"}`))

	// The whole point: the second call did not create a second user.
	if handler.reached != 0 {
		t.Fatalf("the handler ran %d times on a replay", handler.reached)
	}
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want the stored 201", w.Code)
	}
	if w.Body.String() != `{"id":"abc"}` {
		t.Errorf("body = %q, want the stored answer", w.Body.String())
	}
	if got := w.Header().Get("Idempotency-Replayed"); got != "true" {
		t.Errorf("Idempotency-Replayed = %q — a client cannot tell a replay from a run", got)
	}
	if len(claims.completed) != 0 {
		t.Error("a replay stored a response over the original")
	}
}

// A replay must not be cached by anything in between. It is per-caller and
// permission-dependent, exactly like every other management response.
func TestAReplayIsNotCacheable(t *testing.T) {
	claims := &scriptedClaims{replay: &Replay{Status: 200, Response: json.RawMessage(`{}`)}}

	w := serve(t, claims, (&ran{}).handler(), post("key-1", `{}`))

	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

// --- the refusals -----------------------------------------------------------------------

// A conflict from the store stops the handler. If the refusal were written
// after the handler ran, the caller would get a 409 describing a side effect
// that had already happened.
func TestAConflictNeverReachesTheHandler(t *testing.T) {
	claims := &scriptedClaims{beginErr: Fault{
		Class: Conflict, Message: "This Idempotency-Key was already used for a different request.",
		Reason: "request hash mismatch",
	}}
	handler := &ran{}

	w := serve(t, claims, handler.handler(), post("key-1", `{"email":"different"}`))

	if handler.reached != 0 {
		t.Fatalf("the handler ran %d times behind a conflict", handler.reached)
	}
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
	if len(claims.released) != 0 {
		t.Error("a claim nobody made was released — that would drop the FIRST request's record")
	}
}

// The reason belongs in the log. The response says a key was reused; it does
// not say what the first request was.
func TestAConflictDoesNotDescribeTheFirstRequest(t *testing.T) {
	claims := &scriptedClaims{beginErr: Fault{
		Class: Conflict, Message: "This Idempotency-Key was already used for a different request.",
		Reason: "request hash mismatch: stored 9f86d0818, presented 6b86b273f",
	}}

	w := serve(t, claims, (&ran{}).handler(), post("key-1", `{}`))

	if strings.Contains(w.Body.String(), "9f86d0818") {
		t.Error("the refusal leaks the stored request's fingerprint")
	}
}

// The middleware runs inside Require, so a caller is always present. If one is
// not, the route was registered wrong — and the answer is an error, not a run.
// Running would mean an unauthenticated request reached a mutating handler.
func TestWithoutACallerTheHandlerDoesNotRun(t *testing.T) {
	handler := &ran{}
	r := httptest.NewRequest(http.MethodPost, "/v1/users", strings.NewReader(`{}`))
	r.Header.Set("Idempotency-Key", "key-1")

	w := serve(t, &scriptedClaims{}, handler.handler(), r)

	if handler.reached != 0 {
		t.Fatal("a request with no caller reached the handler")
	}
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

// Without a client there is no per-client namespace, and every caller in an
// organization would share one. Refused rather than keyed on the org alone.
func TestATokenWithNoClientCannotUseAKey(t *testing.T) {
	handler := &ran{}
	r := post("key-1", `{}`)
	r = withCaller(r, Caller{UserID: "u", OrgID: testOrg}) // no ClientID

	w := serve(t, &scriptedClaims{}, handler.handler(), r)

	if handler.reached != 0 {
		t.Fatal("the handler ran")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// --- releasing the claim --------------------------------------------------------------

// An error is not stored, and the claim is released. A held claim would refuse
// the caller's corrected retry as a conflict for twenty-four hours — leaving
// them unable to fix their own mistake.
func TestAFailedRequestIsRetryable(t *testing.T) {
	for _, status := range []int{
		http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound,
		http.StatusUnprocessableEntity, http.StatusInternalServerError,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			claims := &scriptedClaims{}
			handler := &ran{status: status, response: `{"error":{}}`}

			w := serve(t, claims, handler.handler(), post("key-1", `{}`))

			if w.Code != status {
				t.Errorf("status = %d, want %d", w.Code, status)
			}
			if len(claims.completed) != 0 {
				t.Error("an error response was stored for replay")
			}
			if len(claims.released) != 1 {
				t.Errorf("the claim was released %d times, want 1 — the retry is now refused",
					len(claims.released))
			}
		})
	}
}

// A panic must not leave a claim behind. It is the case a `defer` exists for,
// and the one an author is most likely to get wrong by releasing on the normal
// path only.
func TestAPanicReleasesTheClaim(t *testing.T) {
	claims := &scriptedClaims{}
	handler := &ran{panics: true}

	func() {
		defer func() { _ = recover() }()
		serve(t, claims, handler.handler(), post("key-1", `{}`))
	}()

	if len(claims.released) != 1 {
		t.Errorf("the claim was released %d times after a panic, want 1", len(claims.released))
	}
	if len(claims.completed) != 0 {
		t.Error("a panicking handler stored a response")
	}
}

// If the record cannot be written, the claim goes too. Leaving it would refuse
// every retry while there is nothing stored to replay — the worst of both.
func TestAFailedStoreReleasesTheClaim(t *testing.T) {
	claims := &scriptedClaims{completeE: errors.New("the database went away")}
	handler := &ran{status: http.StatusCreated, response: `{"id":"abc"}`}

	w := serve(t, claims, handler.handler(), post("key-1", `{}`))

	// The caller still gets the answer: the work was done.
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", w.Code)
	}
	if len(claims.released) != 1 {
		t.Errorf("the claim was released %d times, want 1", len(claims.released))
	}
}

// --- what is worth storing --------------------------------------------------------------

// An oversized response is answered in FULL and simply not stored. Truncating
// the answer to fit a replay budget would corrupt the request that actually
// ran, which is a much worse trade than making a retry re-run.
func TestAnOversizedResponseIsAnsweredInFullAndNotStored(t *testing.T) {
	big := `{"d":"` + strings.Repeat("x", maxStoredResponse) + `"}`
	claims := &scriptedClaims{}
	handler := &ran{status: http.StatusOK, response: big}

	w := serve(t, claims, handler.handler(), post("key-1", `{}`))

	if w.Body.Len() != len(big) {
		t.Errorf("the client received %d bytes of %d", w.Body.Len(), len(big))
	}
	if len(claims.completed) != 0 {
		t.Error("an oversized response was stored, and would replay truncated")
	}
	if len(claims.released) != 1 {
		t.Error("the claim was not released, so the retry is refused with nothing to replay")
	}
}

// An oversized REQUEST is refused before the handler runs. The body has to be
// buffered to be hashed, so an unbounded one is a memory ask.
func TestAnOversizedRequestIsRefused(t *testing.T) {
	handler := &ran{}
	r := post("key-1", `{"d":"`+strings.Repeat("x", maxIdempotentBody+1)+`"}`)

	w := serve(t, &scriptedClaims{}, handler.handler(), r)

	if handler.reached != 0 {
		t.Fatal("an oversized body reached the handler")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// A handler that writes a body with no explicit status is a 200, not a 0. A
// stored status of 0 would replay as an unparseable response forever.
func TestAnImplicitStatusIsStoredAsTwoHundred(t *testing.T) {
	claims := &scriptedClaims{}
	handler := &ran{response: `{"ok":true}`} // no WriteHeader

	serve(t, claims, handler.handler(), post("key-1", `{}`))

	if len(claims.completed) != 1 {
		t.Fatalf("%d responses stored", len(claims.completed))
	}
	if got := claims.completed[0].status; got != http.StatusOK {
		t.Errorf("stored status = %d, want 200", got)
	}
}

// The column is jsonb. A handler answering 200 with something that is not JSON
// must not have it stored, or the write fails and the claim is stranded.
func TestANonJSONSuccessIsNotStored(t *testing.T) {
	claims := &scriptedClaims{}
	handler := &ran{status: http.StatusOK, response: "not json at all"}

	serve(t, claims, handler.handler(), post("key-1", `{}`))

	if len(claims.completed) != 0 {
		t.Error("a non-JSON body was stored into a jsonb column")
	}
}

// --- the hash --------------------------------------------------------------------------

// The same request hashes the same way, which is what makes a replay a replay.
func TestTheSameRequestHashesTheSame(t *testing.T) {
	a := HashRequest(http.MethodPost, "/v1/users", []byte(`{"a":1}`))
	b := HashRequest(http.MethodPost, "/v1/users", []byte(`{"a":1}`))
	if a != b {
		t.Error("two identical requests hashed differently — nothing would ever replay")
	}
}

// Each component changes the fingerprint. A key reused across two endpoints is
// a different request even when the bodies match, and returning the first
// endpoint's answer for the second would be an invented result.
func TestEveryComponentOfARequestChangesItsHash(t *testing.T) {
	base := HashRequest(http.MethodPost, "/v1/users", []byte(`{"a":1}`))

	for name, got := range map[string]string{
		"method": HashRequest(http.MethodPatch, "/v1/users", []byte(`{"a":1}`)),
		"path":   HashRequest(http.MethodPost, "/v1/orgs", []byte(`{"a":1}`)),
		"body":   HashRequest(http.MethodPost, "/v1/users", []byte(`{"a":2}`)),
	} {
		if got == base {
			t.Errorf("a different %s produced the same hash", name)
		}
	}
}

// The separators matter, and the pairs below are chosen so that they are the
// ONLY thing separating the two requests: concatenated without a delimiter,
// each pair produces identical bytes. A first attempt at this test used
// "POST"+"/v1/usersX" against "POSTX"+"/v1/users", which differ concatenated
// too — so it passed with the separators removed and proved nothing.
func TestAdjacentComponentsCannotBeConfused(t *testing.T) {
	pairs := []struct {
		name string
		a, b [3]string
	}{
		{
			"method into path",
			[3]string{"POST", "X/v1/users", ""},
			[3]string{"POSTX", "/v1/users", ""},
		},
		{
			"path into body",
			[3]string{"POST", `/v1/users`, `{"a":1}`},
			[3]string{"POST", `/v1/users{"a":1}`, ``},
		},
	}

	for _, p := range pairs {
		t.Run(p.name, func(t *testing.T) {
			a := HashRequest(p.a[0], p.a[1], []byte(p.a[2]))
			b := HashRequest(p.b[0], p.b[1], []byte(p.b[2]))
			if a == b {
				t.Error("two different requests hash the same — the components run together")
			}
		})
	}
}

// --- the key ------------------------------------------------------------------------

func TestAnUnusableKeyIsRefused(t *testing.T) {
	for name, key := range map[string]string{
		"empty":       "",
		"too long":    strings.Repeat("k", maxKeyLength+1),
		"a newline":   "abc\ndef",
		"a null byte": "abc\x00def",
		"a tab":       "abc\tdef",
		"a space":     "abc def",
		"non-ASCII":   "abcé",
	} {
		t.Run(name, func(t *testing.T) {
			err := ValidateKey(key)
			if err == nil {
				t.Fatal("the key was accepted")
			}
			var fault Fault
			if !errors.As(err, &fault) {
				t.Fatalf("err is %T, not a Fault", err)
			}
			if fault.Class != Invalid {
				t.Errorf("class = %v, want Invalid", fault.Class)
			}
			// The key is caller-supplied. Echoing it into the response would
			// reflect whatever they sent onto whatever renders the error.
			if key != "" && strings.Contains(fault.Message, key) {
				t.Error("the refusal echoes the key back")
			}
		})
	}
}

// A newline in a key would let a caller forge log lines, since the key is
// logged on every refusal. This is the specific reason for the ASCII rule.
func TestAKeyCannotForgeALogLine(t *testing.T) {
	forged := "abc\nlevel=ERROR msg=\"tenant isolation disabled\""
	if err := ValidateKey(forged); err == nil {
		t.Fatal("a key carrying a newline was accepted, and it is logged verbatim")
	}
}

func TestAnOrdinaryKeyIsAccepted(t *testing.T) {
	for _, key := range []string{
		"a", "01234567-89ab-cdef-0123-456789abcdef",
		strings.Repeat("k", maxKeyLength), "req_2026-09-10T12:00:00Z#7",
	} {
		if err := ValidateKey(key); err != nil {
			t.Errorf("ValidateKey(%q) = %v", key, err)
		}
	}
}
