package token

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/oauth/client"
)

func oauthError(t *testing.T, err error) Error {
	t.Helper()
	var e Error
	if !errors.As(err, &e) {
		t.Fatalf("error is not an oauth Error: %v", err)
	}
	return e
}

// --- grants -------------------------------------------------------------------

// P1-07 DoD item 2. Refused by name, not by falling through to "unknown": a
// client built against the password grant should be told the service will
// never support it, not left wondering whether it typed the name wrong.
func TestRefusedGrantsAreNamed(t *testing.T) {
	for _, grant := range []string{"password", "implicit"} {
		t.Run(grant, func(t *testing.T) {
			e := oauthError(t, ValidateGrant(grant))

			if e.Code != ErrUnsupportedGrantType {
				t.Errorf("error = %q, want %q", e.Code, ErrUnsupportedGrantType)
			}
			if !strings.Contains(e.Description, "not supported") {
				t.Errorf("the description does not say it is unsupported: %q", e.Description)
			}
			// It should say what to use instead — an integrator hitting this
			// has a real problem and the fix is one sentence away.
			if !strings.Contains(e.Description, "authorization code") {
				t.Errorf("the description does not point at the supported flow: %q", e.Description)
			}
		})
	}
}

func TestSupportedGrants(t *testing.T) {
	for _, grant := range []string{GrantAuthorizationCode, GrantRefreshToken, GrantClientCredentials} {
		if err := ValidateGrant(grant); err != nil {
			t.Errorf("ValidateGrant(%q) = %v, want nil", grant, err)
		}
	}
	for _, grant := range []string{"", "banana", "urn:ietf:params:oauth:grant-type:jwt-bearer"} {
		if err := ValidateGrant(grant); err == nil {
			t.Errorf("ValidateGrant(%q) accepted it", grant)
		}
	}
}

// --- client authentication ---------------------------------------------------------

func request(t *testing.T, basicID, basicSecret string, form url.Values) (*http.Request, url.Values) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if basicID != "" || basicSecret != "" {
		r.SetBasicAuth(basicID, basicSecret)
	}
	return r, form
}

func TestParseCredentials(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		r, form := request(t, "client-1", "s3cret", url.Values{})
		creds, err := ParseCredentials(r, form)
		if err != nil {
			t.Fatalf("ParseCredentials: %v", err)
		}
		if creds.ClientID != "client-1" || creds.Secret != "s3cret" {
			t.Errorf("got %+v", creds)
		}
		if !creds.UsedBasic || !creds.Presented {
			t.Errorf("basic auth not recorded: %+v", creds)
		}
	})

	t.Run("post", func(t *testing.T) {
		form := url.Values{"client_id": {"client-1"}, "client_secret": {"s3cret"}}
		r, form := request(t, "", "", form)
		creds, err := ParseCredentials(r, form)
		if err != nil {
			t.Fatalf("ParseCredentials: %v", err)
		}
		if creds.ClientID != "client-1" || creds.Secret != "s3cret" || creds.UsedBasic {
			t.Errorf("got %+v", creds)
		}
	})

	t.Run("public client, no secret", func(t *testing.T) {
		form := url.Values{"client_id": {"spa-1"}}
		r, form := request(t, "", "", form)
		creds, err := ParseCredentials(r, form)
		if err != nil {
			t.Fatalf("ParseCredentials: %v", err)
		}
		if creds.Presented {
			t.Error("a request with no secret reported one as presented")
		}
	})

	// RFC 6749 forbids it, and the practical reason is better: a request that
	// authenticates two ways is one where something is confused about which
	// credential it holds, and picking a winner would hide that.
	t.Run("both basic and post is refused", func(t *testing.T) {
		form := url.Values{"client_secret": {"other"}}
		r, form := request(t, "client-1", "s3cret", form)
		if _, err := ParseCredentials(r, form); err == nil {
			t.Error("presenting credentials twice was accepted")
		}
	})

	t.Run("mismatched client_id between header and body", func(t *testing.T) {
		form := url.Values{"client_id": {"someone-else"}}
		r, form := request(t, "client-1", "s3cret", form)
		if _, err := ParseCredentials(r, form); err == nil {
			t.Error("a client_id disagreement was accepted")
		}
	})

	t.Run("no client_id at all", func(t *testing.T) {
		r, form := request(t, "", "", url.Values{})
		_, err := ParseCredentials(r, form)
		e := oauthError(t, err)
		if e.Code != ErrInvalidClient || e.Status != http.StatusUnauthorized {
			t.Errorf("got %q/%d, want invalid_client/401", e.Code, e.Status)
		}
	})

	// RFC 6749 § 2.3.1 form-urlencodes both parts inside Basic. Skipping the
	// decode is a real interoperability bug for any secret with a special
	// character in it.
	t.Run("basic values are form-decoded", func(t *testing.T) {
		r, form := request(t, url.QueryEscape("client 1"), url.QueryEscape("s3c ret+/="), url.Values{})
		creds, err := ParseCredentials(r, form)
		if err != nil {
			t.Fatalf("ParseCredentials: %v", err)
		}
		if creds.ClientID != "client 1" || creds.Secret != "s3c ret+/=" {
			t.Errorf("credentials were not form-decoded: %+v", creds)
		}
	})
}

