package login

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zed378/zed-auth/backend/internal/anomaly"
	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/mail"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/user"
)

// "This wasn't me" (P3-08 F-5).
//
// Specification: MEMORY/specs/P3-08-login-anomaly-detection.md.
//
//	GET  /account/not-me?token=...   a confirmation page, and nothing else
//	POST /account/not-me             sign out everywhere, clear the password, mail a reset link
//
// **The GET changes nothing.** Several mail clients fetch every link in a
// message as it arrives — for previews, for malware scanning — and a link that
// acted on GET would sign a user out of everything the moment the notice landed,
// before they had read a word of it (abuse case A-5).
//
// **The POST grants exactly one power**, and it is a destructive one aimed at
// the account's own owner: end every session and every refresh token, clear the
// password, and send a reset link to the address already on file. It cannot sign
// anybody in and cannot choose a password — so a thief who somehow held the link
// could lock the account, but could not take it (A-2), and the reset goes to the
// mailbox the link was sent to in the first place.

// NotMePath is where the page is mounted.
const NotMePath = "/account/not-me"

// NotMeFlow is what the page needs beyond PasswordFlow.
//
// The token store, lookup, mailer and base URL are PasswordFlow's — the page
// issues a reset link through exactly the same machinery the forgot page uses,
// so there is one reset implementation rather than two.
type NotMeFlow struct {
	Sessions NotMeSessions

	// Refresh is optional in the type and not in intent: without it a user who
	// reported a stranger's login is left with that stranger's refresh tokens
	// still minting access tokens. main.go always sets it.
	Refresh RefreshRevoker

	// Passwords clears the stored hash.
	Passwords PasswordClearer
}

// NotMeSessions ends every session a user has.
type NotMeSessions interface {
	RevokeAllForUser(ctx context.Context, tx *postgres.Tx, userID, orgID, actorUserID string, now time.Time) (func(context.Context) error, error)
}

// PasswordClearer removes a stored password.
type PasswordClearer interface {
	ClearPassword(ctx context.Context, tx *postgres.Tx, userID string) error
}

// errReportLinkSpent is the token failing to consume inside the transaction:
// used by a second submission, expired between the page and the button.
var errReportLinkSpent = errors.New("login: the report link was already used or has expired")

// NotMe serves both methods.
func (h *Handler) NotMe(w http.ResponseWriter, r *http.Request) {
	if h.Password == nil || h.Reports == nil {
		h.notice(w, r, http.StatusNotFound, Notice{
			Title: "Not available",
			Body:  "This service is not configured to handle sign-in reports.",
		})
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.showNotMe(w, r, r.URL.Query().Get("token"), "")
	case http.MethodPost:
		h.submitNotMe(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		h.notice(w, r, http.StatusMethodNotAllowed, Notice{
			Title: "Method not allowed",
			Body:  "This page accepts GET and POST.",
		})
	}
}

// showNotMe renders the confirmation. It consumes nothing.
func (h *Handler) showNotMe(w http.ResponseWriter, r *http.Request, token, problem string) {
	if !h.reportLinkValid(r.Context(), token) {
		h.invalidReportLink(w, r)
		return
	}

	csrf, err := h.csrfFor(w, r)
	if err != nil {
		h.serverError(w, r, "issuing a CSRF token", err)
		return
	}

	page := NotMePage{CSRFToken: csrf, Token: token, Error: problem}
	page.Style, page.StyleHash = renderStyle(DefaultAccent)

	body, err := render(notMeTemplate, page)
	if err != nil {
		h.serverError(w, r, "rendering the sign-in report page", err)
		return
	}
	h.write(w, http.StatusOK, page.ContentSecurityPolicy(), body)
}

// reportLinkValid resolves a token and checks it is a report link.
//
// The purpose check is the same allow-list discipline SetPassword applies: a
// reset or invitation token must not open this page, and a report token must not
// open that one. A link that works on a page it was never issued for is a
// credential with more power than whoever issued it meant to give.
func (h *Handler) reportLinkValid(ctx context.Context, token string) bool {
	if token == "" {
		return false
	}
	claim, err := h.Password.Lookup(ctx, token, h.now())
	return err == nil && claim.Purpose == user.PurposeReportNotMe
}

// notMeOutcome is what the transaction decided, for the work after commit.
type notMeOutcome struct {
	invalidate func(context.Context) error
	email      string
	orgName    string
	reset      user.Token
	resetReady bool
}

func (h *Handler) submitNotMe(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.invalidReportLink(w, r)
		return
	}

	token := r.PostFormValue("token")
	if !checkCSRF(r, r.PostFormValue("csrf_token")) {
		// A stale form, not a bad link. Re-render so the person can press the
		// button again rather than being told their link is broken.
		h.showNotMe(w, r, token, "That form expired. Please try again.")
		return
	}

	ctx := r.Context()
	claim, err := h.Password.Lookup(ctx, token, h.now())
	if err != nil || claim.Purpose != user.PurposeReportNotMe {
		h.invalidReportLink(w, r)
		return
	}

	outcome, err := h.reportNotMe(ctx, token, claim)
	switch {
	case errors.Is(err, errReportLinkSpent):
		h.invalidReportLink(w, r)
		return
	case err != nil:
		// Not the invalid-link page. The link is still good — the transaction
		// rolled back, so it was not consumed — and telling somebody who
		// believes a stranger is in their account that their link is dead
		// would stop them trying again.
		h.serverError(w, r, "handling a sign-in report", err)
		return
	}

	// After commit, in this order: the cache first, so a stranger's session
	// stops resolving as soon as possible; then the mail.
	if outcome.invalidate != nil {
		if err := outcome.invalidate(ctx); err != nil && h.Log != nil {
			// The rows are revoked; the cache entries expire on their own TTL.
			// Worth an operator's attention, not worth failing the page over.
			h.Log.Warn("invalidating sessions after a sign-in report failed", "error", err.Error())
		}
	}

	sent := h.sendReportReset(ctx, outcome)

	body := "Every session on your account has ended, and your password has been cleared so it " +
		"can no longer be used. "
	if sent {
		body += "We have sent a link to choose a new password to your email address. " +
			"If it does not arrive, use “Forgot your password?” on the sign-in page."
	} else {
		body += "Please ask your organization's administrator to set a new password for you, " +
			"or use “Forgot your password?” on the sign-in page."
	}

	h.notice(w, r, http.StatusOK, Notice{
		Title:    "You have been signed out everywhere",
		Body:     body,
		BackPath: Path,
	})
}

