package login

import (
	"context"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/mail"
	"github.com/zed378/zed-auth/backend/internal/ratelimit"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/user"
)

// The two hosted pages a person uses to take possession of an account
// (P1-19.4, P1-19.5).
//
//	GET/POST /login/forgot      — ask for a reset link
//	GET/POST /password/set      — choose a password, using an invite or reset link
//
// They live here rather than under /v1 because they are unauthenticated, and
// an unauthenticated endpoint inside the authenticated surface is how one ends
// up accidentally exempted from something. They share this package's branding,
// CSRF, notice rendering and header discipline — a second copy of any of those
// is a second thing to get wrong on the one surface where it matters.

// PasswordFlow is what the two pages need beyond what Handler already holds.
type PasswordFlow struct {
	// Users issues, resolves and consumes tokens, and writes the password.
	Users *user.Store

	// Policy validates a chosen password against the organization's own rules
	// (P1-02). Nil is a configuration error rather than a permissive default:
	// a set-password page with no policy is one that accepts "1".
	Policy PasswordPolicy

	// Mailer is optional (ADR-018). With none, a reset request answers exactly
	// as it would have — see below.
	Mailer mail.Sender

	// Lookup resolves a token without consuming it.
	//
	// A function rather than a *postgres.DB, because resolving a link before a
	// tenant is known needs the SECURITY DEFINER path and this package holds
	// only the Tenant interface — deliberately, so its routing, headers, CSRF
	// and uniformity can be tested without a container.
	Lookup func(ctx context.Context, token string, now time.Time) (user.Claim, error)

	// BaseURL is where the link points.
	BaseURL string

	// MailLimit bounds messages per recipient (card step 4). The self-service
	// forgot form is the more exposed of the two amplification vectors: it is
	// unauthenticated, so anybody who can reach the login page can ask for a
	// message to be sent to any address they can name.
	MailLimit MailLimiter
}

// MailLimiter bounds how often one address may be mailed.
type MailLimiter interface {
	ConsumeMail(ctx context.Context, address string, now time.Time) ratelimit.Verdict
}

// PasswordPolicy evaluates a candidate password for an organization.
type PasswordPolicy interface {
	Validate(ctx context.Context, tx *postgres.Tx, orgID, userID, password string) error
}

// --- the forgot form --------------------------------------------------------------------

// Forgot asks for an address and always says the same thing.
//
// **The response is identical whether the address exists, does not exist, is
// deactivated, or the mail server is down** (`docs/SECURITY/02` §12). That is
// the entire security property of this endpoint, and it is why the handler is
// written as one path with no early return that a caller could distinguish.
func (h *Handler) Forgot(w http.ResponseWriter, r *http.Request) {
	if h.Password == nil {
		// Self-service reset is not configured. The honest answer, and the one
		// P1-12 shipped before this existed.
		h.notice(w, r, http.StatusOK, Notice{
			Title: "Resetting your password",
			Body: "Self-service password reset is not available yet. " +
				"Please ask your organization's administrator to set a new password for you.",
			BackPath: h.backToLogin(r),
		})
		return
	}

	if r.Method == http.MethodGet {
		h.showForgotForm(w, r, "")
		return
	}

	if err := r.ParseForm(); err != nil {
		h.showForgotForm(w, r, "That form could not be read. Please try again.")
		return
	}
	if !checkCSRF(r, r.PostFormValue("csrf_token")) {
		h.showForgotForm(w, r, "That form expired. Please try again.")
		return
	}

	requestID := r.PostFormValue("request")
	email := strings.TrimSpace(r.PostFormValue("email"))

	// Everything below is best-effort and NONE of it changes what the browser
	// sees. The work happens; the answer does not depend on it.
	h.issueReset(r.Context(), requestID, email)

	h.notice(w, r, http.StatusOK, Notice{
		Title: "Check your email",
		Body: "If an account exists for that address, a link to choose a new password " +
			"is on its way. The link can be used once and expires in an hour.",
		BackPath: backPath(requestID),
	})
}

