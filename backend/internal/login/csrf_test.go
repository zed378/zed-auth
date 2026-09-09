package login

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func withCSRF(r *http.Request, value string) *http.Request {
	r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: value})
	return r
}

func TestCSRFTokensAreUnguessable(t *testing.T) {
	seen := map[string]bool{}
	for range 64 {
		token, err := newCSRFToken()
		if err != nil {
			t.Fatalf("newCSRFToken: %v", err)
		}
		if !validCSRFToken(token) {
			t.Fatalf("a freshly minted token failed its own shape check: %q", token)
		}
		if seen[token] {
			t.Fatal("two tokens collided; the random source is broken")
		}
		seen[token] = true
	}
}

// The four cases the DoD asks for, in one place.
func TestCSRFComparison(t *testing.T) {
	good, err := newCSRFToken()
	if err != nil {
		t.Fatalf("newCSRFToken: %v", err)
	}
	other, _ := newCSRFToken()

	cases := map[string]struct {
		cookie    string
		submitted string
		want      bool
	}{
		"matching":         {cookie: good, submitted: good, want: true},
		"no cookie":        {cookie: "", submitted: good, want: false},
		"no field":         {cookie: good, submitted: "", want: false},
		"different token":  {cookie: good, submitted: other, want: false},
		"truncated field":  {cookie: good, submitted: good[:len(good)-1], want: false},
		"malformed cookie": {cookie: "not-a-token", submitted: good, want: false},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, Path, nil)
			if c.cookie != "" {
				withCSRF(r, c.cookie)
			}
			if got := checkCSRF(r, c.submitted); got != c.want {
				t.Errorf("checkCSRF = %v, want %v", got, c.want)
			}
		})
	}
}

// The __Host- prefix and SameSite are the two things that make double-submit
// hold at all, so they are asserted rather than assumed. Without the prefix, an
// attacker controlling any subdomain could set the cookie and then know its
// value, which is the standard break of this pattern.
func TestCSRFCookieAttributes(t *testing.T) {
	cookie := csrfCookie("token")

	if !strings.HasPrefix(cookie.Name, "__Host-") {
		t.Errorf("the CSRF cookie is named %q; without the __Host- prefix a "+
			"subdomain can set it and double-submit gives nothing", cookie.Name)
	}
	// The prefix is only honoured when all three hold. A browser silently
	// ignores a __Host- cookie that breaks any of them, which would leave the
	// page with no CSRF protection and no error anywhere.
	if !cookie.Secure {
		t.Error("Secure is not set, which a __Host- cookie requires")
	}
	if cookie.Path != "/" {
		t.Errorf("Path = %q, and a __Host- cookie requires /", cookie.Path)
	}
	if cookie.Domain != "" {
		t.Errorf("Domain = %q, and a __Host- cookie must not have one", cookie.Domain)
	}
	if !cookie.HttpOnly {
		t.Error("HttpOnly is not set; there is no script that needs to read this")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v; Lax is required — Strict would drop the cookie "+
			"on the cross-site redirect that renders the form, and None would "+
			"send it on a cross-site POST", cookie.SameSite)
	}
}

// Secure has no way to be false. P1-11 removed exactly such a parameter from
// the session cookie: a control whose only failure mode is somebody passing
// false is better expressed as one with no way to say false.
func TestCSRFCookieHasNoInsecureMode(t *testing.T) {
	for range 8 {
		if !csrfCookie("token").Secure {
			t.Fatal("csrfCookie produced a cookie without Secure")
		}
	}
}