// reportNotMe does the work in one transaction.
//
// **One transaction, so a report is all or nothing.** A report that ended the
// sessions but left the password, or cleared the password but left a refresh
// token minting access tokens, would be a user told they are safe who is not.
func (h *Handler) reportNotMe(ctx context.Context, token string, claim user.Claim) (notMeOutcome, error) {
	var out notMeOutcome
	now := h.now()

	err := h.DB.WithTenant(ctx, claim.OrgID, func(tx *postgres.Tx) error {
		consumed, err := h.Password.Users.ConsumeToken(ctx, tx, token, user.PurposeReportNotMe, now)
		if errors.Is(err, user.ErrTokenInvalid) {
			return errReportLinkSpent
		}
		if err != nil {
			return err
		}
		userID := consumed.UserID

		out.invalidate, err = h.Reports.Sessions.RevokeAllForUser(ctx, tx, userID, claim.OrgID, userID, now)
		if err != nil {
			return err
		}

		var refreshRevoked int64
		if h.Reports.Refresh != nil {
			if refreshRevoked, err = h.Reports.Refresh.RevokeAllForUser(ctx, tx, userID); err != nil {
				return err
			}
		}

		// Every outstanding link, not only report links. An invitation or a
		// reset that somebody else requested while in the account is a way back
		// in that nobody is watching.
		if _, err := h.Password.Users.RetireTokens(ctx, tx, userID, now); err != nil {
			return err
		}

		if err := h.Reports.Passwords.ClearPassword(ctx, tx, userID); err != nil {
			return err
		}

		target, err := h.Password.Users.Get(ctx, tx, userID)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx,
			`SELECT name FROM organizations WHERE id = $1`, claim.OrgID).Scan(&out.orgName); err != nil {
			return err
		}

		// A reset link only when there is somewhere to send it and an account
		// that may use it. Issued inside the transaction so it exists exactly
		// when the password was cleared.
		if h.Password.Mailer != nil && target.Status != user.StatusDeactivated {
			if out.reset, err = h.Password.Users.IssueToken(
				ctx, tx, userID, user.PurposeReset, user.ResetLifetime, now); err != nil {
				return err
			}
			out.email = target.Email
			out.resetReady = true
		}

		if h.Audit == nil {
			return nil
		}
		return h.Audit.Write(ctx, tx, audit.Event{
			OrgID:       claim.OrgID,
			ActorUserID: userID,
			Type:        audit.EventLoginReportedNotMe,
			Payload: map[string]any{
				"user_id":                userID,
				"sessions_revoked":       "all",
				"refresh_tokens_revoked": refreshRevoked,
				"password_cleared":       true,
				"reset_link_issued":      out.resetReady,
			},
		})
	})
	return out, err
}

// sendReportReset mails the reset link. Reports whether it was handed off.
//
// The mail quota applies here too. It is a smaller amplification vector than
// the forgot form — a report link is single use — but one bound for every
// message the service sends to an address is simpler to reason about than a
// list of exemptions.
func (h *Handler) sendReportReset(ctx context.Context, o notMeOutcome) bool {
	if !o.resetReady {
		return false
	}
	if h.Password.MailLimit != nil {
		if verdict := h.Password.MailLimit.ConsumeMail(ctx, o.email, h.now()); !verdict.Allowed {
			return false
		}
	}
	if err := h.Password.Mailer.Send(ctx, mail.PasswordReset(
		o.email, o.orgName, h.resetLink(o.reset.Plaintext), "one hour")); err != nil {
		if h.Log != nil {
			h.Log.Error("sending the reset link after a sign-in report failed", "error", err.Error())
		}
		return false
	}
	return true
}

