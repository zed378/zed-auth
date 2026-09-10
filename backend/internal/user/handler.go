package user

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/mail"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/ratelimit"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// The seven user operations (P1-19.1 – P1-19.3, P1-19.4's administrator half).
//
// The self-service flows — the forgot form and the set-password page — are
// hosted HTML and live with P1-12's login page, not here. They are
// unauthenticated, and putting an unauthenticated endpoint inside the
// authenticated surface is how one ends up accidentally exempted from
// something.

// Revoker ends a user's sessions. Satisfied by session.Manager.
type Revoker interface {
	RevokeAllForUser(
		ctx context.Context, tx *postgres.Tx, userID, orgID, actorUserID string, now time.Time,
	) (func(context.Context) error, error)
}

// RefreshRevoker revokes a user's refresh tokens. Satisfied by token.RefreshStore.
type RefreshRevoker interface {
	RevokeAllForUser(ctx context.Context, tx *postgres.Tx, userID string) (int64, error)
}

// MailLimiter bounds how often one address may be mailed. Satisfied by
// ratelimit.Quotas.
type MailLimiter interface {
	ConsumeMail(ctx context.Context, address string, now time.Time) ratelimit.Verdict
}

// Handler implements the generated user operations.
type Handler struct {
	Store *Store
	DB    *postgres.DB
	Audit management.Recorder
	Log   *slog.Logger

	// Sessions and Refresh are what make a deactivation real. Either one nil
	// means a deactivated user keeps working through that channel, so New
	// refuses to build a handler without them.
	Sessions Revoker
	Refresh  RefreshRevoker

	// Mailer is optional. A deployment with no SMTP configured still invites
	// users; the response says the message was not sent (ADR-018).
	Mailer mail.Sender

	// MailLimit bounds messages per RECIPIENT (card step 4, docs/SECURITY/02
	// §10). Keyed on the address rather than the caller, because the abuse is
	// flooding a third party: the caller is entitled to be there, and it is
	// the mailbox that suffers. Optional; nil means no bound.
	MailLimit MailLimiter

	// BaseURL is where a link in an email points. Without it there is nothing
	// to put in the message, so an unset one is a configuration error rather
	// than a silent no-op.
	BaseURL string

	Now func() time.Time
}

func (h *Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// --- list -------------------------------------------------------------------------------

func (h *Handler) ListUsers(
	ctx context.Context, request api.ListUsersRequestObject,
) (api.ListUsersResponseObject, error) {
	size := management.PageSize(intParam(request.Params.PageSize))

	cursor, err := management.DecodeCursor(stringParam(request.Params.PageToken))
	if err != nil {
		return nil, err
	}

	var rows []User
	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		var err error
		rows, err = h.Store.List(ctx, tx, stringParam(request.Params.Search), cursor, size)
		return err
	}); err != nil {
		return nil, err
	}

	page, err := management.Paginate(rows, size, Position)
	if err != nil {
		return nil, err
	}

	out := api.UserList{Users: make([]api.User, 0, len(page.Items))}
	for _, u := range page.Items {
		rendered, err := render(u)
		if err != nil {
			return nil, err
		}
		out.Users = append(out.Users, rendered)
	}
	if page.NextPageToken != "" {
		out.PageInfo = &api.PageInfo{NextPageToken: &page.NextPageToken}
	}

	return api.ListUsers200JSONResponse(out), nil
}

// --- create -----------------------------------------------------------------------------