// issueReset does the work, and tells the caller nothing.
//
// No error is returned and none is rendered. An unknown address, a deactivated
// account, a mail server refusing the recipient and a database failure must all
// look the same from outside, so the only place any of them is visible is the
// log and the metrics.
func (h *Handler) issueReset(ctx context.Context, requestID, email string) {
	if requestID == "" || email == "" {
		return
	}

	pending, err := h.Authorization.Peek(ctx, requestID)
	if err != nil {
		// No pending authorization request means no organization, and a reset
		// belongs to one tenant. Nothing to do — and nothing said.
		return
	}

	var (
		target  user.User
		token   user.Token
		orgName string
		found   bool
	)
	err = h.DB.WithTenant(ctx, pending.App.OrgID, func(tx *postgres.Tx) error {
		// The organization's name, for the message. Read inside the same
		// transaction as everything else so a message never names a tenant the
		// reset was not actually issued against.
		if err := tx.QueryRow(ctx,
			`SELECT name FROM organizations WHERE id = $1`, pending.App.OrgID).Scan(&orgName); err != nil {
			return err
		}

		var err error
		target, err = h.Password.Users.ByEmail(ctx, tx, email)
		if err != nil {
			// Not found, and that is an ordinary outcome here rather than an
			// error. Returning nil keeps it out of the error log, where a line
			// per probed address would be the enumeration list itself.
			return nil
		}
		if target.Status == user.StatusDeactivated {
			return nil
		}

		if token, err = h.Password.Users.IssueToken(
			ctx, tx, target.ID, user.PurposeReset, user.ResetLifetime, h.now()); err != nil {
			return err
		}
		found = true

		// Written ONLY because a user was found. An event per probe would put
		// every attempted address in the audit log, durably and searchably.
		if h.Audit == nil {
			return nil
		}
		return h.Audit.Write(ctx, tx, audit.Event{
			OrgID:       pending.App.OrgID,
			Type:        audit.EventPasswordResetSent,
			ActorUserID: target.ID,
			Payload: map[string]any{
				"user_id":   target.ID,
				"initiator": "self-service",
			},
		})
	})
	if err != nil && h.Log != nil {
		h.Log.Error("issuing a password reset failed", "error", err.Error())
	}
	if !found || h.Password.Mailer == nil {
		return
	}

	if h.Password.MailLimit != nil {
		if verdict := h.Password.MailLimit.ConsumeMail(ctx, target.Email, h.now()); !verdict.Allowed {
			// Refused, and the browser is told exactly what it was told for an
			// address that does not exist. A different answer here would make
			// the bound itself an enumeration oracle: "you are being rate
			// limited" confirms the account.
			return
		}
	}

	_ = h.Password.Mailer.Send(ctx, mail.PasswordReset(
		target.Email, orgName, h.resetLink(token.Plaintext), "one hour"))
}

func (h *Handler) showForgotForm(w http.ResponseWriter, r *http.Request, problem string) {
	token, err := h.csrfFor(w, r)
	if err != nil {
		h.serverError(w, r, "issuing a CSRF token", err)
		return
	}

	requestID := r.URL.Query().Get("request")
	if requestID == "" {
		requestID = r.PostFormValue("request")
	}

	page := ForgotPage{
		CSRFToken: token,
		RequestID: requestID,
		Error:     problem,
		BackPath:  backPath(requestID),
	}
	page.Style, page.StyleHash = renderStyle(DefaultAccent)

	body, err := render(forgotTemplate, page)
	if err != nil {
		h.serverError(w, r, "rendering the forgot page", err)
		return
	}
	h.write(w, http.StatusOK, page.ContentSecurityPolicy(), body)
}

// --- the set-password page --------------------------------------------------------------

