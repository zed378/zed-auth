package userinfo

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Reading and checking the bearer access token.
//
// The order of the checks is the security property, the same way it is at
// /oauth/authorize: nothing here touches the database until the token has been
// proven to be one this service signed, of the right kind, for this audience,
// and still valid. An unverified token must not be able to make this service
// do work on its behalf.

// ErrInvalidToken is every reason a token cannot be used.
//
// One error for expired, forged, wrong-audience, wrong-issuer, wrong-type and
// revoked. Collapsing them is the point: a helpful error_description per case
// turns this endpoint into a probe, and the most useful thing it would tell an
// attacker holding a captured token is whether the user has logged out since.
// The specific reason goes to the log, which is why `reason` exists.
var ErrInvalidToken = errors.New("userinfo: the access token is not usable")

// ErrInsufficientScope means the token is fine and does not cover this.
//
// Separate from ErrInvalidToken deliberately, and it is not a disclosure: the
// caller already knows what scopes it asked for. RFC 6750 defines the code
// precisely so a client can tell "your token is broken" from "your token is
// fine but does not cover this", and conflating them sends a working client
// into a pointless re-authentication loop.
var ErrInsufficientScope = errors.New("userinfo: the access token lacks the openid scope")

// clockSkew is how far ahead of us another clock may be.
//
// Applies to `iat` only. `exp` gets no leeway in the permissive direction:
// this service issued the token and stamped both timestamps with its own
// clock, so an expiry in the past is expired. Skew tolerance on `exp` would be
// extending the life of every access token by the tolerance.
const clockSkew = 30 * time.Second

// AccessToken is the subset of P1-07's access token claims this endpoint reads.
type AccessToken struct {
	Issuer    string `json:"iss"`
	Subject   string `json:"sub"`
	Audience  string `json:"aud"`
	ExpiresAt int64  `json:"exp"`
	IssuedAt  int64  `json:"iat"`
	ClientID  string `json:"client_id"`
	OrgID     string `json:"org_id"`
	SessionID string `json:"sid"`
	Scope     string `json:"scope"`
}

// Scopes splits the space-delimited scope claim.
func (t AccessToken) Scopes() []string { return strings.Fields(t.Scope) }

// BearerToken extracts the credential from an Authorization header.
//
// The header and nothing else. RFC 6750 §2.3 also defines an `access_token`
// URI query parameter and calls it NOT RECOMMENDED; it is not accepted here,
// because a query parameter is written to the access log of every proxy in the
// path, to the browser's history, and to the Referer of whatever the page
// loads next. P1-06 goes to some trouble to keep credentials out of URLs and
// accepting one here would undo that on the endpoint that returns an email
// address.
func BearerToken(header string) (string, bool) {
	// SplitN rather than Fields: a token containing whitespace is not a token,
	// and Fields would silently take the first piece of one.
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 {
		return "", false
	}

	// Case-insensitive, per RFC 7235 §2.1 — the scheme is a token, and clients
	// send "bearer" as well as "Bearer".
	if !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}

	credential := strings.TrimSpace(parts[1])
	if credential == "" {
		return "", false
	}
	return credential, true
}

// Validate checks a verified token's claims.
//
// Takes the payload AFTER signature and `typ` verification, because a claim
// read from an unverified token is a claim an attacker wrote. Returns a reason
// for the log alongside the error the caller answers with.
func Validate(payload []byte, issuer string, now time.Time) (AccessToken, string, error) {
	var token AccessToken
	if err := json.Unmarshal(payload, &token); err != nil {
		return AccessToken{}, "the payload is not JSON", ErrInvalidToken
	}

	// A token this service signed but for a different deployment. Checked even
	// though the signature already passed, because a key set can legitimately
	// be shared across issuers and the signature alone would not notice.
	if token.Issuer != issuer {
		return AccessToken{}, fmt.Sprintf("issuer is %q", token.Issuer), ErrInvalidToken
	}

	// The audience is this service. P1-07 sets an access token's `aud` to the
	// issuer and an ID token's to the client — so this check is the second
	// half of refusing an ID token here, after `typ`. Two independent reasons
	// to refuse the same thing, which is what makes A-5 hard to reintroduce.
	if token.Audience != issuer {
		return AccessToken{}, fmt.Sprintf("audience is %q", token.Audience), ErrInvalidToken
	}

	if token.ExpiresAt == 0 || now.Unix() >= token.ExpiresAt {
		return AccessToken{}, "expired", ErrInvalidToken
	}

	// A token issued in the future is a clock problem or a forgery attempt,
	// and either way its expiry cannot be trusted.
	if token.IssuedAt > now.Add(clockSkew).Unix() {
		return AccessToken{}, "issued in the future", ErrInvalidToken
	}

	if token.Subject == "" || token.OrgID == "" {
		return AccessToken{}, "no subject or organization", ErrInvalidToken
	}

	// No `sid` means no session behind it, which means a client_credentials
	// token — where `sub` is the CLIENT, not a person. Answering would mean
	// describing an application as if it were a user, and the `sub` in the
	// response would be a client id that a consumer stores as a person's
	// permanent identifier.
	if token.SessionID == "" {
		return AccessToken{}, "no session; this looks like a client_credentials token", ErrInvalidToken
	}

	// Scope last, so that an invalid token is never answered with the more
	// informative insufficient_scope.
	if !hasOpenID(token.Scopes()) {
		return token, "no openid scope", ErrInsufficientScope
	}

	return token, "", nil
}

func hasOpenID(scope []string) bool {
	for _, s := range scope {
		if s == ScopeOpenID {
			return true
		}
	}
	return false
}