// Abuse case A-4: the client-authentication bypass. A confidential client
// presenting no secret must never be quietly treated as public — that is the
// kind of leniency that looks harmless until somebody notices a web client's
// code can be redeemed by anyone who saw it.
func TestConfidentialClientIsNeverDowngradedToPublic(t *testing.T) {
	app := client.Application{ID: "c1", Type: client.TypeWeb}
	_, hash, _ := client.Generate()
	stored := client.Credentials{Hash: hash}

	err := AuthenticateClient(app, Credentials{ClientID: "c1"}, stored, time.Now())

	e := oauthError(t, err)
	if e.Code != ErrInvalidClient {
		t.Errorf("error = %q, want %q", e.Code, ErrInvalidClient)
	}
	if e.Status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", e.Status)
	}
}

func TestAuthenticateClient(t *testing.T) {
	secret, hash, _ := client.Generate()
	stored := client.Credentials{Hash: hash}
	now := time.Now()

	web := client.Application{ID: "c1", Type: client.TypeWeb}
	spa := client.Application{ID: "c2", Type: client.TypeSPA}
	saml := client.Application{ID: "c3", Type: client.TypeSAML}

	cases := []struct {
		name  string
		app   client.Application
		creds Credentials
		ok    bool
	}{
		{"confidential, correct secret", web,
			Credentials{ClientID: "c1", Secret: secret.Reveal(), Presented: true}, true},
		{"confidential, wrong secret", web,
			Credentials{ClientID: "c1", Secret: "nope", Presented: true}, false},
		{"confidential, empty secret presented", web,
			Credentials{ClientID: "c1", Secret: "", Presented: true}, false},
		{"public, no secret", spa, Credentials{ClientID: "c2"}, true},
		// A public client holding a secret has a credential it should not
		// have — a copied configuration, or the wrong client_id — and
		// accepting it would confirm the mistake works.
		{"public, secret presented", spa,
			Credentials{ClientID: "c2", Secret: "anything", Presented: true}, false},
		{"saml is neither", saml, Credentials{ClientID: "c3"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := AuthenticateClient(tc.app, tc.creds, stored, now)
			if tc.ok && err != nil {
				t.Errorf("AuthenticateClient = %v, want nil", err)
			}
			if !tc.ok && err == nil {
				t.Error("AuthenticateClient accepted it")
			}
		})
	}
}

// The rotation overlap from P1-05 has to work here, or rotating a secret
// breaks every client until it redeploys.
func TestThePreviousSecretAuthenticatesDuringItsOverlap(t *testing.T) {
	now := time.Now()

	old, hash, _ := client.Generate()
	fresh, rotated, err := client.Credentials{Hash: hash}.Rotate(client.DefaultRotationOverlap, now)
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	app := client.Application{ID: "c1", Type: client.TypeWeb}

	during := now.Add(time.Hour)
	for name, secret := range map[string]string{"new": fresh.Reveal(), "previous": old.Reveal()} {
		if err := AuthenticateClient(app,
			Credentials{ClientID: "c1", Secret: secret, Presented: true}, rotated, during); err != nil {
			t.Errorf("the %s secret does not authenticate during the overlap: %v", name, err)
		}
	}

	after := now.Add(client.DefaultRotationOverlap + time.Minute)
	if err := AuthenticateClient(app,
		Credentials{ClientID: "c1", Secret: old.Reveal(), Presented: true}, rotated, after); err == nil {
		t.Error("the previous secret still authenticates after the overlap expired")
	}
}

// --- PKCE -----------------------------------------------------------------------------

