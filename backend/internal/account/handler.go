// Package account is the caller's own account: their profile and their
// password (P3-12).
//
// The personal settings screen's API. Everything here takes the user from the
// token — there is no parameter that names a user — so a member with no
// administrative role can manage their own account without being handed a
// route that could reach anybody else's.
package account

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/authn"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/ratelimit"
	"github.com/zed378/zed-auth/backend/internal/session"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Credentials verifies and reads a user. Satisfied by authn.UserStore.
type Credentials interface {
	ByID(ctx context.Context, tx *postgres.Tx, userID string) (authn.User, error)
	Authenticate(ctx context.Context, tx *postgres.Tx, email, password string) (authn.User, bool, error)
}

// Passwords stores a new password. Satisfied by user.Store.
type Passwords interface {
	SetPassword(ctx context.Context, tx *postgres.Tx, id, plaintext string, verifyEmail bool, now time.Time) error
}

// Validator decides whether a new password is acceptable. Satisfied by
// authn.PasswordValidator.
type Validator interface {
	Validate(ctx context.Context, tx *postgres.Tx, orgID, userID, password string) error
}

// Policies reads the organization's password policy. Satisfied by
// authn.PolicyStore.
type Policies interface {
	Policy(ctx context.Context, tx *postgres.Tx, orgID string) (authn.Policy, error)
}

// Limiter is sign-in's per-address bound (P1-13). Satisfied by ratelimit.Limiter.
type Limiter interface {
	Check(ctx context.Context, address, ip string, now time.Time) (ratelimit.Decision, string)
	Fail(ctx context.Context, address, ip string, now time.Time) (bool, string)
	Succeed(ctx context.Context, address string)
}

// Sessions signs out every other session. Satisfied by session.Manager.
type Sessions interface {
	RevokeOthers(ctx context.Context, tx *postgres.Tx, userID, keep string, now time.Time) (session.Revocation, error)
}

// RefreshRevoker ends refresh tokens by session. Satisfied by token.RefreshStore.
type RefreshRevoker interface {
	RevokeForSessions(ctx context.Context, tx *postgres.Tx, sessionIDs []string) (int64, error)
}

// Handler implements GetMe and ChangeMyPassword.
type Handler struct {
	DB          *postgres.DB
	Audit       management.Recorder
	Credentials Credentials
	Passwords   Passwords
	Validator   Validator
	Policies    Policies
	Limiter     Limiter
	Sessions    Sessions
	Refresh     RefreshRevoker

	// BreachChecked reports whether new passwords are checked against a corpus,
	// so the screen states the rule truthfully rather than promising a check
	// that is switched off.
	BreachChecked bool

	Log *slog.Logger
	Now func() time.Time
}

