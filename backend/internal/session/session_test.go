package session

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- the token ---------------------------------------------------------------

func TestTokenEntropy(t *testing.T) {
	token, hash, err := NewToken()
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}

	decoded, err := base64.RawURLEncoding.DecodeString(token.Reveal())
	if err != nil {
		t.Fatalf("the token is not base64url: %v", err)
	}
	if len(decoded) != tokenBytes {
		t.Errorf("token carries %d bytes, want %d — storing only a fast hash "+
			"stops being defensible below this", len(decoded), tokenBytes)
	}
	if tokenBytes < 32 {
		t.Errorf("tokenBytes is %d; ADR-016's argument needs at least 32", tokenBytes)
	}
	if hash == token.Reveal() || strings.Contains(hash, token.Reveal()) {
		t.Fatal("the stored hash contains the token")
	}
}

func TestNewTokenIsNotDeterministic(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		token, _, err := NewToken()
		if err != nil {
			t.Fatalf("NewToken: %v", err)
		}
		if seen[token.Reveal()] {
			t.Fatal("NewToken returned a duplicate")
		}
		seen[token.Reveal()] = true
	}
}

// P1-05 found that fmt.Stringer leaks under %d, because fmt consults String
// only for some verbs and prints struct fields for the rest. The same trap
// applies to any redacting type, so the same enumeration guards this one.
func TestTokenDoesNotPrintItself(t *testing.T) {
	token, _, err := NewToken()
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	plaintext := token.Reveal()

	for _, format := range []string{"%s", "%v", "%q", "%#v", "%+v", "%d", "%x"} {
		if rendered := fmt.Sprintf(format, token); strings.Contains(rendered, plaintext) {
			t.Errorf("Sprintf(%q, token) leaked it: %s", format, rendered)
		}
	}

	wrapper := struct {
		User  string
		Token Token
	}{User: "u1", Token: token}
	if rendered := fmt.Sprintf("%+v", wrapper); strings.Contains(rendered, plaintext) {
		t.Errorf("a struct containing a Token leaked it: %s", rendered)
	}
}

func TestTokenRefusesToMarshal(t *testing.T) {
	token, _, _ := NewToken()
	if encoded, err := json.Marshal(token); err == nil {
		t.Errorf("marshalling a Token succeeded: %s", encoded)
	}
}

// The control: the redaction tests would pass against a Token that was always
// empty.
func TestRevealReturnsTheToken(t *testing.T) {
	token, hash, _ := NewToken()
	if token.Reveal() == "" {
		t.Fatal("Reveal returned empty; the redaction tests prove nothing")
	}
	if HashToken(token.Reveal()) != hash {
		t.Fatal("the revealed token does not hash to the stored hash")
	}
}