func (h *Handler) CreateUser(
	ctx context.Context, request api.CreateUserRequestObject,
) (api.CreateUserResponseObject, error) {
	if request.Body == nil {
		return nil, missingBody()
	}
	if err := refuseImmutableFields(ctx, "creating"); err != nil {
		return nil, err
	}

	email, err := validEmail(string(request.Body.Email))
	if err != nil {
		return nil, err
	}
	username, err := bounded("username", strValue(request.Body.Username), MaxUsernameLength)
	if err != nil {
		return nil, err
	}
	displayName, err := bounded("display_name", strValue(request.Body.DisplayName), MaxDisplayNameLength)
	if err != nil {
		return nil, err
	}

	send := true
	if request.Body.SendInviteEmail != nil {
		send = *request.Body.SendInviteEmail
	}

	var (
		created User
		token   Token
	)
	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		var err error
		if created, err = h.Store.Create(ctx, tx, email, username, displayName); err != nil {
			return err
		}
		if token, err = h.Store.IssueToken(
			ctx, tx, created.ID, PurposeInvite, InviteLifetime, h.now()); err != nil {
			return err
		}
		return h.write(ctx, tx, audit.EventUserCreated, map[string]any{
			"user_id": created.ID,
			"email":   created.Email,
			"status":  created.Status,
			"invited": true,
		})
	}); err != nil {
		return nil, faultFrom(err)
	}

	// **Outside the transaction, and after it committed.** ADR-018: the
	// account exists whether or not a mail server does, and a user rolled back
	// because SMTP was busy is a worse outcome than one who exists and has not
	// been mailed.
	sent := false
	if send {
		sent = h.deliver(ctx, mail.Invitation(
			created.Email, h.organizationName(ctx), h.actorLabel(ctx),
			h.link(token.Plaintext), "three days"))
	}

	rendered, err := render(created)
	if err != nil {
		return nil, err
	}
	return api.CreateUser201JSONResponse(api.UserCreated{
		Id: rendered.Id, Email: rendered.Email, Username: rendered.Username,
		DisplayName: rendered.DisplayName, Status: rendered.Status,
		MfaEnabled: rendered.MfaEnabled, EmailVerified: rendered.EmailVerified,
		CreatedAt: rendered.CreatedAt, UpdatedAt: rendered.UpdatedAt,
		InviteEmailSent: sent,
	}), nil
}

// --- read -------------------------------------------------------------------------------

func (h *Handler) GetUser(
	ctx context.Context, request api.GetUserRequestObject,
) (api.GetUserResponseObject, error) {
	var found User
	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		var err error
		found, err = h.Store.Get(ctx, tx, request.UserId.String())
		return err
	}); err != nil {
		return nil, faultFrom(err)
	}

	rendered, err := render(found)
	if err != nil {
		return nil, err
	}
	return api.GetUser200JSONResponse(rendered), nil
}

// --- update -----------------------------------------------------------------------------

func (h *Handler) UpdateUser(
	ctx context.Context, request api.UpdateUserRequestObject,
) (api.UpdateUserResponseObject, error) {
	if request.Body == nil {
		return nil, missingBody()
	}
	if err := refuseImmutableFields(ctx, "updating"); err != nil {
		return nil, err
	}

	var profile Profile
	if request.Body.Email != nil {
		email, err := validEmail(string(*request.Body.Email))
		if err != nil {
			return nil, err
		}
		profile.Email = &email
	}
	if request.Body.Username.IsSpecified() {
		value, _ := request.Body.Username.Get()
		if _, err := bounded("username", value, MaxUsernameLength); err != nil {
			return nil, err
		}
		profile.Username = &value
	}
	if request.Body.DisplayName.IsSpecified() {
		value, _ := request.Body.DisplayName.Get()
		if _, err := bounded("display_name", value, MaxDisplayNameLength); err != nil {
			return nil, err
		}
		profile.DisplayName = &value
	}

	var (
		before  User
		updated User
	)
	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		var err error
		if before, err = h.Store.Get(ctx, tx, request.UserId.String()); err != nil {
			return err
		}
		if updated, err = h.Store.UpdateProfile(ctx, tx, request.UserId.String(), profile); err != nil {
			return err
		}
		// Field by field, not `before == updated`: User carries a *time.Time,
		// and comparing structs would compare the POINTERS — two equal
		// timestamps at different addresses would read as a change, and every
		// no-op update would write an event.
		if before.Email == updated.Email &&
			before.Username == updated.Username &&
			before.DisplayName == updated.DisplayName {
			// Nothing changed. No event, for the reason a rename to the same
			// name writes none (P1-17).
			return nil
		}
		payload := map[string]any{"user_id": updated.ID}
		if before.Email != updated.Email {
			// The addresses by value: a changed login address is the single
			// most consequential profile edit, and it is also what an account
			// takeover looks like.
			payload["email_before"] = before.Email
			payload["email_after"] = updated.Email
			payload["email_verification_cleared"] = before.Verified() && !updated.Verified()
		}
		if before.Username != updated.Username {
			payload["username_changed"] = true
		}
		if before.DisplayName != updated.DisplayName {
			payload["display_name_changed"] = true
		}
		return h.write(ctx, tx, audit.EventUserUpdated, payload)
	}); err != nil {
		return nil, faultFrom(err)
	}

	rendered, err := render(updated)
	if err != nil {
		return nil, err
	}
	return api.UpdateUser200JSONResponse(rendered), nil
}

