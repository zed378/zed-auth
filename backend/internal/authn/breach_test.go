package authn

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// corpus starts a stub range API and returns a Client pointed at it, along
// with a pointer to the last request it received.
//
// A stub rather than the live service. A test that depends on a third party is
// a test that fails on their bad day, and a suite whose failures nobody
// believes is worse than a slower one (P1-03's lesson). The live service is
// verified by hand from the VM and recorded.
func corpus(t *testing.T, handler http.HandlerFunc) (*Client, *http.Request) {
	t.Helper()

	var seen http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = *r.Clone(r.Context())
		handler(w, r)
	}))
	t.Cleanup(server.Close)

	return &Client{Endpoint: server.URL + "/range/", HTTP: server.Client()}, &seen
}

// suffixes builds a range response body in the service's format.
//
// Each entry is padded to the real suffix length, because the parser requires
// well-formed entries and a hand-counted string of zeros is the kind of
// fixture that fails for a reason unrelated to what the test is about.
func suffixes(entries ...string) string {
	var b strings.Builder
	for i, e := range entries {
		if len(e) > suffixLength {
			panic("test fixture: suffix longer than a real one")
		}
		fmt.Fprintf(&b, "%s:%d\r\n", e+strings.Repeat("0", suffixLength-len(e)), (i+1)*100)
	}
	return b.String()
}

// P1-02 DoD item 2: the check transmits only a hash prefix, verified by an
// outbound-request test.
//
// The important half is the negative: the request must contain nothing derived
// from the password beyond those five characters. Checking only that the
// prefix is right would pass against an implementation that also sent the
// password in a header.
func TestBreachRequestCarriesOnlyAPrefix(t *testing.T) {
	const password = "correct horse battery staple"
	prefix, suffix := hashPrefixSuffix(password)

	client, seen := corpus(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, suffixes("0"))
	})

	if _, err := client.Breached(context.Background(), password); err != nil {
		t.Fatalf("Breached: %v", err)
	}

	if got := strings.TrimPrefix(seen.URL.Path, "/range/"); got != prefix {
		t.Errorf("requested %q, want the %d-character prefix %q", got, prefixLength, prefix)
	}
	if len(prefix) != prefixLength {
		t.Errorf("prefix is %d characters, want %d", len(prefix), prefixLength)
	}

	// Everything the request could possibly carry, flattened.
	var carried strings.Builder
	carried.WriteString(seen.URL.String())
	for name, values := range seen.Header {
		carried.WriteString(name)
		carried.WriteString(strings.Join(values, " "))
	}
	if seen.Body != nil {
		body, _ := io.ReadAll(seen.Body)
		carried.Write(body)
	}
	request := carried.String()

	for _, forbidden := range []struct{ what, value string }{
		{"the password", password},
		{"the password's suffix", suffix},
		{"the full hash", prefix + suffix},
	} {
		if strings.Contains(strings.ToUpper(request), strings.ToUpper(forbidden.value)) {
			t.Errorf("the outbound request contains %s; only the %d-character prefix may leave this process",
				forbidden.what, prefixLength)
		}
	}
}

// A control on the test above: it has to be able to fail.
//
// If the assertion cannot detect a password in the request, it is not
// verifying anything — the vacuous-check failure this project keeps finding.
func TestTheOutboundAssertionWouldCatchALeak(t *testing.T) {
	const password = "correct horse battery staple"
	prefix, suffix := hashPrefixSuffix(password)

	// A request shaped the way a leaky implementation would build one.
	leaky := "/range/" + prefix + "?full=" + prefix + suffix

	if !strings.Contains(strings.ToUpper(leaky), strings.ToUpper(prefix+suffix)) {
		t.Error("the containment check used by TestBreachRequestCarriesOnlyAPrefix " +
			"cannot detect a full hash in a request; that test proves nothing")
	}
	if strings.Contains(strings.ToUpper("/range/"+prefix), strings.ToUpper(suffix)) {
		t.Error("the containment check reports a leak in a correct request; " +
			"it would fail on everything and therefore mean nothing")
	}
}

