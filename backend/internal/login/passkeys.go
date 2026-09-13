package login

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/redis/go-redis/v9"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/mfa"
	"github.com/zed378/zed-auth/backend/internal/oauth/client"
	"github.com/zed378/zed-auth/backend/internal/session"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Registering a passkey (P3-10, PG-43).
//
// Specification: MEMORY/specs/P3-10-mfa-tab.md.
//
//	GET  /account/passkeys?client_id=…&return_to=…   begin the ceremony, render the page
//	POST /account/passkeys                           finish it
//
// **Why a hosted page and not the console.** WebAuthn binds a credential to the
// relying party, and P3-05 made the relying party this service's own origin —
// deliberately, because that binding is the phishing defence. The console is a
// separate deployment on a separate origin (docs/PLAN/06), and a ceremony run
// from its page is refused by the browser. So registration happens here, on the
// same origin as the sign-in step that will later ask for the passkey, and the
// console sends the user here and back.
//
// **Who may use it.** The SSO session cookie, and a RECENT one: the session must
// have authenticated within mfa.RecentAuthentication. A stolen session adding
// its own passkey is how a takeover becomes permanent, and a passkey is the
// strongest factor there is — it deserves at least the check an authenticator
// app gets.
//
// It carries the second script in the hosted flow, under the same rules as the
// first (PG-40): pinned by hash, no fetch, no connect-src, one browser API and
// one form submit.

// PasskeysPath is where the page is mounted.
const PasskeysPath = "/account/passkeys"

// passkeyCookieName carries the registration handle. `__Host-` for the reason
// the challenge cookie is.
const passkeyCookieName = "__Host-zedauth_passkey"

// passkeyRegistrationTTL bounds a ceremony from page to submit.
const passkeyRegistrationTTL = 5 * time.Minute

// maxCredentialBytes bounds the posted attestation, as maxAssertionBytes bounds
// the assertion.
const maxCredentialBytes = 64 << 10

// PasskeyCeremony is P3-05's registration half. Satisfied by mfa.WebAuthnVerifier.
type PasskeyCeremony interface {
	BeginRegistration(ctx context.Context, userID, orgID, label, displayName string) ([]byte, *webauthn.SessionData, error)
	FinishRegistration(ctx context.Context, userID, orgID, label, displayName string,
		session webauthn.SessionData, response []byte) (string, error)
}

// PasskeyRecovery issues recovery codes with a first factor. Satisfied by
// mfa.RecoveryStore.
type PasskeyRecovery interface {
	Remaining(ctx context.Context, tx *postgres.Tx, userID string) (int, error)
	Issue(ctx context.Context, tx *postgres.Tx, userID, orgID string, count int, now time.Time) ([]string, string, error)
}

// PasskeyRegistration is what the page needs.
type PasskeyRegistration struct {
	Ceremony PasskeyCeremony
	Recovery PasskeyRecovery
	States   *RedisPasskeyStates

	// Clients resolves the application a return_to must belong to.
	Clients func(ctx context.Context, clientID string) (client.Application, error)
}

// PasskeyState is the server-side half of one ceremony.
type PasskeyState struct {
	UserID    string `json:"user_id"`
	OrgID     string `json:"org_id"`
	SessionID string `json:"session_id"`

	// DisplayName is what the ceremony was begun with; finishing must present
	// the same user to the relying party library.
	DisplayName string               `json:"display_name"`
	Ceremony    webauthn.SessionData `json:"ceremony"`
	ReturnTo    string               `json:"return_to,omitempty"`
}

// RedisPasskeyStates stores ceremonies under a hashed handle.
type RedisPasskeyStates struct {
	Client redis.UniversalClient
}

func passkeyStateKey(handle string) string {
	sum := sha256.Sum256([]byte(handle))
	return "login:passkey:" + base64.RawURLEncoding.EncodeToString(sum[:])
}