// --- deactivate and reactivate -----------------------------------------------------------

// DeactivateUser bars an account and ends everything it can still do.
//
// **Three revocations, all in this request.** The status alone stops the next
// login; it does nothing about a session cookie already in a browser or a
// refresh token already in an application's store. Any one of the three left
// out leaves a deactivated user working, which is the card's fourth DoD item
// and the reason it says "within one request cycle".
func (h *Handler) DeactivateUser(
	ctx context.Context, request api.DeactivateUserRequestObject,
) (api.DeactivateUserResponseObject, error) {
	if h.Sessions == nil || h.Refresh == nil {
		// A deactivation that cannot revoke is not a deactivation, and the
		// worst possible outcome is a 204 that left the user working. Refused
		// loudly instead — and refused HERE rather than by a nil dereference,
		// so the message names the missing dependency.
		return nil, management.Fault{
			Class: management.Internal, Message: "An unexpected error occurred.",
			Reason: "deactivation requires both the session and refresh-token revokers",
		}
	}

	id := request.UserId.String()
	now := h.now()

	var invalidate func(context.Context) error

	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		updated, previous, err := h.Store.SetStatus(ctx, tx, id, StatusDeactivated)
		if err != nil {
			return err
		}

		// 1. Sessions, plus the cache tombstones that make the revocation
		//    visible to the session cache rather than only to the table.
		if invalidate, err = h.Sessions.RevokeAllForUser(
			ctx, tx, id, tx.OrgID(), actor(ctx), now); err != nil {
			return err
		}

		// 2. Refresh tokens. A session ends the browser's access; a refresh
		//    token is a consumer application's, and it outlives the browser.
		refreshRevoked, err := h.Refresh.RevokeAllForUser(ctx, tx, id)
		if err != nil {
			return err
		}

		// 3. Any live invitation or reset link, which would otherwise be a way
		//    back in that nobody is watching.
		linksRetired, err := h.Store.RetireTokens(ctx, tx, id, now)
		if err != nil {
			return err
		}

		if previous == StatusDeactivated {
			// Already deactivated. The revocations above still ran — they are
			// idempotent and cheap, and running them makes a repeat request a
			// way to be sure rather than a no-op. No event, because nothing
			// changed.
			return nil
		}

		return h.write(ctx, tx, audit.EventUserDeactivated, map[string]any{
			"user_id":                updated.ID,
			"previous_status":        previous,
			"refresh_tokens_revoked": refreshRevoked,
			"links_retired":          linksRetired,
		})
	}); err != nil {
		return nil, faultFrom(err)
	}

	// The cache tombstones. A failure here is an error rather than a warning:
	// the session table says revoked and the cache would still say valid, and
	// the cache is what the next request reads.
	if invalidate != nil {
		if err := invalidate(ctx); err != nil {
			return nil, management.Fault{
				Class: management.Internal, Message: "An unexpected error occurred.",
				Reason: "the user was deactivated but the session cache could not be invalidated: " + err.Error(),
			}
		}
	}

	return api.DeactivateUser204Response{}, nil
}

