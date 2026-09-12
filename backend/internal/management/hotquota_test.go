package management

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// The high-volume routes are counted separately (P2-17).
//
// `/v1/authz/check` is called by a consumer on every protected request, and
// `P2-17`'s load test found it refusing 19,658 of 40,515 requests at 20
// workers — because it shared the Management API's bound, which was sized for
// "a console session and a provisioning script" and says so in its own comment.
//
// Two things have to hold for a second bound to mean anything, and the second
// is the one that is easy to get wrong:
//
//  1. the route uses it;
//  2. it has its own counter. Two quotas sharing a key share an allowance, so
//     the looser one is spent by the traffic the tighter one governs — which
//     is the same as not having two.

// routeRequest builds a request carrying the chi route pattern the middleware
// reads, and an authenticated caller — without which the middleware refuses
// before it ever picks a counter.
func routeRequest(method, pattern string) *http.Request {
	r := httptest.NewRequest(method, "/v1/whatever", nil)
	rc := chi.NewRouteContext()
	rc.RoutePatterns = []string{pattern}
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
	return r.WithContext(context.WithValue(r.Context(), callerKey{},
		Caller{UserID: "u1", ClientID: "c1"}))
}

func TestAHotRouteIsCountedAgainstItsOwnBound(t *testing.T) {
	ordinary := &countingCounter{verdict: allowed(599)}
	hot := &countingCounter{verdict: allowed(5999)}

	rl := &RateLimit{Counter: ordinary, HotCounter: hot}
	handler := rl.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), routeRequest(http.MethodPost, "/v1/authz/check"))

	if len(hot.asked) != 1 {
		t.Errorf("the authorization check was not counted against the hot bound (%d calls)", len(hot.asked))
	}
	if len(ordinary.asked) != 0 {
		t.Errorf("the authorization check was ALSO counted against the ordinary bound (%d calls) — "+
			"two counters sharing traffic is the same as one", len(ordinary.asked))
	}
}

func TestAnOrdinaryRouteIsNotGivenTheLooserBound(t *testing.T) {
	ordinary := &countingCounter{verdict: allowed(599)}
	hot := &countingCounter{verdict: allowed(5999)}

	rl := &RateLimit{Counter: ordinary, HotCounter: hot}
	handler := rl.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// The routes an administrator's tooling walks. Every one of them must stay
	// on the tighter bound, or the fix has quietly widened the whole API.
	for _, pattern := range []string{
		"/v1/organizations",
		"/v1/organizations/{org_id}",
		"/v1/organizations/{org_id}/users",
		"/v1/organizations/{org_id}/projects/{project_id}/roles",
		"/v1/organizations/{org_id}/users/{user_id}/grants",
	} {
		before := len(hot.asked)
		handler.ServeHTTP(httptest.NewRecorder(), routeRequest(http.MethodGet, pattern))
		if len(hot.asked) != before {
			t.Errorf("%s was given the hot bound", pattern)
		}
	}

	if len(ordinary.asked) != 5 {
		t.Errorf("the ordinary bound counted %d of 5 management requests", len(ordinary.asked))
	}
}

// Without a hot counter configured, every route uses the ordinary one.
//
// That is what every deployment did before Phase 2 had an endpoint on a
// consumer's hot path, and a nil field must not mean "unbounded".
func TestWithNoHotCounterEveryRouteUsesTheOrdinaryBound(t *testing.T) {
	ordinary := &countingCounter{verdict: allowed(599)}

	rl := &RateLimit{Counter: ordinary}
	handler := rl.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), routeRequest(http.MethodPost, "/v1/authz/check"))

	if len(ordinary.asked) != 1 {
		t.Errorf("with no hot counter, the authorization check was counted %d times "+
			"against the ordinary bound — a nil field must not mean unbounded", len(ordinary.asked))
	}
}

// Every route named in HotRoutes is a route that exists.
//
// A stale entry is a looser bound aimed at nothing, and — worse — it reads
// like the endpoint it names is still being protected differently.
func TestEveryHotRouteIsInThePolicyTable(t *testing.T) {
	if len(HotRoutes) == 0 {
		t.Fatal("HotRoutes is empty, so this test proves nothing")
	}

	for key := range HotRoutes {
		if _, declared := Policy[key]; !declared {
			t.Errorf("HotRoutes names %q, which management.Policy does not — "+
				"so it is either a typo or an endpoint that no longer exists", key)
		}
	}
}