// Put stores a ceremony and returns its handle.
func (r *RedisPasskeyStates) Put(ctx context.Context, state PasskeyState) (string, error) {
	handle, err := mfa.NewHandle()
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("login: encoding a passkey ceremony: %w", err)
	}
	ok, err := r.Client.SetNX(ctx, passkeyStateKey(handle), raw, passkeyRegistrationTTL).Result()
	if err != nil {
		return "", fmt.Errorf("login: storing a passkey ceremony: %w", err)
	}
	if !ok {
		return "", errors.New("login: a passkey ceremony handle collided")
	}
	return handle, nil
}

// Take reads and deletes a ceremony in one step, so it answers once.
func (r *RedisPasskeyStates) Take(ctx context.Context, handle string) (PasskeyState, error) {
	raw, err := r.Client.GetDel(ctx, passkeyStateKey(handle)).Bytes()
	if err != nil {
		return PasskeyState{}, err
	}
	var state PasskeyState
	if err := json.Unmarshal(raw, &state); err != nil {
		return PasskeyState{}, fmt.Errorf("login: decoding a passkey ceremony: %w", err)
	}
	return state, nil
}

// Passkeys serves both methods.
func (h *Handler) Passkeys(w http.ResponseWriter, r *http.Request) {
	if h.PasskeyRegistration == nil {
		h.notice(w, r, http.StatusNotFound, Notice{
			Title: "Not available",
			Body:  "This service is not configured to register passkeys.",
		})
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.beginPasskey(w, r)
	case http.MethodPost:
		h.finishPasskey(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		h.notice(w, r, http.StatusMethodNotAllowed, Notice{
			Title: "Method not allowed", Body: "This page accepts GET and POST.",
		})
	}
}

// signedInRecently resolves the session cookie and checks its age.
func (h *Handler) signedInRecently(r *http.Request, now time.Time) (session.Session, bool, bool) {
	current, ok := h.currentSession(r.Context(), r, now)
	if !ok {
		return session.Session{}, false, false
	}
	return current, true, now.Sub(current.CreatedAt) <= mfa.RecentAuthentication
}

func (h *Handler) beginPasskey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := h.now()

	returnTo, err := h.passkeyReturn(ctx, r.URL.Query().Get("client_id"), r.URL.Query().Get("return_to"))
	if err != nil {
		h.notice(w, r, http.StatusBadRequest, Notice{
			Title: "This link is not valid",
			Body:  "The page that sent you here is not one this service recognises.",
		})
		return
	}

	current, signedIn, recent := h.signedInRecently(r, now)
	switch {
	case !signedIn:
		h.notice(w, r, http.StatusUnauthorized, Notice{
			Title: "Sign in first",
			Body:  "Sign in, then add your passkey from your account settings.",
		})
		return
	case !recent:
		h.notice(w, r, http.StatusForbidden, Notice{
			Title: "Sign in again to add a passkey",
			Body: "For your security, adding a way to sign in needs a recent sign-in. " +
				"Go back, sign in again, and try once more.",
		})
		return
	}

	var displayName string
	if err := h.DB.WithTenant(ctx, current.OrgID, func(tx *postgres.Tx) error {
		account, err := h.Users.ByID(ctx, tx, current.UserID)
		if err != nil {
			return err
		}
		if !account.CanSignIn() {
			return errors.New("login: the account cannot sign in")
		}
		displayName = account.Email
		return nil
	}); err != nil {
		h.serverError(w, r, "loading the account for a passkey", err)
		return
	}

	options, ceremony, err := h.PasskeyRegistration.Ceremony.BeginRegistration(
		ctx, current.UserID, current.OrgID, "Passkey", displayName)
	if err != nil {
		h.serverError(w, r, "beginning a passkey registration", err)
		return
	}

	handle, err := h.PasskeyRegistration.States.Put(ctx, PasskeyState{
		UserID: current.UserID, OrgID: current.OrgID, SessionID: current.ID,
		DisplayName: displayName, Ceremony: *ceremony, ReturnTo: returnTo,
	})
	if err != nil {
		h.serverError(w, r, "storing a passkey ceremony", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: passkeyCookieName, Value: handle, Path: "/", HttpOnly: true, Secure: true,
		SameSite: http.SameSiteLaxMode, MaxAge: int(passkeyRegistrationTTL.Seconds()),
	})

	csrf, err := h.csrfFor(w, r)
	if err != nil {
		h.serverError(w, r, "issuing a CSRF token", err)
		return
	}

	page := PasskeyPage{CSRFToken: csrf, Options: template.JS(options), ReturnTo: returnTo}
	page.Style, page.StyleHash = renderStyle(DefaultAccent)
	body, err := render(passkeyTemplate, page)
	if err != nil {
		h.serverError(w, r, "rendering the passkey page", err)
		return
	}
	h.write(w, http.StatusOK, page.ContentSecurityPolicy(), body)
}