// A malformed cookie must not become a database query: it is free to send and
// costs us a round trip.
func TestValidToken(t *testing.T) {
	good, _, _ := NewToken()

	cases := []struct {
		name  string
		value string
		want  bool
	}{
		{"a real token", good.Reveal(), true},
		{"empty", "", false},
		{"too short", good.Reveal()[:10], false},
		{"too long", good.Reveal() + "a", false},
		{"right length, wrong alphabet", strings.Repeat("!", tokenLength), false},
		{"base64 padding", strings.Repeat("A", tokenLength-1) + "=", false},
		{"standard base64 characters", strings.Repeat("A", tokenLength-1) + "/", false},
		{"a uuid", "0e3b0c44-2fc1-4f0e-9c1a-2b6e5f4d3a21", false},
		{"sql fragment", strings.Repeat("'", tokenLength), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidToken(tc.value); got != tc.want {
				t.Errorf("ValidToken(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// --- expiry --------------------------------------------------------------------

func TestLiveAppliesBothBounds(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	const idle = time.Hour

	session := func(created, lastSeen, expires time.Duration) Session {
		return Session{
			CreatedAt:  now.Add(created),
			LastSeenAt: now.Add(lastSeen),
			ExpiresAt:  now.Add(expires),
		}
	}

	cases := []struct {
		name string
		s    Session
		want bool
	}{
		{"fresh", session(-time.Minute, -time.Minute, 11*time.Hour), true},
		{"absolute passed", session(-13*time.Hour, -time.Minute, -time.Second), false},
		{"absolute exactly now", session(-12*time.Hour, -time.Minute, 0), false},
		{"one second before absolute", session(-12*time.Hour, -time.Minute, time.Second), true},

		{"idle passed", session(-2*time.Hour, -(idle + time.Second), time.Hour), false},
		{"idle exactly at the bound", session(-2*time.Hour, -idle, time.Hour), true},
		{"one second past idle", session(-2*time.Hour, -(idle + time.Second), time.Hour), false},

		{"both passed", session(-13*time.Hour, -13*time.Hour, -time.Hour), false},

		// Clock skew must never produce a negative age that wraps into expiry.
		{"last seen in the future", session(-time.Hour, time.Minute, time.Hour), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s.Live(idle, now); got != tc.want {
				t.Errorf("Live() = %v, want %v", got, tc.want)
			}
		})
	}
}

// A zero idle timeout means no idle bound, not "expires immediately" — the
// same reasoning as P1-02's max_age_days.
func TestZeroIdleTimeoutMeansNoIdleBound(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	s := Session{LastSeenAt: now.Add(-30 * 24 * time.Hour), ExpiresAt: now.Add(time.Hour)}

	if s.ExpiredIdle(0, now) {
		t.Error("a zero idle timeout expired a session; it must mean no idle bound")
	}
	if !s.Live(0, now) {
		t.Error("a session inside its absolute lifetime was not live with no idle bound")
	}
}

// --- policy ----------------------------------------------------------------------

func TestPolicySanitize(t *testing.T) {
	cases := []struct {
		name            string
		in              Policy
		want            Policy
		wantAdjustments int
	}{
		{
			name: "the default is untouched",
			in:   DefaultPolicy,
			want: DefaultPolicy,
		},
		{
			// The case that matters: an administrator configuring the control
			// away.
			name:            "below the floor is clamped",
			in:              Policy{AbsoluteLifetime: time.Second, IdleTimeout: time.Minute},
			want:            Policy{AbsoluteLifetime: MinAbsoluteLifetime, IdleTimeout: time.Minute},
			wantAdjustments: 1,
		},
		{
			name:            "above the ceiling is clamped",
			in:              Policy{AbsoluteLifetime: 365 * 24 * time.Hour, IdleTimeout: time.Hour},
			want:            Policy{AbsoluteLifetime: MaxAbsoluteLifetime, IdleTimeout: time.Hour},
			wantAdjustments: 1,
		},
		{
			// An idle timeout longer than the absolute lifetime can never
			// fire, so it is a setting that appears to do something and does
			// not.
			name:            "idle longer than absolute is capped to it",
			in:              Policy{AbsoluteLifetime: time.Hour, IdleTimeout: 24 * time.Hour},
			want:            Policy{AbsoluteLifetime: time.Hour, IdleTimeout: time.Hour},
			wantAdjustments: 1,
		},
		{
			name:            "a tiny idle timeout is floored",
			in:              Policy{AbsoluteLifetime: time.Hour, IdleTimeout: time.Second},
			want:            Policy{AbsoluteLifetime: time.Hour, IdleTimeout: MinIdleTimeout},
			wantAdjustments: 1,
		},
		{
			name: "zero idle is left alone",
			in:   Policy{AbsoluteLifetime: time.Hour, IdleTimeout: 0},
			want: Policy{AbsoluteLifetime: time.Hour, IdleTimeout: 0},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, adjustments := tc.in.Sanitize()
			if got != tc.want {
				t.Errorf("Sanitize() = %+v, want %+v", got, tc.want)
			}
			if len(adjustments) != tc.wantAdjustments {
				t.Errorf("got %d adjustments, want %d: %+v",
					len(adjustments), tc.wantAdjustments, adjustments)
			}
		})
	}
}

// A correction that is not reported is indistinguishable from a policy that
// was never applied.
func TestSanitizeReportsWhatItChanged(t *testing.T) {
	_, adjustments := Policy{AbsoluteLifetime: time.Second}.Sanitize()

	if len(adjustments) == 0 {
		t.Fatal("clamping the lifetime reported no adjustment")
	}
	a := adjustments[0]
	if a.Field == "" || a.Configured == "" || a.Applied == "" || a.Reason == "" {
		t.Errorf("an adjustment is missing detail an operator needs: %+v", a)
	}
}

func TestPolicyFromHours(t *testing.T) {
	// P0-07's column default.
	if got := PolicyFromHours(12); got.AbsoluteLifetime != 12*time.Hour {
		t.Errorf("PolicyFromHours(12) = %v, want 12h", got.AbsoluteLifetime)
	}
	// Absent or nonsensical falls back rather than producing a zero-length
	// session.
	for _, hours := range []int{0, -1} {
		if got := PolicyFromHours(hours); got != DefaultPolicy {
			t.Errorf("PolicyFromHours(%d) = %+v, want the default", hours, got)
		}
	}
}

// --- the cookie --------------------------------------------------------------------

// P1-11 DoD item 2: the attributes are asserted on the Set-Cookie header
// itself, because that string is the contract with the browser — a struct
// assertion would pass against a field the encoder ignores.
func TestCookieAttributes(t *testing.T) {
	token, _, _ := NewToken()

	rec := httptest.NewRecorder()
	http.SetCookie(rec, Cookie(token))
	header := rec.Header().Get("Set-Cookie")

	for _, want := range []string{
		CookieName + "=" + token.Reveal(),
		"Path=/",
		"HttpOnly",
		"Secure",
		"SameSite=Lax",
	} {
		if !strings.Contains(header, want) {
			t.Errorf("Set-Cookie is missing %q: %s", want, header)
		}
	}

	// No Domain: the __Host- prefix requires its absence, and a browser
	// silently refuses to store the cookie if it appears.
	if strings.Contains(header, "Domain=") {
		t.Errorf("Set-Cookie carries a Domain, which voids the __Host- prefix: %s", header)
	}

	// No Max-Age or Expires: a persistent cookie would outlive the session it
	// names, leaving a browser holding a credential the server has forgotten.
	if strings.Contains(header, "Max-Age") || strings.Contains(header, "Expires") {
		t.Errorf("Set-Cookie is persistent; it should be a browser-session cookie: %s", header)
	}

	if !strings.HasPrefix(CookieName, "__Host-") {
		t.Error("the cookie name lost its __Host- prefix; the browser stops enforcing " +
			"Secure, Path=/ and the absence of Domain")
	}
}

// Clearing must match the setting cookie in every attribute, or the browser
// treats it as a different cookie and keeps the original — a logout that
// appears to work and does not.
func TestClearCookieMatchesTheSetCookie(t *testing.T) {
	token, _, _ := NewToken()

	set := httptest.NewRecorder()
	http.SetCookie(set, Cookie(token))

	clear := httptest.NewRecorder()
	http.SetCookie(clear, ClearCookie())
	cleared := clear.Header().Get("Set-Cookie")

	for _, want := range []string{CookieName + "=", "Path=/", "HttpOnly", "Secure", "SameSite=Lax"} {
		if !strings.Contains(cleared, want) {
			t.Errorf("the clearing cookie is missing %q: %s", want, cleared)
		}
	}
	if !strings.Contains(cleared, "Max-Age=0") {
		t.Errorf("the clearing cookie does not expire: %s", cleared)
	}
	if strings.Contains(cleared, token.Reveal()) {
		t.Errorf("the clearing cookie still carries the token: %s", cleared)
	}
}

func TestFromRequest(t *testing.T) {
	good, _, _ := NewToken()

	cases := []struct {
		name   string
		cookie *http.Cookie
		wantOK bool
	}{
		{"a real token", &http.Cookie{Name: CookieName, Value: good.Reveal()}, true},
		{"no cookie", nil, false},
		{"malformed value", &http.Cookie{Name: CookieName, Value: "not-a-token"}, false},
		{"empty value", &http.Cookie{Name: CookieName, Value: ""}, false},
		{"a different cookie name", &http.Cookie{Name: "session", Value: good.Reveal()}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)
			if tc.cookie != nil {
				r.AddCookie(tc.cookie)
			}

			got, ok := FromRequest(r)
			if ok != tc.wantOK {
				t.Fatalf("FromRequest ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && got != good.Reveal() {
				t.Errorf("FromRequest returned %q, want the cookie value", got)
			}
		})
	}
}

func TestTokenZeroValue(t *testing.T) {
	var zero Token
	if !zero.IsZero() {
		t.Error("the zero Token does not report itself as zero")
	}
	token, _, _ := NewToken()
	if token.IsZero() {
		t.Error("a real token reports itself as zero")
	}

	// String and GoString are reachable directly, not only through fmt.
	if zero.String() != "[REDACTED]" {
		t.Errorf("String() = %q", zero.String())
	}
	if !strings.Contains(zero.GoString(), "REDACTED") {
		t.Errorf("GoString() = %q", zero.GoString())
	}
}

// --- P2-10: an organization's own lifetime ----------------------------------

// WithLifetimeHours replaces the absolute lifetime and KEEPS the idle timeout.
//
// The distinction is the whole design: how long a session may live is something
// an organization configures (`session_lifetime_hours` in its settings), and how
// this service treats inactivity is not — there is no field for it, and
// inventing one here would be a policy nobody asked for and nobody can see.
func TestWithLifetimeHoursReplacesOnlyTheAbsoluteBound(t *testing.T) {
	base := Policy{AbsoluteLifetime: 12 * time.Hour, IdleTimeout: 30 * time.Minute}

	got := base.WithLifetimeHours(48)

	if got.AbsoluteLifetime != 48*time.Hour {
		t.Errorf("absolute lifetime is %s, want 48h", got.AbsoluteLifetime)
	}
	if got.IdleTimeout != base.IdleTimeout {
		t.Errorf("the idle timeout changed to %s; it is not an organization's to configure", got.IdleTimeout)
	}
}

// Out of range is clamped, not honoured and not refused.
//
// Refusing would mean one bad settings value stops every login in the
// organization — a typo in a form becoming an outage. Honouring it would mean a
// session outliving any policy anybody intended.
func TestWithLifetimeHoursClampsRatherThanRefusing(t *testing.T) {
	base := Policy{AbsoluteLifetime: 12 * time.Hour, IdleTimeout: 30 * time.Minute}

	if got := base.WithLifetimeHours(100000); got.AbsoluteLifetime > MaxAbsoluteLifetime {
		t.Errorf("100000 hours produced %s, past the ceiling of %s", got.AbsoluteLifetime, MaxAbsoluteLifetime)
	}
	if got := base.WithLifetimeHours(0); got.AbsoluteLifetime != base.AbsoluteLifetime {
		t.Errorf("zero hours changed the lifetime to %s; absent configuration must leave it alone", got.AbsoluteLifetime)
	}
	if got := base.WithLifetimeHours(-5); got.AbsoluteLifetime != base.AbsoluteLifetime {
		t.Errorf("a negative lifetime changed it to %s", got.AbsoluteLifetime)
	}
}

// The hour bounds `internal/authn` duplicates must match the durations here.
//
// They are duplicated rather than imported because importing `internal/session`
// from `internal/authn` would be a cycle — sessions already depend on the
// password policy. This is what keeps the two honest.
func TestTheHourBoundsMatchTheDurationBounds(t *testing.T) {
	if MinAbsoluteLifetime > time.Hour {
		t.Errorf("authn.MinSessionLifetimeHours is 1, but the floor here is %s — a 1-hour setting would be clamped upward",
			MinAbsoluteLifetime)
	}
	if MaxAbsoluteLifetime != 720*time.Hour {
		t.Errorf("the ceiling here is %s; authn.MaxSessionLifetimeHours says 720", MaxAbsoluteLifetime)
	}
}