// SetPassword renders and processes the page an invite or reset link opens.
func (h *Handler) SetPassword(w http.ResponseWriter, r *http.Request) {
	if h.Password == nil {
		h.notice(w, r, http.StatusNotFound, Notice{
			Title: "Not available",
			Body:  "This service is not configured to set passwords through a link.",
		})
		return
	}

	if r.Method == http.MethodGet {
		h.showSetPasswordForm(w, r, r.URL.Query().Get("token"), "")
		return
	}

	if err := r.ParseForm(); err != nil {
		h.invalidLink(w, r)
		return
	}

	token := r.PostFormValue("token")
	if !checkCSRF(r, r.PostFormValue("csrf_token")) {
		// The form is stale rather than the link. Re-render it so the person
		// can submit again, rather than telling them their link is broken.
		h.showSetPasswordForm(w, r, token, "That form expired. Please try again.")
		return
	}

	password := r.PostFormValue("password")
	confirm := r.PostFormValue("password_confirm")
	if password != confirm {
		h.showSetPasswordForm(w, r, token, "Those passwords do not match.")
		return
	}

	claim, err := h.Password.Lookup(r.Context(), token, h.now())
	if err != nil {
		h.invalidLink(w, r)
		return
	}

	var policyProblem string
	now := h.now()
	err = h.DB.WithTenant(r.Context(), claim.OrgID, func(tx *postgres.Tx) error {
		// The policy first, so a rejected password does not burn the token.
		// Consuming it and then refusing the password would leave the person
		// holding a dead link and no account.
		if h.Password.Policy != nil {
			if problem := h.Password.Policy.Validate(
				r.Context(), tx, claim.OrgID, claim.UserID, password); problem != nil {
				policyProblem = problem.Error()
				return nil
			}
		}

		// One conditional UPDATE decides who wins a race; see ConsumeToken.
		consumed, err := h.Password.Users.ConsumeToken(r.Context(), tx, token, claim.Purpose, now)
		if err != nil {
			return err
		}

		// The invitation flow proves the address was reachable, which is what
		// verification means (PG-18). A reset does not extend that claim:
		// widening it here would be a security statement made by accident.
		verify := consumed.Purpose == user.PurposeInvite
		if err := h.Password.Users.SetPassword(
			r.Context(), tx, consumed.UserID, password, verify, now); err != nil {
			return err
		}

		if h.Audit == nil {
			return nil
		}
		kind := audit.EventPasswordChanged
		if verify {
			kind = audit.EventUserInviteAccepted
		}
		return h.Audit.Write(r.Context(), tx, audit.Event{
			OrgID:       claim.OrgID,
			ActorUserID: consumed.UserID,
			Type:        kind,
			Payload: map[string]any{
				"user_id": consumed.UserID,
				"via":     consumed.Purpose,
			},
		})
	})

	switch {
	case err != nil:
		if h.Log != nil {
			h.Log.Error("setting a password failed", "error", err.Error())
		}
		h.invalidLink(w, r)
		return
	case policyProblem != "":
		h.showSetPasswordForm(w, r, token, policyProblem)
		return
	}

	// Deliberately no session. This page sets a password; it does not sign
	// anybody in. A page that both consumes an emailed link and issues a
	// session is a second authentication path with none of the login page's
	// rate limiting, and it would let a stolen link become a live session in
	// one step rather than two.
	h.notice(w, r, http.StatusOK, Notice{
		Title:    "Your password is set",
		Body:     "You can now sign in with your new password.",
		BackPath: Path,
	})
}

func (h *Handler) showSetPasswordForm(w http.ResponseWriter, r *http.Request, token, problem string) {
	if token == "" {
		h.invalidLink(w, r)
		return
	}

	claim, err := h.Password.Lookup(r.Context(), token, h.now())
	if err != nil {
		h.invalidLink(w, r)
		return
	}

	csrf, err := h.csrfFor(w, r)
	if err != nil {
		h.serverError(w, r, "issuing a CSRF token", err)
		return
	}

	page := SetPasswordPage{
		CSRFToken: csrf,
		Token:     token,
		Invite:    claim.Purpose == user.PurposeInvite,
		Error:     problem,
	}
	page.Style, page.StyleHash = renderStyle(DefaultAccent)

	body, err := render(setPasswordTemplate, page)
	if err != nil {
		h.serverError(w, r, "rendering the set-password page", err)
		return
	}
	h.write(w, http.StatusOK, page.ContentSecurityPolicy(), body)
}