// invalidReportLink is the one answer for every way a report link can fail.
//
// It names the likeliest benign cause — a newer notice replaced this link — and
// the way forward that does not need the link at all.
func (h *Handler) invalidReportLink(w http.ResponseWriter, r *http.Request) {
	h.notice(w, r, http.StatusOK, Notice{
		Title: "This link is not valid",
		Body: "It may have expired, it may already have been used, or a newer notice may have " +
			"replaced it. If you still think somebody else signed in to your account, use " +
			"“Forgot your password?” on the sign-in page to choose a new one.",
		BackPath: Path,
	})
}

// --- the notifier ------------------------------------------------------------------------

// AnomalyMail is the anomaly.Notifier: it emails the user a notice carrying a
// report link.
//
// Lives here rather than in `anomaly` because the link it builds points at a
// page this package serves, and the token it issues is this package's
// PasswordFlow's — `anomaly` stays ignorant of pages, tokens and mail.
type AnomalyMail struct {
	Users     *user.Store
	DB        Tenant
	Mailer    mail.Sender
	MailLimit MailLimiter
	BaseURL   string

	// Now is overridable for tests.
	Now func() time.Time
}

func (n *AnomalyMail) now() time.Time {
	if n.Now != nil {
		return n.Now()
	}
	return time.Now()
}

// reportValidFor is ReportNotMeLifetime in words, for the notice.
const reportValidFor = "7 days"

// NotifyAnomaly sends one notice.
//
// **The quota is consulted before a token is issued**, because issuing retires
// the previous report link. A notice the quota then refused would have killed
// the link in the last notice the user did receive, and left them with none.
func (n *AnomalyMail) NotifyAnomaly(ctx context.Context, f anomaly.Finding) error {
	if n == nil || n.Mailer == nil {
		return nil
	}

	var (
		target  user.User
		orgName string
	)
	if err := n.DB.WithTenant(ctx, f.OrgID, func(tx *postgres.Tx) error {
		var err error
		if target, err = n.Users.Get(ctx, tx, f.UserID); err != nil {
			return err
		}
		return tx.QueryRow(ctx,
			`SELECT name FROM organizations WHERE id = $1`, f.OrgID).Scan(&orgName)
	}); err != nil {
		return err
	}
	if target.Status == user.StatusDeactivated || target.Email == "" {
		return nil
	}

	if n.MailLimit != nil {
		if verdict := n.MailLimit.ConsumeMail(ctx, target.Email, n.now()); !verdict.Allowed {
			return nil
		}
	}

	var token user.Token
	if err := n.DB.WithTenant(ctx, f.OrgID, func(tx *postgres.Tx) error {
		var err error
		token, err = n.Users.IssueToken(ctx, tx, f.UserID,
			user.PurposeReportNotMe, user.ReportNotMeLifetime, n.now())
		return err
	}); err != nil {
		return err
	}

	link := strings.TrimRight(n.BaseURL, "/") + NotMePath + "?token=" + url.QueryEscape(token.Plaintext)

	return n.Mailer.Send(ctx, mail.LoginAnomaly(
		target.Email, orgName, anomaly.Reasons(f.Signals),
		f.At.UTC().Format("2 January 2006 at 15:04 UTC"), f.Where, link, reportValidFor))
}

// --- the page -----------------------------------------------------------------------------

// NotMePage is the confirmation.
type NotMePage struct {
	CSRFToken string

	// Token is in a hidden field, not the form's action URL, for the reason
	// SetPasswordPage gives.
	Token string
	Error string

	Style     template.CSS
	StyleHash string
}

func (p NotMePage) ContentSecurityPolicy() string {
	return strings.Join([]string{
		"default-src 'none'",
		"style-src '" + p.StyleHash + "'",
		"img-src 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
		"base-uri 'none'",
	}, "; ")
}

// The button says what it does in full. It is the one destructive action a
// person reaches from an email, and "Continue" would be a way to sign
// somebody out of everything without their having understood that it would.
var notMeTemplate = template.Must(template.New("not-me").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>Report a sign-in</title>
<style>{{.Style}}</style>
</head>
<body>
<main>
<div class="card">
<h1>This sign-in wasn't me</h1>
{{if .Error}}
<div class="alert" role="alert" tabindex="-1" autofocus><p>{{.Error}}</p></div>
{{end}}
<p>If you did not sign in, confirm below. We will:</p>
<ul>
<li>sign your account out on every device, including this one,</li>
<li>clear your password so it can no longer be used, and</li>
<li>email you a link to choose a new one.</li>
</ul>
<p>If the sign-in was you after all, close this page. Nothing has changed.</p>
<form method="post" action="/account/not-me">
<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
<input type="hidden" name="token" value="{{.Token}}">
<button type="submit" class="danger">Sign out everywhere and reset my password</button>
</form>
</div>
</main>
</body>
</html>
`))