func (h *Handler) finishPasskey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := h.now()

	if err := r.ParseForm(); err != nil || !checkCSRF(r, r.PostFormValue("csrf_token")) {
		h.passkeyFailed(w, r, "", "That form expired. Please start again.")
		return
	}

	cookie, err := r.Cookie(passkeyCookieName)
	if err != nil || cookie.Value == "" {
		h.passkeyFailed(w, r, "", "That registration expired. Please start again.")
		return
	}
	state, err := h.PasskeyRegistration.States.Take(ctx, cookie.Value)
	http.SetCookie(w, &http.Cookie{Name: passkeyCookieName, Value: "", Path: "/", HttpOnly: true, Secure: true,
		SameSite: http.SameSiteLaxMode, MaxAge: -1})
	if err != nil {
		h.passkeyFailed(w, r, "", "That registration expired. Please start again.")
		return
	}

	// The same session that began the ceremony, still recent. A ceremony
	// begun by one session and finished by another — or by the same one after
	// it went stale — is refused.
	current, signedIn, recent := h.signedInRecently(r, now)
	if !signedIn || current.ID != state.SessionID || current.UserID != state.UserID {
		h.passkeyFailed(w, r, state.ReturnTo, "Your sign-in changed while adding the passkey. Please start again.")
		return
	}
	if !recent {
		h.passkeyFailed(w, r, state.ReturnTo, "For your security, sign in again and start over.")
		return
	}

	credential := r.PostFormValue("credential")
	if credential == "" || len(credential) > maxCredentialBytes {
		h.passkeyFailed(w, r, state.ReturnTo, "The passkey could not be added. Please try again.")
		return
	}
	label := strings.TrimSpace(r.PostFormValue("label"))
	if label == "" {
		label = "Passkey"
	}
	if len([]rune(label)) > 64 {
		label = string([]rune(label)[:64])
	}

	factorID, err := h.PasskeyRegistration.Ceremony.FinishRegistration(
		ctx, state.UserID, state.OrgID, label, state.DisplayName, state.Ceremony, []byte(credential))
	if err != nil {
		if errors.Is(err, mfa.ErrWrongCode) {
			h.passkeyFailed(w, r, state.ReturnTo, "The passkey could not be added. Please try again.")
			return
		}
		h.serverError(w, r, "finishing a passkey registration", err)
		return
	}

	var codes []string
	if err := h.DB.WithTenant(ctx, state.OrgID, func(tx *postgres.Tx) error {
		remaining, err := h.PasskeyRegistration.Recovery.Remaining(ctx, tx, state.UserID)
		if err != nil {
			return err
		}
		if remaining == 0 {
			if codes, _, err = h.PasskeyRegistration.Recovery.Issue(
				ctx, tx, state.UserID, state.OrgID, mfa.RecoveryCodeCount, now); err != nil {
				return err
			}
			if err := h.Audit.Write(ctx, tx, audit.Event{
				OrgID: state.OrgID, ActorUserID: state.UserID, Type: audit.EventMFACodesIssued,
				Payload: map[string]any{"count": len(codes), "initiator": "enrolment"},
				IP:      h.clientIP(r),
			}); err != nil {
				return err
			}
		}
		return h.Audit.Write(ctx, tx, audit.Event{
			OrgID: state.OrgID, ActorUserID: state.UserID, Type: audit.EventMFAEnrolled,
			Payload: map[string]any{
				"factor_id": factorID, "factor_type": string(mfa.TypeWebAuthn),
				"recovery_codes_issued": len(codes) > 0,
			},
			IP: h.clientIP(r),
		})
	}); err != nil {
		h.serverError(w, r, "recording a passkey registration", err)
		return
	}

	page := PasskeyDonePage{RecoveryCodes: codes, ReturnTo: state.ReturnTo}
	page.Style, page.StyleHash = renderStyle(DefaultAccent)
	body, err := render(passkeyDoneTemplate, page)
	if err != nil {
		h.serverError(w, r, "rendering the passkey result", err)
		return
	}
	h.write(w, http.StatusOK, page.ContentSecurityPolicy(), body)
}