// invalidLink is the one answer for every way a link can fail.
//
// Expired, already used, never existed, or malformed — all the same page.
// Distinguishing them would let a holder of an expired token learn it was once
// real, and would let anybody with a guess learn whether one exists.
func (h *Handler) invalidLink(w http.ResponseWriter, r *http.Request) {
	h.notice(w, r, http.StatusOK, Notice{
		Title: "This link is not valid",
		Body: "It may have expired, or it may already have been used. " +
			"Ask for a new one, or ask your administrator to invite you again.",
		BackPath: Path,
	})
}

func (h *Handler) resetLink(plaintext string) string {
	return strings.TrimRight(h.Password.BaseURL, "/") +
		"/password/set?token=" + url.QueryEscape(plaintext)
}

func (h *Handler) backToLogin(r *http.Request) string {
	return backPath(r.URL.Query().Get("request"))
}

func backPath(requestID string) string {
	if !validPendingID(requestID) {
		return ""
	}
	return Path + "?request=" + url.QueryEscape(requestID)
}

// --- the pages --------------------------------------------------------------------------

// ForgotPage is the address form.
type ForgotPage struct {
	CSRFToken string
	RequestID string
	Error     string
	BackPath  string

	Style     template.CSS
	StyleHash string
}

func (p ForgotPage) ContentSecurityPolicy() string {
	return strings.Join([]string{
		"default-src 'none'",
		"style-src '" + p.StyleHash + "'",
		"img-src 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
		"base-uri 'none'",
	}, "; ")
}

// SetPasswordPage is the password form.
type SetPasswordPage struct {
	CSRFToken string

	// Token is rendered into a hidden field rather than kept in the form's
	// action URL, so it does not travel again in a Referer header when the
	// browser follows the redirect afterwards.
	Token  string
	Invite bool
	Error  string

	Style     template.CSS
	StyleHash string
}

func (p SetPasswordPage) ContentSecurityPolicy() string {
	return strings.Join([]string{
		"default-src 'none'",
		"style-src '" + p.StyleHash + "'",
		"img-src 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
		"base-uri 'none'",
	}, "; ")
}

// Same discipline as the login page: no script, no event-handler attribute, no
// style attribute, and no value interpolated from the query string except the
// token, which is escaped by html/template into a hidden input's value.
var forgotTemplate = template.Must(template.New("forgot").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Reset your password</title>
<style>{{.Style}}</style>
</head>
<body>
<main>
<div class="card">
<h1>Reset your password</h1>
<p>Enter the address you sign in with. If an account exists for it, we will send a link to choose a new password.</p>
{{if .Error}}
<div class="alert" role="alert" tabindex="-1" autofocus><p>{{.Error}}</p></div>
{{end}}
<form method="post" action="/login/forgot">
<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
<input type="hidden" name="request" value="{{.RequestID}}">
<div class="field">
<label for="email">Email address</label>
<input id="email" name="email" type="email" inputmode="email" autocomplete="username"
 autocapitalize="none" spellcheck="false" required {{if not .Error}}autofocus{{end}}>
</div>
<button type="submit">Send the link</button>
</form>
{{if .BackPath}}<p class="alt"><a href="{{.BackPath}}">Back to sign in</a></p>{{end}}
</div>
</main>
</body>
</html>
`))

var setPasswordTemplate = template.Must(template.New("set-password").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>{{if .Invite}}Set your password{{else}}Choose a new password{{end}}</title>
<style>{{.Style}}</style>
</head>
<body>
<main>
<div class="card">
<h1>{{if .Invite}}Set your password{{else}}Choose a new password{{end}}</h1>
{{if .Invite}}<p>Choose a password to finish setting up your account.</p>{{end}}
{{if .Error}}
<div class="alert" role="alert" tabindex="-1" autofocus><p>{{.Error}}</p></div>
{{end}}
<form method="post" action="/password/set">
<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
<input type="hidden" name="token" value="{{.Token}}">
<div class="field">
<label for="password">New password</label>
<input id="password" name="password" type="password" autocomplete="new-password" required
 {{if not .Error}}autofocus{{end}}>
</div>
<div class="field">
<label for="password_confirm">Confirm new password</label>
<input id="password_confirm" name="password_confirm" type="password" autocomplete="new-password" required>
</div>
<button type="submit">Save the password</button>
</form>
</div>
</main>
</body>
</html>
`))
