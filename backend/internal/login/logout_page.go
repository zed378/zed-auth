package login

import (
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// The logout confirmation interstitial, and the pages that follow it.
//
// Step 5 of P1-10: a confirmation rather than acting on an unauthenticated
// GET. It carries exactly the login page's headers — strict CSP with a hashed
// stylesheet, unframable, no-referrer, no-store — because it is the same kind
// of surface and the same attacks apply.

// LogoutPage is what the interstitial renders.
//
// Like Page, nothing here comes from the query string except the two values
// that were VALIDATED — the redirect target against the client's registered
// list, and the state, which is opaque and is echoed rather than examined.
type LogoutPage struct {
	AppName  string
	Branding Branding

	Style     template.CSS
	StyleHash string

	CSRFToken string

	// RedirectURI and State are carried through the round trip in hidden
	// fields and re-validated on the way back. See the comment on confirm().
	RedirectURI string
	State       string
	ClientID    string

	// Error is a form-level message, from the constants in page.go.
	Error string

	// needsCookie is set when the CSRF token was minted for this render and
	// the browser has not been given it yet. Kept off the template's reach —
	// it decides a header, not a field.
	needsCookie bool
}

// ContentSecurityPolicy for the interstitial.
//
// The same policy the login page uses, and for the same reasons: no script
// anywhere, the stylesheet whitelisted by hash, images only when there is a
// logo to show, and the form allowed to post back here — and to follow the
// redirect that produces, which lands at the client's registered
// post-logout URI.
//
// `form-action 'self'` alone breaks this page in Chrome exactly as it broke
// the login page (see `Page.RedirectOrigin`): the confirmation posts, the
// service answers 302, and the browser refuses to follow it. The user is left
// on an interstitial that appears to do nothing.
func (p LogoutPage) ContentSecurityPolicy() string {
	img := "'none'"
	if p.Branding.LogoURL != "" {
		img = "https:"
	}

	return strings.Join([]string{
		"default-src 'none'",
		"style-src '" + p.StyleHash + "'",
		"img-src " + img,
		"form-action " + formAction(originOf(p.RedirectURI)),
		"frame-ancestors 'none'",
		"base-uri 'none'",
	}, "; ")
}

// logoutTemplate is the confirmation.
//
// No script, no inline handler, no style attribute — the same construction as
// the login form, and the same test asserts it.
//
// The "everywhere" checkbox is docs/PLAN/05's "log out of all sessions"
// button. It is unchecked by default, deliberately: signing out of one
// application should not silently end a user's session in every other one,
// and a default that did would be a surprise measured in other people's
// support tickets.
var logoutTemplate = template.Must(template.New("logout").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Sign out</title>
<style>{{.Style}}</style>
</head>
<body>
<main>
<div class="card">
{{if .Branding.LogoURL}}<img class="mark" src="{{.Branding.LogoURL}}" alt="">{{end}}
<h1>{{if .AppName}}Sign out of {{.AppName}}?{{else}}Sign out?{{end}}</h1>
{{if .Error}}
<div class="alert" role="alert" tabindex="-1" autofocus><p>{{.Error}}</p></div>
{{end}}
<form method="post" action="/oidc/logout">
<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
{{if .RedirectURI}}<input type="hidden" name="post_logout_redirect_uri" value="{{.RedirectURI}}">{{end}}
{{if .State}}<input type="hidden" name="state" value="{{.State}}">{{end}}
{{if .ClientID}}<input type="hidden" name="client_id" value="{{.ClientID}}">{{end}}
<div class="field">
<label class="check"><input type="checkbox" name="everywhere" value="1"> Sign out of every application</label>
</div>
<button type="submit" {{if not .Error}}autofocus{{end}}>Sign out</button>
</form>
</div>
</main>
</body>
</html>
`))

// ask renders the interstitial.
func (h *LogoutHandler) ask(w http.ResponseWriter, r *http.Request, req request) {
	page, err := h.page(r, req)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	h.observe(LogoutAsked)
	h.renderPage(w, r, http.StatusOK, page)
}

// render re-renders the interstitial with a message, and a fresh CSRF token so
// the retry works.
func (h *LogoutHandler) render(w http.ResponseWriter, r *http.Request, req request, message string) {
	page, err := h.page(r, req)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	fresh, tokenErr := newCSRFToken()
	if tokenErr != nil {
		h.serverError(w, r, tokenErr)
		return
	}
	http.SetCookie(w, csrfCookie(fresh))
	page.CSRFToken = fresh
	page.Error = message

	h.renderPage(w, r, http.StatusOK, page)
}

// page assembles the interstitial, minting a CSRF token if the browser has none.
func (h *LogoutHandler) page(r *http.Request, req request) (LogoutPage, error) {
	branding := DefaultBranding

	if req.App.OrgID != "" && h.Brandings != nil {
		err := h.DB.WithTenant(r.Context(), req.App.OrgID, func(tx *postgres.Tx) error {
			found, _, err := h.Brandings.Branding(r.Context(), tx, req.App.OrgID)
			if err != nil {
				return err
			}
			branding = found
			return nil
		})
		if err != nil {
			// A logo is not worth failing a logout over.
			if h.Log != nil {
				h.Log.Warn("reading branding for the logout page failed", "error", err.Error())
			}
		}
	}

	style, hash := renderStyle(branding.AccentColor)

	page := LogoutPage{
		AppName:     req.App.Name,
		Branding:    branding,
		Style:       style,
		StyleHash:   hash,
		RedirectURI: req.RedirectURI,
		State:       req.State,
		ClientID:    req.App.ID,
	}

	token, ok := csrfFromRequest(r)
	if !ok {
		fresh, err := newCSRFToken()
		if err != nil {
			return LogoutPage{}, err
		}
		token = fresh
		page.needsCookie = true
	}
	page.CSRFToken = token

	return page, nil
}

// renderPage writes the interstitial with its headers.
func (h *LogoutHandler) renderPage(
	w http.ResponseWriter, r *http.Request, status int, page LogoutPage,
) {
	if page.needsCookie {
		http.SetCookie(w, csrfCookie(page.CSRFToken))
	}

	body, err := render(logoutTemplate, page)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	writePage(w, status, page.ContentSecurityPolicy(), body)
}

// done sends the user wherever the request said, or tells them it is finished.
func (h *LogoutHandler) done(w http.ResponseWriter, r *http.Request, req request) {
	if req.RedirectURI == "" {
		// Nowhere to send them, and inventing somewhere is the vulnerability
		// this endpoint spends most of its code avoiding.
		h.notice(w, r, http.StatusOK, Notice{
			Title: "Signed out",
			Body:  "You have been signed out. You can close this page.",
		})
		return
	}

	target, err := url.Parse(req.RedirectURI)
	if err != nil {
		// Unreachable: it matched a registered URI exactly, and those are
		// validated at registration. Handled rather than ignored.
		h.notice(w, r, http.StatusOK, Notice{
			Title: "Signed out",
			Body:  "You have been signed out. You can close this page.",
		})
		return
	}

	if req.State != "" {
		query := target.Query()
		// Echoed unchanged and never examined. It is the application's value
		// and it is opaque to us.
		query.Set("state", req.State)
		target.RawQuery = query.Encode()
	}

	http.Redirect(w, r, target.String(), http.StatusFound)
}

// refuse answers a request whose redirect target is not registered.
//
// No redirect, and no logout. Reporting the error by redirecting to the
// unvalidated address IS the open-redirect vulnerability, delivered by the
// code meant to prevent it — the same branch P1-06 calls the most important
// one in its file.
func (h *LogoutHandler) refuse(w http.ResponseWriter, r *http.Request, err error) {
	if h.Log != nil {
		h.Log.Info("a logout request was refused", "reason", err.Error())
	}
	h.observe(LogoutRefused)

	h.notice(w, r, http.StatusBadRequest, Notice{
		Title: "We can't complete this sign-out",
		Body: "The application asked us to return you to an address it has not " +
			"registered. Nothing has changed about your session. Please contact " +
			"the application that sent you here.",
	})
}

func (h *LogoutHandler) badRequest(w http.ResponseWriter, r *http.Request, detail string) {
	if h.Log != nil {
		h.Log.Info("a logout submission was rejected", "detail", detail)
	}
	h.observe(LogoutRefused)
	h.notice(w, r, http.StatusBadRequest, Notice{
		Title: "We couldn't read that",
		Body:  "Please try again.",
	})
}

func (h *LogoutHandler) serverError(w http.ResponseWriter, r *http.Request, err error) {
	if h.Log != nil && err != nil {
		h.Log.Error("rendering the sign-out page failed", "error", err.Error())
	}
	h.observe(LogoutError)
	h.notice(w, r, http.StatusInternalServerError, Notice{
		Title: "Something went wrong",
		Body:  "Please try again in a moment.",
	})
}

func (h *LogoutHandler) notice(w http.ResponseWriter, _ *http.Request, status int, notice Notice) {
	notice.Style, notice.StyleHash = renderStyle(DefaultAccent)

	body, err := render(noticeTemplate, notice)
	if err != nil {
		if h.Log != nil {
			h.Log.Error("rendering a logout notice failed", "error", err.Error())
		}
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
		return
	}
	writePage(w, status, notice.ContentSecurityPolicy(), body)
}

func (h *LogoutHandler) observe(outcome string) {
	if h.Observer != nil {
		h.Observer.Logout(outcome)
	}
}