func (h *Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// GetMe reads the caller's account and the password rules that apply to them.
func (h *Handler) GetMe(ctx context.Context, _ api.GetMeRequestObject) (api.GetMeResponseObject, error) {
	caller, err := self(ctx)
	if err != nil {
		return nil, err
	}

	var (
		account authn.User
		orgName string
		policy  authn.Policy
	)
	if err := h.DB.WithTenant(ctx, caller.OrgID, func(tx *postgres.Tx) error {
		var err error
		if account, err = h.Credentials.ByID(ctx, tx, caller.UserID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx,
			`SELECT name FROM organizations WHERE id = $1`, caller.OrgID).Scan(&orgName); err != nil {
			return err
		}
		policy, err = h.Policies.Policy(ctx, tx, caller.OrgID)
		return err
	}); err != nil {
		return nil, fmt.Errorf("account: reading the caller's account: %w", err)
	}

	userID, err := uuid.Parse(account.ID)
	if err != nil {
		return nil, fmt.Errorf("account: a user id is not a uuid: %w", err)
	}
	orgID, err := uuid.Parse(caller.OrgID)
	if err != nil {
		return nil, fmt.Errorf("account: an organization id is not a uuid: %w", err)
	}

	out := api.Me{
		Id:                userID,
		Email:             account.Email,
		DisplayName:       nullable.NewNullNullable[string](),
		PasswordChangedAt: nullable.NewNullNullable[time.Time](),
		PasswordPolicy: api.MyPasswordPolicy{
			MinLength:        policy.MinLength,
			RequireUppercase: policy.RequireUppercase,
			MaxAgeDays:       policy.MaxAgeDays,
			BreachChecked:    h.BreachChecked,
		},
	}
	out.Organization.Id = orgID
	out.Organization.Name = orgName
	if account.DisplayName != "" {
		out.DisplayName = nullable.NewNullableWithValue(account.DisplayName)
	}
	if account.PasswordChangedAt != nil {
		out.PasswordChangedAt = nullable.NewNullableWithValue(*account.PasswordChangedAt)
	}
	return api.GetMe200JSONResponse(out), nil
}

// ChangeMyPassword changes the caller's password.
func (h *Handler) ChangeMyPassword(
	ctx context.Context, request api.ChangeMyPasswordRequestObject,
) (api.ChangeMyPasswordResponseObject, error) {
	caller, err := self(ctx)
	if err != nil {
		return nil, err
	}
	if request.Body == nil || request.Body.CurrentPassword == "" || request.Body.NewPassword == "" {
		return nil, management.Fault{
			Class: management.Invalid, Message: "Both the current and the new password are required.",
			Reason: "a password change with a field missing",
		}
	}
	if caller.SessionID == "" {
		// A token with no session belongs to no person at a keyboard, and has
		// no session to keep while signing the others out.
		return nil, management.Fault{
			Class: management.Invalid, Message: "This token is not tied to a sign-in session.",
			Reason: "a password change with a token that carries no sid",
		}
	}
	current, chosen := request.Body.CurrentPassword, request.Body.NewPassword
	now := h.now()

	var (
		email      string
		revocation session.Revocation
		wrong      bool
	)
	err = h.DB.WithTenant(ctx, caller.OrgID, func(tx *postgres.Tx) error {
		account, err := h.Credentials.ByID(ctx, tx, caller.UserID)
		if err != nil {
			return err
		}
		email = account.Email

		// Sign-in's bound, keyed on the same address, BEFORE the Argon2
		// verification — so this is not a second, unthrottled place to guess a
		// password, and a refused attempt costs no hash.
		if decision, _ := h.Limiter.Check(ctx, email, "", now); !decision.Allowed {
			return management.Fault{
				Class: management.RateLimited, Message: "Too many attempts. Wait a few minutes and try again.",
				Reason: "the per-address sign-in bound is in cooldown",
			}
		}

		_, verified, err := h.Credentials.Authenticate(ctx, tx, email, current)
		if err != nil {
			return err
		}
		if !verified {
			wrong = true
			return errWrongPassword
		}

		if chosen == current {
			return management.Fault{
				Class: management.Invalid, Message: "Choose a password different from your current one.",
				Details: []api.ErrorDetail{{Field: "new_password", Issue: "is the same as the current password"}},
				Reason:  "a password change to the same password",
			}
		}

		if err := h.Validator.Validate(ctx, tx, caller.OrgID, caller.UserID, chosen); err != nil {
			var rejected authn.PasswordRejected
			if errors.As(err, &rejected) {
				details := make([]api.ErrorDetail, 0, len(rejected.Violations))
				for _, v := range rejected.Violations {
					details = append(details, api.ErrorDetail{Field: "new_password", Issue: v.Message})
				}
				return management.Fault{
					Class: management.Invalid, Message: "That password does not meet the requirements.",
					Details: details, Reason: "a new password refused by policy or the breach corpus",
				}
			}
			return err
		}

		if err := h.Passwords.SetPassword(ctx, tx, caller.UserID, chosen, false, now); err != nil {
			return err
		}

		// Everybody else out. A password change is what somebody does when they
		// think somebody else knows the old one, and a session that survived it
		// would be that somebody, still in.
		if revocation, err = h.Sessions.RevokeOthers(ctx, tx, caller.UserID, caller.SessionID, now); err != nil {
			return err
		}
		refreshRevoked, err := h.Refresh.RevokeForSessions(ctx, tx, revocation.SessionIDs)
		if err != nil {
			return err
		}

		return management.Audit(ctx, h.Audit, tx, audit.Event{
			OrgID: caller.OrgID, Type: audit.EventPasswordChanged,
			Payload: map[string]any{
				"user_id":                caller.UserID,
				"via":                    "self-service",
				"sessions_revoked":       len(revocation.SessionIDs),
				"refresh_tokens_revoked": refreshRevoked,
			},
		})
	})

	if wrong {
		// Counted OUTSIDE the transaction that rolled back, so the failure is
		// recorded even though nothing else from this request was.
		h.Limiter.Fail(ctx, email, "", now)
		return nil, management.Fault{
			Class: management.Invalid, Message: "Your current password is not correct.",
			Details: []api.ErrorDetail{{Field: "current_password", Issue: "is not correct"}},
			Reason:  "a password change with the wrong current password",
		}
	}
	if err != nil {
		var fault management.Fault
		if errors.As(err, &fault) {
			return nil, err
		}
		return nil, fmt.Errorf("account: changing the password: %w", err)
	}

	h.Limiter.Succeed(ctx, email)
	if revocation.Invalidate != nil {
		if err := revocation.Invalidate(context.Background()); err != nil {
			return nil, management.Fault{
				Class: management.Internal, Message: "An unexpected error occurred.",
				Reason: "the password changed but other sessions could not be removed from the cache: " + err.Error(),
			}
		}
	}
	return api.ChangeMyPassword204Response{}, nil
}

var errWrongPassword = errors.New("account: the current password is not correct")

func self(ctx context.Context) (management.Caller, error) {
	caller, ok := management.CallerFrom(ctx)
	if !ok || caller.UserID == "" || caller.OrgID == "" {
		return management.Caller{}, management.Fault{
			Class: management.Internal, Message: "An unexpected error occurred.",
			Reason: "a self-scoped account route ran with no caller",
		}
	}
	return caller, nil
}