func TestBreachedRecognisesAMatch(t *testing.T) {
	const password = "hunter2hunter2"
	_, suffix := hashPrefixSuffix(password)

	client, _ := corpus(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, suffixes(
			"0",
			suffix,
			"F",
		))
	})

	breached, err := client.Breached(context.Background(), password)
	if err != nil {
		t.Fatalf("Breached: %v", err)
	}
	if !breached {
		t.Error("a password whose suffix is in the response was reported clean")
	}
}

func TestBreachedRecognisesNoMatch(t *testing.T) {
	client, _ := corpus(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, suffixes(
			"0",
			"1",
		))
	})

	breached, err := client.Breached(context.Background(), "a password not in this list")
	if err != nil {
		t.Fatalf("Breached: %v", err)
	}
	if breached {
		t.Error("a password absent from the response was reported breached")
	}
}

// The suffix comparison is case-insensitive: the service returns uppercase and
// a future one might not.
func TestBreachedMatchIsCaseInsensitive(t *testing.T) {
	const password = "hunter2hunter2"
	_, suffix := hashPrefixSuffix(password)

	client, _ := corpus(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, suffixes(strings.ToLower(suffix)))
	})

	breached, err := client.Breached(context.Background(), password)
	if err != nil {
		t.Fatalf("Breached: %v", err)
	}
	if !breached {
		t.Error("a lowercase suffix in the response was not matched")
	}
}

// Every way the service can fail must produce ErrBreachServiceUnavailable and
// never a clean answer — because CheckBreach turns "clean" into an accepted
// password and an error into a recorded skip.
func TestBreachServiceFailuresAreDistinguishable(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"rate limited", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}},
		{"server error", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}},
		{"not found", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}},
		// A captive portal or a misconfigured proxy answers 200 with something
		// that is not a range response. Every prefix in this corpus has
		// hundreds of suffixes, so an empty body is not "no matches" — reading
		// it as clean would be a silent always-open.
		{"empty 200", func(w http.ResponseWriter, r *http.Request) {}},
		{"an HTML error page with a 200", func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, "<html><body>Access denied</body></html>\n")
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := corpus(t, tc.handler)

			breached, err := client.Breached(context.Background(), "any password")
			if !errors.Is(err, ErrBreachServiceUnavailable) {
				t.Errorf("error = %v, want ErrBreachServiceUnavailable — a service failure "+
					"must never be indistinguishable from an answer", err)
			}
			if breached {
				t.Error("a failure reported the password as breached; the boolean is only meaningful when err is nil")
			}
		})
	}
}

// A response whose lines contain colons but no well-formed suffix is a service
// failure, not "no matches".
//
// This is the case that first shipped wrong. The check counted lines, and an
// error page is one line — one line is not zero — so a proxy's
// "Error: service unavailable" was read as a clean answer and the password was
// admitted with OutcomeClean. An always-open path that no metric would show.
func TestBreachedRejectsAResponseWithNoWellFormedEntries(t *testing.T) {
	client, _ := corpus(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "Error: service unavailable\nRetry: later\n")
	})

	breached, err := client.Breached(context.Background(), "hunter2hunter2")
	if !errors.Is(err, ErrBreachServiceUnavailable) {
		t.Errorf("error = %v, want ErrBreachServiceUnavailable — a body with no range "+
			"entries has not answered the question, and reading it as clean is a silent always-open", err)
	}
	if breached {
		t.Error("a non-range response reported the password as breached")
	}
}

// Junk mixed into a genuine range response is skipped rather than fatal: that
// response did answer the question.
func TestBreachedSkipsJunkLinesInARealResponse(t *testing.T) {
	const password = "hunter2hunter2"
	_, suffix := hashPrefixSuffix(password)

	client, _ := corpus(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "X-Note: served from cache\n"+suffixes("0", suffix))
	})

	breached, err := client.Breached(context.Background(), password)
	if err != nil {
		t.Fatalf("Breached: %v", err)
	}
	if !breached {
		t.Error("a match in an otherwise valid response was missed")
	}
}

// A suffix of the wrong length is not an entry, so a body made only of them
// has answered nothing.
func TestBreachedRejectsMalformedSuffixes(t *testing.T) {
	client, _ := corpus(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ABC:1\nDEADBEEF:2\n")
	})

	if _, err := client.Breached(context.Background(), "any password"); !errors.Is(err, ErrBreachServiceUnavailable) {
		t.Errorf("error = %v, want ErrBreachServiceUnavailable", err)
	}
}

