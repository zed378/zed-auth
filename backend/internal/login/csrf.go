package login

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"time"
)

// CSRF protection for a form with no JavaScript.
//
// Double-submit: the token is set in a cookie and rendered into a hidden
// field, and the POST is refused unless the two match. It needs no server-side
// state, which matters here — the login page is reached by people who have no
// session yet, so there is nowhere to keep a per-user token.
//
// Two properties make it hold, and both are structural rather than
// conventional:
//
//   - The __Host- prefix. A browser refuses to store such a cookie unless it
//     is Secure, has no Domain, and has Path=/. The Domain restriction is the
//     one that matters: without it, an attacker who controls any subdomain of
//     the auth service could set the cookie and then know its value, which
//     defeats double-submit entirely. This is the standard break of the naive
//     pattern and the prefix is the standard answer.
//   - SameSite=Lax. A cross-site POST does not carry the cookie at all, so
//     the comparison has nothing to compare and fails before the token's
//     secrecy is even relied on.
//
// Belt and braces, deliberately: either one alone would do, and neither alone
// is worth trusting on the page where a compromise hands over an account.

// CSRFCookieName is the cookie carrying the token.
//
// A different cookie from the session — this one exists before anybody is
// authenticated, and conflating them would mean issuing a session cookie to
// every visitor who merely loaded a page.
const CSRFCookieName = "__Host-zedauth_csrf"

// csrfField is the hidden input's name.
const csrfField = "csrf_token"

// csrfBytes is the entropy in a token. 256 bits, matching every other
// unguessable value in this service.
const csrfBytes = 32

// csrfLifetime bounds how long a rendered form stays submittable.
//
// One hour: comfortably longer than PendingTTL, so the pending request always
// expires first and the CSRF token is never the reason a login fails. Making
// it shorter would produce a second, differently-worded timeout for the same
// situation.
const csrfLifetime = time.Hour

var csrfLength = base64.RawURLEncoding.EncodedLen(csrfBytes)

// newCSRFToken returns a fresh token.
func newCSRFToken() (string, error) {
	buf := make([]byte, csrfBytes)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", fmt.Errorf("login: generating CSRF token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// validCSRFToken reports whether a value is shaped like one of ours.
//
// Checked before comparison so a malformed cookie is rejected without the
// comparison having to be safe against odd lengths.
func validCSRFToken(value string) bool {
	if len(value) != csrfLength {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil
}

// csrfCookie builds the cookie carrying a token.
//
// Secure is unconditional, and there is no parameter that could turn it off.
// P1-11 removed exactly such a parameter from the session cookie: a control
// whose only failure mode is somebody passing false is better expressed as a
// control with no way to say false. Local development reaches this over
// https://localhost, which browsers treat as a secure context.
func csrfCookie(token string) *http.Cookie {
	return &http.Cookie{
		Name:  CSRFCookieName,
		Value: token,
		Path:  "/",
		// Required by the __Host- prefix, and independently correct: no
		// script needs to read this, because there is no script.
		HttpOnly: true,
		Secure:   true,
		// Lax, not Strict. The login page is arrived at by a cross-site
		// redirect from an application, so Strict would drop the cookie on the
		// navigation that renders the form — and the form would then never be
		// submittable. Lax sends it on a top-level navigation and withholds it
		// on a cross-site POST, which is exactly the shape of this flow.
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(csrfLifetime.Seconds()),
	}
}

// csrfFromRequest reads a token from the cookie.
func csrfFromRequest(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(CSRFCookieName)
	if err != nil || !validCSRFToken(cookie.Value) {
		return "", false
	}
	return cookie.Value, true
}

// checkCSRF compares the submitted token against the cookie.
//
// Constant time. The comparison is not obviously timing-sensitive — an
// attacker who could measure it would still have to forge a value they cannot
// read — but the cost of doing it properly is nothing and the argument for
// doing it the other way is only that it is shorter.
func checkCSRF(r *http.Request, submitted string) bool {
	cookie, ok := csrfFromRequest(r)
	if !ok || !validCSRFToken(submitted) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookie), []byte(submitted)) == 1
}