// passkeyReturn validates where the page may send the user back to.
//
// **Only to an origin the named application registered** (ADR-020's
// `allowed_origins`). An arbitrary return URL would make this page an open
// redirect on the identity provider's own origin — the most trusted origin a
// phishing link could borrow. No return_to at all is valid: the page then says
// "you can close this tab".
func (h *Handler) passkeyReturn(ctx context.Context, clientID, returnTo string) (string, error) {
	if returnTo == "" {
		return "", nil
	}
	if clientID == "" || h.PasskeyRegistration.Clients == nil {
		return "", errors.New("login: a return_to needs the application that owns it")
	}
	target, err := url.Parse(returnTo)
	if err != nil || target.Scheme == "" || target.Host == "" || target.User != nil {
		return "", errors.New("login: return_to is not an absolute URL")
	}
	app, err := h.PasskeyRegistration.Clients(ctx, clientID)
	if err != nil {
		return "", err
	}
	origin := target.Scheme + "://" + target.Host
	for _, allowed := range app.AllowedOrigins {
		if allowed == origin {
			return target.String(), nil
		}
	}
	return "", errors.New("login: return_to is not one of the application's origins")
}

func (h *Handler) passkeyFailed(w http.ResponseWriter, r *http.Request, returnTo, message string) {
	body := message
	if returnTo == "" {
		body += " You can close this tab."
	}
	h.notice(w, r, http.StatusOK, Notice{Title: "The passkey was not added", Body: body})
}

// --- the pages ----------------------------------------------------------------------------

// PasskeyPage runs the ceremony.
type PasskeyPage struct {
	CSRFToken string
	Options   template.JS
	ReturnTo  string

	Style     template.CSS
	StyleHash string
}

// Script is the constant whose hash the policy pins.
func (p PasskeyPage) Script() template.JS { return template.JS(passkeyRegisterScript) }

func (p PasskeyPage) ContentSecurityPolicy() string {
	return strings.Join([]string{
		"default-src 'none'",
		"style-src '" + p.StyleHash + "'",
		"script-src '" + passkeyRegisterScriptHash + "'",
		"img-src 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
		"base-uri 'none'",
	}, "; ")
}

// PasskeyDonePage reports the result, with recovery codes when some were issued.
type PasskeyDonePage struct {
	RecoveryCodes []string
	ReturnTo      string

	Style     template.CSS
	StyleHash string
}

func (p PasskeyDonePage) ContentSecurityPolicy() string {
	return strings.Join([]string{
		"default-src 'none'",
		"style-src '" + p.StyleHash + "'",
		"img-src 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
		"base-uri 'none'",
	}, "; ")
}