func TestBreachedHonoursContextCancellation(t *testing.T) {
	client, _ := corpus(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := client.Breached(ctx, "any password"); !errors.Is(err, ErrBreachServiceUnavailable) {
		t.Errorf("error = %v, want ErrBreachServiceUnavailable", err)
	}
}

// --- the fail-open decision (ADR-015) --------------------------------------

type stubChecker struct {
	breached bool
	err      error
}

func (s stubChecker) Breached(context.Context, string) (bool, error) {
	return s.breached, s.err
}

// ADR-015: a service failure admits the password, and says so.
//
// Both halves are asserted. Accepting the password without producing
// OutcomeSkipped would be an always-open policy that no dashboard could
// distinguish from a working one.
func TestCheckBreachFailsOpenAndSaysSo(t *testing.T) {
	violations, outcome, err := CheckBreach(context.Background(),
		stubChecker{err: ErrBreachServiceUnavailable}, "any password")

	if len(violations) != 0 {
		t.Errorf("violations = %v; a service failure must not reject the password", rules(violations))
	}
	if outcome != OutcomeSkipped {
		t.Errorf("outcome = %q, want %q — a fail-open nobody can see is indistinguishable "+
			"from a breach check that was never wired up", outcome, OutcomeSkipped)
	}
	if err == nil {
		t.Error("the error was swallowed; the caller needs it to log the reason")
	}
}

// A definitive answer is always a rejection. Fail-open covers service failure
// only.
func TestCheckBreachRejectsAKnownBreachedPassword(t *testing.T) {
	violations, outcome, err := CheckBreach(context.Background(),
		stubChecker{breached: true}, "any password")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome != OutcomeBreached {
		t.Errorf("outcome = %q, want %q", outcome, OutcomeBreached)
	}
	if got := rules(violations); !equal(got, []string{RuleBreached}) {
		t.Errorf("violations = %v, want %v", got, []string{RuleBreached})
	}
}

func TestCheckBreachPassesACleanPassword(t *testing.T) {
	violations, outcome, err := CheckBreach(context.Background(),
		stubChecker{breached: false}, "any password")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome != OutcomeClean {
		t.Errorf("outcome = %q, want %q", outcome, OutcomeClean)
	}
	if len(violations) != 0 {
		t.Errorf("violations = %v, want none", rules(violations))
	}
}

// Disabling the check is a distinct outcome, not an absence of data. It shows
// up in the same metric as a failure, so "somebody turned it off" and "it has
// been broken for three weeks" are both visible in the same place.
func TestCheckBreachDisabledIsItsOwnOutcome(t *testing.T) {
	_, outcome, err := CheckBreach(context.Background(), nil, "any password")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome != OutcomeDisabled {
		t.Errorf("outcome = %q, want %q", outcome, OutcomeDisabled)
	}
}

// The three accepting outcomes must be three distinct values.
//
// Conflating any two of them is exactly how the fail-open decision stops being
// defensible: the metric that justifies it can no longer tell whether a
// password was checked.
func TestAcceptingOutcomesAreDistinct(t *testing.T) {
	seen := map[Outcome]bool{}
	for _, o := range []Outcome{OutcomeClean, OutcomeBreached, OutcomeSkipped, OutcomeDisabled} {
		if o == "" {
			t.Error("an outcome is the empty string; it would be indistinguishable from an unset label")
		}
		if seen[o] {
			t.Errorf("outcome %q is duplicated", o)
		}
		seen[o] = true
	}
}

// The password never reaches the corpus service, so it must not reach an error
// string either — errors travel to logs, traces and responses.
func TestBreachErrorsCarryNoPasswordMaterial(t *testing.T) {
	const password = "correct horse battery staple"
	prefix, suffix := hashPrefixSuffix(password)

	client, _ := corpus(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})

	_, err := client.Breached(context.Background(), password)
	if err == nil {
		t.Fatal("expected an error")
	}

	text := strings.ToUpper(err.Error())
	for _, forbidden := range []struct{ what, value string }{
		{"the password", password},
		{"the hash suffix", suffix},
		{"the hash prefix", prefix},
	} {
		if strings.Contains(text, strings.ToUpper(forbidden.value)) {
			t.Errorf("the error message contains %s: %q", forbidden.what, err.Error())
		}
	}
}