func (h *Handler) ReactivateUser(
	ctx context.Context, request api.ReactivateUserRequestObject,
) (api.ReactivateUserResponseObject, error) {
	id := request.UserId.String()

	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		current, err := h.Store.Get(ctx, tx, id)
		if err != nil {
			return err
		}
		if current.Status != StatusDeactivated {
			return management.Fault{
				Class:   management.Conflict,
				Message: "That user is not deactivated.",
				Reason:  "reactivating a user whose status is " + current.Status,
			}
		}

		// Back to `active` if they ever had a password, and to `invited` if
		// they never did — otherwise reactivating somebody who was deactivated
		// before accepting their invitation would leave an `active` account
		// with no password, which cannot sign in and does not look broken.
		target := StatusActive
		var hasPassword bool
		if err := tx.QueryRow(ctx,
			`SELECT password_hash IS NOT NULL FROM users WHERE id = $1`, id).Scan(&hasPassword); err != nil {
			return fmt.Errorf("user: checking for a password: %w", err)
		}
		if !hasPassword {
			target = StatusInvited
		}

		updated, previous, err := h.Store.SetStatus(ctx, tx, id, target)
		if err != nil {
			return err
		}
		return h.write(ctx, tx, audit.EventUserReactivated, map[string]any{
			"user_id":         updated.ID,
			"previous_status": previous,
			"status":          updated.Status,
		})
	}); err != nil {
		return nil, faultFrom(err)
	}

	return api.ReactivateUser204Response{}, nil
}

// --- administrator-triggered reset ---------------------------------------------------------

// ResetUserPassword issues a reset link and mails it to the user.
//
// **The link is not in the response.** The administrator triggering this
// cannot see it, and that is the point: a reset link is a bearer credential
// for the account, so the only party who should hold it is the one who can
// read that mailbox. An administrator who could read it could take over any
// account in the organization without leaving a trace that says so.
func (h *Handler) ResetUserPassword(
	ctx context.Context, request api.ResetUserPasswordRequestObject,
) (api.ResetUserPasswordResponseObject, error) {
	id := request.UserId.String()

	var (
		target User
		token  Token
	)
	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		var err error
		if target, err = h.Store.Get(ctx, tx, id); err != nil {
			return err
		}
		if target.Status == StatusDeactivated {
			return management.Fault{
				Class:   management.Conflict,
				Message: "That user is deactivated. Reactivate them first.",
				Reason:  "a reset was requested for a deactivated user",
			}
		}
		if token, err = h.Store.IssueToken(
			ctx, tx, id, PurposeReset, ResetLifetime, h.now()); err != nil {
			return err
		}
		return h.write(ctx, tx, audit.EventPasswordResetSent, map[string]any{
			"user_id":   target.ID,
			"initiator": "administrator",
		})
	}); err != nil {
		return nil, faultFrom(err)
	}

	sent := h.deliver(ctx, mail.PasswordReset(
		target.Email, h.organizationName(ctx), h.link(token.Plaintext), "one hour"))

	return api.ResetUserPassword202JSONResponse(api.ResetRequested{EmailSent: sent}), nil
}

// --- shared ------------------------------------------------------------------------------

// deliver attempts a send and reports whether it worked.
//
// Never returns an error, deliberately: no caller of this may fail a request
// because of it (ADR-018). The failure is logged and counted inside the mail
// package.
func (h *Handler) deliver(ctx context.Context, msg mail.Message) bool {
	if h.Mailer == nil {
		return false
	}
	// The recipient's bound, checked here rather than at each call site: every
	// message this service sends goes through this function, so there is one
	// place the bound can be missing rather than three.
	if h.MailLimit != nil {
		if verdict := h.MailLimit.ConsumeMail(ctx, msg.To, h.now()); !verdict.Allowed {
			if h.Log != nil {
				// The address is NOT logged. A line per refused recipient
				// would be an enumeration list in the log store.
				h.Log.Warn("a message was not sent because the recipient's rate limit was reached",
					"retry_after_seconds", int(verdict.RetryAfter.Seconds()))
			}
			return false
		}
	}
	if h.BaseURL == "" {
		if h.Log != nil {
			h.Log.Error("cannot send a link with no base URL configured")
		}
		return false
	}
	return h.Mailer.Send(ctx, msg) == nil
}

// link builds the URL in an email.
//
// The token is a query parameter, which puts it in the recipient's browser
// history and in this service's access log unless something removes it. P0-09's
// redaction covers `token`, and the set-password page below never logs its own
// URL — but the honest summary is that a link in an email is a credential in a
// place with weak custody, which is why the lifetimes are short and the use is
// single.
func (h *Handler) link(plaintext string) string {
	return strings.TrimRight(h.BaseURL, "/") + "/password/set?token=" + url.QueryEscape(plaintext)
}