// passkeyRegisterScript is the second script in the hosted flow (PG-40).
//
// The registration twin of passkeyScript: read the options already in the page,
// call navigator.credentials.create, put the answer in a hidden field, submit
// the form that is already here. No fetch, no dynamic code.
const passkeyRegisterScript = `
(function () {
  var form = document.getElementById('passkey-register-form');
  var button = document.getElementById('passkey-register-button');
  var options = document.getElementById('passkey-register-options');
  var unsupported = document.getElementById('passkey-register-unsupported');
  if (!form || !button || !options) { return; }

  if (!window.PublicKeyCredential || !navigator.credentials || !navigator.credentials.create) {
    if (unsupported) { unsupported.hidden = false; }
    return;
  }
  button.disabled = false;

  function decode(value) {
    var padded = value.replace(/-/g, '+').replace(/_/g, '/');
    while (padded.length % 4) { padded += '='; }
    var raw = atob(padded);
    var bytes = new Uint8Array(raw.length);
    for (var i = 0; i < raw.length; i++) { bytes[i] = raw.charCodeAt(i); }
    return bytes.buffer;
  }

  function encode(buffer) {
    var bytes = new Uint8Array(buffer);
    var text = '';
    for (var i = 0; i < bytes.length; i++) { text += String.fromCharCode(bytes[i]); }
    return btoa(text).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  }

  button.addEventListener('click', function () {
    button.disabled = true;
    var request = JSON.parse(options.textContent).publicKey;
    request.challenge = decode(request.challenge);
    request.user.id = decode(request.user.id);
    if (request.excludeCredentials) {
      request.excludeCredentials = request.excludeCredentials.map(function (c) {
        return { id: decode(c.id), type: c.type, transports: c.transports };
      });
    }

    navigator.credentials.create({ publicKey: request }).then(function (credential) {
      var transports = credential.response.getTransports ? credential.response.getTransports() : [];
      document.getElementById('passkey-register-credential').value = JSON.stringify({
        id: credential.id,
        rawId: encode(credential.rawId),
        type: credential.type,
        response: {
          attestationObject: encode(credential.response.attestationObject),
          clientDataJSON: encode(credential.response.clientDataJSON),
          transports: transports
        }
      });
      form.submit();
    }).catch(function () {
      button.disabled = false;
    });
  });
})();
`

var passkeyRegisterScriptHash = scriptHash(passkeyRegisterScript)

var passkeyTemplate = template.Must(template.New("passkey").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>Add a passkey</title>
<style>{{.Style}}</style>
</head>
<body>
<main>
<div class="card">
<h1>Add a passkey</h1>
<p>A passkey lets you confirm it is you with this device's fingerprint, face, PIN or security key, instead of a code.</p>
<form method="post" action="/account/passkeys" id="passkey-register-form">
<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
<input type="hidden" name="credential" id="passkey-register-credential">
<div class="field">
<label for="passkey-label">Name this passkey</label>
<input id="passkey-label" name="label" type="text" maxlength="64" autocomplete="off" placeholder="For example, work laptop">
</div>
<p class="note" id="passkey-register-unsupported" hidden>This browser cannot create passkeys. Try a current version of Chrome, Edge, Firefox or Safari.</p>
<button type="button" id="passkey-register-button" disabled>Create passkey</button>
</form>
{{if .ReturnTo}}<p class="foot"><a href="{{.ReturnTo}}">Cancel</a></p>{{end}}
</div>
</main>
<script type="application/json" id="passkey-register-options">{{.Options}}</script>
<script>{{.Script}}</script>
</body>
</html>
`))

var passkeyDoneTemplate = template.Must(template.New("passkey-done").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>Passkey added</title>
<style>{{.Style}}</style>
</head>
<body>
<main>
<div class="card">
<h1>Passkey added</h1>
<p>You can use it the next time you sign in.</p>
{{if .RecoveryCodes}}
<p class="note" role="status"><strong>Save these recovery codes now.</strong> They are the way back into your account if you lose your passkey, and they will not be shown again. Each works once.</p>
<ul id="recovery-codes">
{{range .RecoveryCodes}}<li><code>{{.}}</code></li>
{{end}}</ul>
{{end}}
{{if .ReturnTo}}<p class="foot"><a href="{{.ReturnTo}}">Back to your account</a></p>{{else}}<p class="note">You can close this tab.</p>{{end}}
</div>
</main>
</body>
</html>
`))