// The challenge is computed here independently rather than by the function
// under test, so a bug in the derivation cannot make the test agree with
// itself.
func challengeFor(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestVerifyPKCE(t *testing.T) {
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	challenge := challengeFor(verifier)

	if err := VerifyPKCE(challenge, verifier); err != nil {
		t.Fatalf("the correct verifier was rejected: %v", err)
	}

	cases := []struct {
		name      string
		challenge string
		verifier  string
	}{
		{"wrong verifier", challenge, "M25iVXpKU3puUjFaYWg3T1NDTDQtcW1ROUY5YXlwalNoc0hhakxifmZHag"},
		{"empty verifier", challenge, ""},
		{"too short", challenge, "short"},
		{"too long", challenge, strings.Repeat("a", 129)},
		{"illegal characters", challenge, strings.Repeat("!", 43)},
		{"no challenge stored", "", verifier},
		{"the challenge presented as the verifier", challenge, challenge},
		{"verifier with one character changed", challenge, "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXj"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := VerifyPKCE(tc.challenge, tc.verifier); err == nil {
				t.Error("VerifyPKCE accepted it")
			}
		})
	}
}

// RFC 7636's unreserved set is wider than base64url — it includes '.' and '~'.
// Narrowing it would reject conforming clients for no gain.
func TestVerifierAlphabetFollowsRFC7636(t *testing.T) {
	verifier := "abcABC123-._~" + strings.Repeat("a", 30)
	if err := VerifyPKCE(challengeFor(verifier), verifier); err != nil {
		t.Errorf("a conforming verifier using the full unreserved set was rejected: %v", err)
	}
}

// --- scope ----------------------------------------------------------------------------

// Widening would make the refresh token more powerful than the consent that
// created it, which is the one thing a refresh token must not be.
func TestRefreshCannotWidenScope(t *testing.T) {
	original := []string{"openid", "profile"}

	if _, err := NarrowScope(original, []string{"openid", "email"}); err == nil {
		t.Error("a refresh widened its scope")
	}

	narrowed, err := NarrowScope(original, []string{"openid"})
	if err != nil {
		t.Fatalf("narrowing was refused: %v", err)
	}
	if len(narrowed) != 1 || narrowed[0] != "openid" {
		t.Errorf("narrowed = %v, want [openid]", narrowed)
	}

	// No scope requested keeps the original.
	same, err := NarrowScope(original, nil)
	if err != nil {
		t.Fatalf("NarrowScope: %v", err)
	}
	if len(same) != len(original) {
		t.Errorf("an unspecified scope changed it: %v", same)
	}
}

func TestParseScope(t *testing.T) {
	got := ParseScope("openid  profile openid email")
	want := []string{"openid", "profile", "email"}

	if len(got) != len(want) {
		t.Fatalf("ParseScope = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ParseScope[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if ParseScope("   ") != nil {
		t.Error("an empty scope did not parse to nil")
	}
}

// The Error type carries the OAuth code and the status it travels on.
func TestErrorCarriesItsCodeAndStatus(t *testing.T) {
	e := Error{Code: ErrInvalidGrant, Status: http.StatusBadRequest, Description: "no"}

	if !strings.Contains(e.Error(), ErrInvalidGrant) {
		t.Errorf("Error() = %q, want it to name the code", e.Error())
	}
	if !strings.Contains(e.Error(), "no") {
		t.Errorf("Error() = %q, want it to carry the description", e.Error())
	}
}

// Redaction on the refresh token, the third type in the project to need it —
// and the third to be checked across every verb, because P1-05 found that
// fmt.Stringer alone leaks under %d.
func TestRefreshTokenDoesNotPrintItself(t *testing.T) {
	tok := RefreshToken{plaintext: "s3cret-refresh-value"}

	for _, format := range []string{"%s", "%v", "%q", "%#v", "%+v", "%d", "%x"} {
		if rendered := fmt.Sprintf(format, tok); strings.Contains(rendered, tok.Reveal()) {
			t.Errorf("Sprintf(%q, token) leaked it: %s", format, rendered)
		}
	}

	if _, err := json.Marshal(tok); err == nil {
		t.Error("a RefreshToken marshalled to JSON")
	}
	if tok.String() != "[REDACTED]" || !strings.Contains(tok.GoString(), "REDACTED") {
		t.Error("a redaction method returned something else")
	}
	if tok.IsZero() {
		t.Error("a real token reports itself as zero")
	}
	if !(RefreshToken{}).IsZero() {
		t.Error("the zero token does not report itself as zero")
	}
	// The control: the assertions above would pass against a token that was
	// always empty.
	if tok.Reveal() != "s3cret-refresh-value" {
		t.Error("Reveal does not return the value")
	}
}

func TestHashRefreshIsNotTheToken(t *testing.T) {
	const plaintext = "a-refresh-token"
	hash := HashRefresh(plaintext)

	if hash == plaintext || strings.Contains(hash, plaintext) {
		t.Error("the hash contains the token")
	}
	if HashRefresh(plaintext) != hash {
		t.Error("HashRefresh is not deterministic")
	}
	if HashRefresh("other") == hash {
		t.Error("two different tokens hash the same")
	}
}