// organizationName is what an email says the invitation is to.
func (h *Handler) organizationName(ctx context.Context) string {
	orgID, _ := management.ScopeFrom(ctx)
	if orgID == "" {
		return "your organization"
	}

	var name string
	err := h.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		return tx.QueryRow(ctx, `SELECT name FROM organizations WHERE id = $1`, orgID).Scan(&name)
	})
	if err != nil || strings.TrimSpace(name) == "" {
		return "your organization"
	}
	return name
}

// actorLabel names who sent an invitation, for the message.
//
// Their display name or address, and an empty string when neither is
// available — the template then omits the sentence rather than saying an
// invitation came from nobody.
func (h *Handler) actorLabel(ctx context.Context) string {
	caller, ok := management.CallerFrom(ctx)
	if !ok || caller.UserID == "" {
		return ""
	}

	orgID, _ := management.ScopeFrom(ctx)
	var label string
	err := h.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT coalesce(nullif(display_name, ''), email) FROM users WHERE id = $1`,
			caller.UserID).Scan(&label)
	})
	if err != nil {
		return ""
	}
	return label
}

func (h *Handler) inScope(ctx context.Context, fn func(*postgres.Tx) error) error {
	orgID, _ := management.ScopeFrom(ctx)
	if orgID == "" {
		return management.Fault{
			Class: management.Internal, Message: "An unexpected error occurred.",
			Reason: "a user operation ran with no tenant scope",
		}
	}
	return h.DB.WithTenant(ctx, orgID, fn)
}

func (h *Handler) write(
	ctx context.Context, tx *postgres.Tx, kind audit.EventType, payload map[string]any,
) error {
	if h.Audit == nil {
		return nil
	}
	return management.Audit(ctx, h.Audit, tx, audit.Event{Type: kind, Payload: payload})
}

func actor(ctx context.Context) string {
	caller, _ := management.CallerFrom(ctx)
	return caller.UserID
}

// refuseImmutableFields rejects a body naming something this API does not set.
//
// From the RAW body: `additionalProperties: false` is documentation and
// json.Unmarshal discards what it does not recognise (P1-16), so
// `{"status":"active"}` would otherwise get a 200 and a response still saying
// `invited` — a caller told a privilege change happened when it did not, which
// is abuse case A-5.
func refuseImmutableFields(ctx context.Context, doing string) error {
	raw, ok := management.RawBody(ctx)
	if !ok {
		return management.Fault{
			Class: management.Internal, Message: "An unexpected error occurred.",
			Reason: "the request body was not buffered, so refused fields cannot be detected",
		}
	}
	if len(raw) == 0 {
		return nil
	}

	var sent map[string]json.RawMessage
	if err := json.Unmarshal(raw, &sent); err != nil {
		return management.Fault{
			Class: management.Invalid, Message: "The request body could not be read.",
			Reason: "body did not decode: " + err.Error(),
		}
	}

	var refused []api.ErrorDetail
	for _, field := range []string{
		// Privilege and identity. `status` has its own operations; `roles` and
		// `manager_roles` are not on this surface at all and naming them must
		// not look like it worked.
		"status", "email_verified", "email_verified_at", "id", "org_id",
		"roles", "manager_roles", "mfa_enabled",
		// A password never crosses this API. Accepting one here would let an
		// administrator set a password for somebody else, which is exactly the
		// capability the invitation flow exists to avoid.
		"password", "password_hash",
	} {
		if _, present := sent[field]; present {
			refused = append(refused, api.ErrorDetail{Field: field, Issue: reasonFor(field)})
		}
	}
	if len(refused) == 0 {
		return nil
	}

	return management.Fault{
		Class:   management.Invalid,
		Message: "That field cannot be set here.",
		Details: refused,
		Reason:  "a refused field appeared in a body while " + doing + " a user",
	}
}

func reasonFor(field string) string {
	switch field {
	case "status":
		return "use the deactivate or reactivate operation"
	case "password", "password_hash":
		return "a password is set by its owner, through a link sent to their address"
	case "email_verified", "email_verified_at":
		return "verification records what happened; it is not a setting"
	case "roles", "manager_roles":
		return "roles are not managed through this endpoint"
	case "mfa_enabled":
		return "multi-factor enrolment is the user's own action"
	default:
		return "cannot be changed after the user is created"
	}
}

func faultFrom(err error) error {
	var fault management.Fault
	switch {
	case err == nil:
		return nil
	case errors.As(err, &fault):
		return err
	case errors.Is(err, ErrNotFound):
		return management.Fault{
			Class:   management.NotFound,
			Message: "The requested resource was not found.",
			Reason:  "no such user in this organization",
		}
	case errors.Is(err, ErrEmailTaken):
		return management.Fault{
			Class:   management.Conflict,
			Message: "A user with that email address already exists in this organization.",
			Details: []api.ErrorDetail{{Field: "email", Issue: "already in use"}},
			Reason:  "email uniqueness violation",
		}
	case errors.Is(err, ErrUsernameTaken):
		return management.Fault{
			Class:   management.Conflict,
			Message: "A user with that username already exists in this organization.",
			Details: []api.ErrorDetail{{Field: "username", Issue: "already in use"}},
			Reason:  "username uniqueness violation",
		}
	}
	return err
}

func missingBody() error {
	return management.Fault{
		Class: management.Invalid, Message: "A request body is required.", Reason: "no body",
	}
}

// validEmail checks the shape and returns the stored form.
//
// Structural only — an `@` with something either side and a bounded length.
// Anything more ambitious rejects addresses that work: the authoritative test
// of an address is whether a message to it arrives, which is what the
// invitation is.
func validEmail(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	invalid := func(issue string) error {
		return management.Fault{
			Class: management.Invalid, Message: "That email address is not valid.",
			Details: []api.ErrorDetail{{Field: "email", Issue: issue}},
			Reason:  "email failed structural validation",
		}
	}

	switch {
	case trimmed == "":
		return "", invalid("is required")
	case len(trimmed) > MaxEmailLength:
		return "", invalid(fmt.Sprintf("must be at most %d characters", MaxEmailLength))
	}

	at := strings.LastIndex(trimmed, "@")
	if at < 1 || at == len(trimmed)-1 || strings.Contains(trimmed, " ") {
		return "", invalid("must look like name@example.com")
	}
	return trimmed, nil
}

func bounded(field, value string, max int) (string, error) {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) > max {
		return "", management.Fault{
			Class: management.Invalid, Message: "That value is too long.",
			Details: []api.ErrorDetail{{
				Field: field, Issue: fmt.Sprintf("must be at most %d characters", max),
			}},
			Reason: field + " over the length bound",
		}
	}
	return trimmed, nil
}

// render turns a stored user into the API resource.
//
// **There is no branch here that could emit a password or a hash.** User
// carries neither field, so this is a property of the type rather than a rule
// this function follows.
func render(u User) (api.User, error) {
	id, err := uuid.Parse(u.ID)
	if err != nil {
		return api.User{}, fmt.Errorf("user: %q is not a uuid: %w", u.ID, err)
	}

	out := api.User{
		Id:            id,
		Email:         openapi_types.Email(u.Email),
		Status:        api.UserStatus(u.Status),
		MfaEnabled:    u.MFAEnabled,
		EmailVerified: u.Verified(),
		CreatedAt:     u.CreatedAt,
		UpdatedAt:     u.UpdatedAt,
	}
	// Set only when present. A nullable left unspecified is omitted from the
	// JSON entirely, which is the honest rendering of "this user has no
	// username" — unlike an explicit null, which the update schema uses to
	// mean "clear it".
	if u.Username != "" {
		out.Username = nullable.NewNullableWithValue(u.Username)
	}
	if u.DisplayName != "" {
		out.DisplayName = nullable.NewNullableWithValue(u.DisplayName)
	}
	return out, nil
}

func strValue(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func intParam(p *int) string {
	if p == nil {
		return ""
	}
	return fmt.Sprintf("%d", *p)
}

func stringParam(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
