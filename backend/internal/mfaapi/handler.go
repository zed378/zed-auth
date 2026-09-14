// Package mfaapi is the Management API's view of second factors (P3-10).
//
// Specification: MEMORY/specs/P3-10-mfa-tab.md.
//
// **The API nobody's card assigned (PG-42).** P3-02, P3-04 and P3-05 each built
// a mechanism — TOTP enrolment, recovery codes, passkey registration — and
// deferred "the endpoint". This is it, and it carries the requirements those
// tasks passed forward: enrolment needs recent authentication, enrolment is
// audited, and recovery codes arrive with the first factor.
//
// Two shapes of caller, as in sessionapi: a person managing their own factors
// under /v1/me/mfa, where the user comes from the token and nowhere else; and an
// administrator reading a member's, which is read-only by design. An
// administrator who could add a factor to somebody else's account could sign in
// as them, so no route here lets one — `POST .../mfa-reset` (P3-04) is the only
// administrator write, and it only ever removes.
package mfaapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"rsc.io/qr"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/authn"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/mfa"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/user"
)

// RecentAuthentication is mfa.RecentAuthentication, named here for readers of
// this package.
const RecentAuthentication = mfa.RecentAuthentication

// maxLabel bounds a factor's name.
const maxLabel = 64

// Factors reads and removes factors. Satisfied by mfa.Store.
type Factors interface {
	ForUser(ctx context.Context, tx *postgres.Tx, userID string) ([]mfa.Factor, error)
	Delete(ctx context.Context, tx *postgres.Tx, factorID string) error
}

// Enroller begins and confirms a TOTP enrolment. Satisfied by mfa.TOTP.
type Enroller interface {
	Begin(ctx context.Context, userID, orgID, label string) (mfa.Enrolment, error)
	Confirm(ctx context.Context, factorID, code string) error
}

// Recovery counts and issues recovery codes. Satisfied by mfa.RecoveryStore.
type Recovery interface {
	Remaining(ctx context.Context, tx *postgres.Tx, userID string) (int, error)
	Issue(ctx context.Context, tx *postgres.Tx, userID, orgID string, count int, now time.Time) ([]string, string, error)
}

// Attempts is the per-user bound shared with the sign-in challenge.
// Satisfied by mfa.RedisAttempts.
type Attempts interface {
	Allowed(ctx context.Context, userID string, now time.Time) (bool, error)
	Fail(ctx context.Context, userID string, now time.Time) (bool, error)
}

// Policies reads the organization's mandate. Satisfied by authn.PolicyStore.
type Policies interface {
	LoginPolicy(ctx context.Context, tx *postgres.Tx, orgID string) (authn.LoginPolicy, error)
}

// Members confirms a user exists in the scoped organization. Satisfied by
// user.Store.
type Members interface {
	Get(ctx context.Context, tx *postgres.Tx, id string) (user.User, error)
}

// Handler implements the six generated operations.
type Handler struct {
	DB       *postgres.DB
	Audit    management.Recorder
	Factors  Factors
	Recovery Recovery
	Policies Policies
	Members  Members

	// Enroller and Attempts are nil when MFA is not configured on this
	// deployment. Reads still work — a user's factor list is simply empty —
	// and every write says the feature is unavailable rather than failing.
	Enroller Enroller
	Attempts Attempts

	// PasskeysAvailable reports whether the hosted registration page can run a
	// ceremony (P3-05's relying party was built).
	PasskeysAvailable bool

	Log *slog.Logger
	Now func() time.Time
}

func (h *Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// --- reading --------------------------------------------------------------------------

// GetMyMfa reads the caller's factors.
func (h *Handler) GetMyMfa(ctx context.Context, _ api.GetMyMfaRequestObject) (api.GetMyMfaResponseObject, error) {
	caller, err := self(ctx)
	if err != nil {
		return nil, err
	}

	out := api.MyMfa{Factors: []api.MfaFactor{}, AvailableTypes: []api.MyMfaAvailableTypes{}}
	if h.Enroller != nil {
		out.AvailableTypes = append(out.AvailableTypes, api.MyMfaAvailableTypesTotp)
	}
	if h.PasskeysAvailable {
		out.AvailableTypes = append(out.AvailableTypes, api.MyMfaAvailableTypesWebauthn)
	}

	if err := h.DB.WithTenant(ctx, caller.OrgID, func(tx *postgres.Tx) error {
		factors, remaining, err := h.active(ctx, tx, caller.UserID)
		if err != nil {
			return err
		}
		policy, err := h.Policies.LoginPolicy(ctx, tx, caller.OrgID)
		if err != nil {
			return err
		}
		out.Factors, out.RecoveryCodesRemaining, out.MfaRequired = factors, remaining, policy.MFARequired
		// The deadline, so the account screen can warn somebody inside the
		// grace (P3-13). `P3-07` defined that warning and nothing displayed
		// it: a user learnt the mandate existed at the sign-in that refused to
		// let them through without enrolling.
		if deadline := authn.MFADeadline(policy); !deadline.IsZero() {
			out.GraceEndsAt.Set(deadline.UTC())
		} else {
			out.GraceEndsAt.SetNull()
		}
		return nil
	}); err != nil {
		return nil, internal("reading the caller's factors", err)
	}
	return api.GetMyMfa200JSONResponse(out), nil
}

// GetUserMfa reads a member's factors, for an administrator.
func (h *Handler) GetUserMfa(ctx context.Context, request api.GetUserMfaRequestObject) (api.GetUserMfaResponseObject, error) {
	orgID, _ := management.ScopeFrom(ctx)
	if orgID == "" {
		return nil, wiring("an organization MFA route ran with no tenant scope")
	}

	out := api.MemberMfa{Factors: []api.MfaFactor{}}
	if err := h.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		if _, err := h.Members.Get(ctx, tx, request.UserId.String()); err != nil {
			return err
		}
		factors, remaining, err := h.active(ctx, tx, request.UserId.String())
		out.Factors, out.RecoveryCodesRemaining = factors, remaining
		return err
	}); err != nil {
		if errors.Is(err, user.ErrNotFound) {
			return nil, notFound("no such user in this organization")
		}
		return nil, internal("reading a member's factors", err)
	}
	return api.GetUserMfa200JSONResponse(out), nil
}

// active reads a user's ACTIVE factors and their unused recovery code count.
//
// Pending enrolments are left out: they verify nothing, are replaced by the
// next enrolment, and listing them would show a user a factor that does not
// protect them.
func (h *Handler) active(ctx context.Context, tx *postgres.Tx, userID string) ([]api.MfaFactor, int, error) {
	factors, err := h.Factors.ForUser(ctx, tx, userID)
	if err != nil {
		return nil, 0, err
	}
	out := []api.MfaFactor{}
	for _, f := range factors {
		if f.Status != mfa.StatusActive {
			continue
		}
		rendered, err := render(f)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, rendered)
	}
	remaining, err := h.Recovery.Remaining(ctx, tx, userID)
	return out, remaining, err
}

// --- enrolling an authenticator app -----------------------------------------------------

// BeginMyTotpEnrolment starts an enrolment and returns the secret, once.
func (h *Handler) BeginMyTotpEnrolment(
	ctx context.Context, request api.BeginMyTotpEnrolmentRequestObject,
) (api.BeginMyTotpEnrolmentResponseObject, error) {
	caller, err := self(ctx)
	if err != nil {
		return nil, err
	}
	if h.Enroller == nil {
		return nil, unconfigured()
	}

	label := "Authenticator app"
	if request.Body != nil && request.Body.Label != nil {
		trimmed := strings.TrimSpace(*request.Body.Label)
		if len([]rune(trimmed)) > maxLabel {
			return nil, management.Fault{
				Class: management.Invalid, Message: "The name is too long.",
				Details: []api.ErrorDetail{{Field: "label", Issue: fmt.Sprintf("at most %d characters", maxLabel)}},
				Reason:  "label over the limit",
			}
		}
		if trimmed != "" {
			label = trimmed
		}
	}

	if err := h.requireRecentAuthentication(ctx, caller); err != nil {
		return nil, err
	}

	enrolment, err := h.Enroller.Begin(ctx, caller.UserID, caller.OrgID, label)
	switch {
	case errors.Is(err, mfa.ErrAlreadyEnrolled):
		return nil, management.Fault{
			Class:   management.Conflict,
			Message: "An authenticator app is already set up. Remove it first to set up a different one.",
			Reason:  "a second TOTP enrolment with one active",
		}
	case errors.Is(err, mfa.ErrNoSealKey):
		return nil, unconfigured()
	case err != nil:
		return nil, internal("beginning a TOTP enrolment", err)
	}

	uri, _ := enrolment.Challenge["provisioning_uri"].(string)
	grid, err := qrGrid(uri)
	if err != nil {
		return nil, internal("encoding the provisioning URI as a QR code", err)
	}
	factorID, err := uuid.Parse(enrolment.FactorID)
	if err != nil {
		return nil, internal("reading the new factor's id", err)
	}

	// Recorded after Begin succeeds and before the secret leaves: an
	// enrolment that started is worth knowing about even if it is never
	// finished. The secret is never in the payload.
	if err := h.DB.WithTenant(ctx, caller.OrgID, func(tx *postgres.Tx) error {
		return management.Audit(ctx, h.Audit, tx, audit.Event{
			OrgID: caller.OrgID, Type: audit.EventMFAEnrolmentStarted,
			Payload: map[string]any{"factor_id": enrolment.FactorID, "factor_type": string(mfa.TypeTOTP)},
		})
	}); err != nil {
		return nil, internal("auditing an enrolment start", err)
	}

	return api.BeginMyTotpEnrolment201JSONResponse{
		FactorId:        factorID,
		Secret:          enrolment.Secret,
		ProvisioningUri: uri,
		Qr:              grid,
		Digits:          mfa.TOTPDigits,
		Period:          int(mfa.TOTPPeriod.Seconds()),
	}, nil
}

// ConfirmMyTotpEnrolment proves a code and activates the factor.
//
// Not gated by recent authentication — the enrolment was begun under one — but
// bounded by the same per-user attempt counter as the sign-in challenge,
// because it is a six-digit code check and a million guesses is not many.
func (h *Handler) ConfirmMyTotpEnrolment(
	ctx context.Context, request api.ConfirmMyTotpEnrolmentRequestObject,
) (api.ConfirmMyTotpEnrolmentResponseObject, error) {
	caller, err := self(ctx)
	if err != nil {
		return nil, err
	}
	if h.Enroller == nil || h.Attempts == nil {
		return nil, unconfigured()
	}
	if request.Body == nil {
		return nil, invalidCode("no code was sent")
	}
	code := strings.TrimSpace(request.Body.Code)
	now := h.now()

	allowed, err := h.Attempts.Allowed(ctx, caller.UserID, now)
	if err != nil {
		// Fails closed, as it does at sign-in: a bound that cannot count is
		// not a bound.
		return nil, internal("checking the attempt bound", err)
	}
	if !allowed {
		return nil, management.Fault{
			Class: management.RateLimited, Message: "Too many attempts. Wait a few minutes and try again.",
			Reason: "the per-user factor attempt bound is exhausted",
		}
	}

	factorID := request.FactorId.String()

	// The factor must be the caller's AND pending, resolved in the caller's
	// tenant with the caller's user id. A pending factor of somebody else's is
	// the same 404 as none.
	var pending mfa.Factor
	if err := h.DB.WithTenant(ctx, caller.OrgID, func(tx *postgres.Tx) error {
		found, err := h.owned(ctx, tx, caller.UserID, factorID)
		pending = found
		return err
	}); err != nil {
		return nil, notFoundOr(err, "confirming a factor")
	}
	if pending.Status != mfa.StatusPending || pending.Type != mfa.TypeTOTP {
		return nil, notFound("no pending authenticator app with that id for this user")
	}

	if err := h.Enroller.Confirm(ctx, factorID, code); err != nil {
		if errors.Is(err, mfa.ErrWrongCode) {
			if _, failErr := h.Attempts.Fail(ctx, caller.UserID, now); failErr != nil && h.Log != nil {
				h.Log.Warn("recording a failed enrolment attempt failed", "error", failErr.Error())
			}
			return nil, invalidCode("the code did not match")
		}
		return nil, internal("confirming a TOTP enrolment", err)
	}

	out := api.TotpConfirmed{RecoveryCodes: nullable.NewNullNullable[[]string]()}
	if err := h.DB.WithTenant(ctx, caller.OrgID, func(tx *postgres.Tx) error {
		// Recovery codes arrive with the first factor (P3-04 step 1). Only
		// when the user holds none: issuing a new batch would silently
		// invalidate codes somebody already wrote down.
		remaining, err := h.Recovery.Remaining(ctx, tx, caller.UserID)
		if err != nil {
			return err
		}
		issued := false
		if remaining == 0 {
			codes, _, err := h.Recovery.Issue(ctx, tx, caller.UserID, caller.OrgID, mfa.RecoveryCodeCount, now)
			if err != nil {
				return err
			}
			out.RecoveryCodes = nullable.NewNullableWithValue(codes)
			issued = true
			if err := management.Audit(ctx, h.Audit, tx, audit.Event{
				OrgID: caller.OrgID, Type: audit.EventMFACodesIssued,
				Payload: map[string]any{"count": len(codes), "initiator": "enrolment"},
			}); err != nil {
				return err
			}
		}

		pending.Status = mfa.StatusActive
		rendered, err := render(pending)
		if err != nil {
			return err
		}
		out.Factor = rendered

		return management.Audit(ctx, h.Audit, tx, audit.Event{
			OrgID: caller.OrgID, Type: audit.EventMFAEnrolled,
			Payload: map[string]any{
				"factor_id": factorID, "factor_type": string(mfa.TypeTOTP),
				"recovery_codes_issued": issued,
			},
		})
	}); err != nil {
		// The factor is already active — the confirmation committed. What
		// failed is the codes or the record, and the user sees the factor with
		// no codes, which the tab then says plainly.
		return nil, internal("finishing an enrolment", err)
	}

	return api.ConfirmMyTotpEnrolment200JSONResponse(out), nil
}

// --- removing ------------------------------------------------------------------------------

// RemoveMyFactor removes one of the caller's active factors.
func (h *Handler) RemoveMyFactor(
	ctx context.Context, request api.RemoveMyFactorRequestObject,
) (api.RemoveMyFactorResponseObject, error) {
	caller, err := self(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.requireRecentAuthentication(ctx, caller); err != nil {
		return nil, err
	}

	factorID := request.FactorId.String()
	err = h.DB.WithTenant(ctx, caller.OrgID, func(tx *postgres.Tx) error {
		factors, err := h.Factors.ForUser(ctx, tx, caller.UserID)
		if err != nil {
			return err
		}

		var target *mfa.Factor
		activeCount := 0
		for i := range factors {
			if factors[i].Status == mfa.StatusActive {
				activeCount++
			}
			if factors[i].ID == factorID && factors[i].Status == mfa.StatusActive {
				target = &factors[i]
			}
		}
		if target == nil {
			return errNoFactor
		}

		if activeCount == 1 {
			policy, err := h.Policies.LoginPolicy(ctx, tx, caller.OrgID)
			if err != nil {
				return err
			}
			if policy.MFARequired {
				// The next sign-in would have nothing to challenge with and
				// would route the user straight back into enrolment (P3-07).
				// Refused here, with the reason, rather than allowed and
				// discovered at the next login.
				return management.Fault{
					Class: management.Conflict,
					Message: "Your organization requires a second factor, so you cannot remove your last one. " +
						"Add another first, then remove this one.",
					Reason: "removing the last factor under the mandate",
				}
			}
		}

		if err := h.Factors.Delete(ctx, tx, factorID); err != nil {
			return err
		}
		return management.Audit(ctx, h.Audit, tx, audit.Event{
			OrgID: caller.OrgID, Type: audit.EventMFARemoved,
			Payload: map[string]any{
				"factor_id": factorID, "factor_type": string(target.Type), "remaining": activeCount - 1,
			},
		})
	})
	if err != nil {
		return nil, notFoundOr(err, "removing a factor")
	}
	return api.RemoveMyFactor204Response{}, nil
}

// --- recovery codes --------------------------------------------------------------------------

// RegenerateMyRecoveryCodes replaces the caller's recovery codes.
func (h *Handler) RegenerateMyRecoveryCodes(
	ctx context.Context, _ api.RegenerateMyRecoveryCodesRequestObject,
) (api.RegenerateMyRecoveryCodesResponseObject, error) {
	caller, err := self(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.requireRecentAuthentication(ctx, caller); err != nil {
		return nil, err
	}

	var codes []string
	err = h.DB.WithTenant(ctx, caller.OrgID, func(tx *postgres.Tx) error {
		factors, _, err := h.active(ctx, tx, caller.UserID)
		if err != nil {
			return err
		}
		if len(factors) == 0 {
			return management.Fault{
				Class:   management.Conflict,
				Message: "Set up a second factor first. Recovery codes get you past a second factor, so without one they recover nothing.",
				Reason:  "regenerating recovery codes with no active factor",
			}
		}

		// Issue deletes every earlier code and inserts the new batch in this
		// transaction, so there is no moment with two working sets or none.
		if codes, _, err = h.Recovery.Issue(ctx, tx, caller.UserID, caller.OrgID, mfa.RecoveryCodeCount, h.now()); err != nil {
			return err
		}
		return management.Audit(ctx, h.Audit, tx, audit.Event{
			OrgID: caller.OrgID, Type: audit.EventMFACodesIssued,
			Payload: map[string]any{"count": len(codes), "initiator": "self-service"},
		})
	})
	if err != nil {
		return nil, internal("regenerating recovery codes", err)
	}
	return api.RegenerateMyRecoveryCodes200JSONResponse{Codes: codes}, nil
}

// --- recent authentication ------------------------------------------------------------------

// requireRecentAuthentication refuses unless the caller's session signed in
// within RecentAuthentication.
//
// The session's `created_at` IS the authentication time: a session is created
// by a sign-in and never extended by one — re-authenticating creates a new
// session and revokes the old (P1-12). Read in the caller's own tenant, with
// the caller's user id, so a token cannot borrow another session's freshness.
func (h *Handler) requireRecentAuthentication(ctx context.Context, caller management.Caller) error {
	refuse := management.Fault{
		Class:   management.Reauthenticate,
		Message: "For your security, sign in again to change how your account is protected.",
		Reason:  "the session is older than the recent-authentication window",
	}
	if caller.SessionID == "" {
		refuse.Reason = "a token with no session cannot manage factors"
		return refuse
	}

	var authenticatedAt time.Time
	err := h.DB.WithTenant(ctx, caller.OrgID, func(tx *postgres.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT created_at FROM sessions WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`,
			caller.SessionID, caller.UserID).Scan(&authenticatedAt)
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		refuse.Reason = "the token's session is not the caller's or has ended"
		return refuse
	case err != nil:
		return internal("reading the session's authentication time", err)
	}

	if h.now().Sub(authenticatedAt) > RecentAuthentication {
		return refuse
	}
	return nil
}

// --- helpers -----------------------------------------------------------------------------------

var errNoFactor = errors.New("mfaapi: no such factor for this user")

// owned finds one of a user's factors by id, in any status.
func (h *Handler) owned(ctx context.Context, tx *postgres.Tx, userID, factorID string) (mfa.Factor, error) {
	factors, err := h.Factors.ForUser(ctx, tx, userID)
	if err != nil {
		return mfa.Factor{}, err
	}
	for _, f := range factors {
		if f.ID == factorID {
			return f, nil
		}
	}
	return mfa.Factor{}, errNoFactor
}

func render(f mfa.Factor) (api.MfaFactor, error) {
	id, err := uuid.Parse(f.ID)
	if err != nil {
		return api.MfaFactor{}, fmt.Errorf("mfaapi: a factor id is not a uuid: %w", err)
	}
	out := api.MfaFactor{
		Id:         id,
		Type:       api.MfaFactorType(f.Type),
		Label:      nullable.NewNullNullable[string](),
		CreatedAt:  f.CreatedAt,
		LastUsedAt: nullable.NewNullNullable[time.Time](),
	}
	if f.Label != "" {
		out.Label = nullable.NewNullableWithValue(f.Label)
	}
	if f.LastUsedAt != nil {
		out.LastUsedAt = nullable.NewNullableWithValue(*f.LastUsedAt)
	}
	return out, nil
}

// qrGrid encodes a URI as a module grid.
//
// A grid rather than an image or SVG markup: the client draws it with its own
// elements, so nothing that could be interpreted as markup crosses the API, and
// the console needs no QR library. Medium error correction, which is what
// authenticator apps are tested against.
func qrGrid(uri string) (api.QrCode, error) {
	if uri == "" {
		return api.QrCode{}, errors.New("mfaapi: no provisioning URI to encode")
	}
	code, err := qr.Encode(uri, qr.M)
	if err != nil {
		return api.QrCode{}, err
	}
	rows := make([]string, code.Size)
	var b strings.Builder
	for y := 0; y < code.Size; y++ {
		b.Reset()
		for x := 0; x < code.Size; x++ {
			if code.Black(x, y) {
				b.WriteByte('1')
			} else {
				b.WriteByte('0')
			}
		}
		rows[y] = b.String()
	}
	return api.QrCode{Size: code.Size, Rows: rows}, nil
}

func self(ctx context.Context) (management.Caller, error) {
	caller, ok := management.CallerFrom(ctx)
	if !ok || caller.UserID == "" || caller.OrgID == "" {
		return management.Caller{}, wiring("a self-scoped MFA route ran with no caller")
	}
	return caller, nil
}

func unconfigured() error {
	return management.Fault{
		Class:   management.Conflict,
		Message: "Second factors are not available on this service.",
		Reason:  "MFA is not configured on this deployment",
	}
}

func invalidCode(reason string) error {
	return management.Fault{
		Class:   management.Invalid,
		Message: "That code didn't match. Codes change every 30 seconds — try the one showing now.",
		Details: []api.ErrorDetail{{Field: "code", Issue: "does not match"}},
		Reason:  reason,
	}
}

func notFound(reason string) error {
	return management.Fault{
		Class: management.NotFound, Message: "The requested resource was not found.", Reason: reason,
	}
}

func notFoundOr(err error, doing string) error {
	if errors.Is(err, errNoFactor) {
		return notFound("no such factor for this user")
	}
	return internal(doing, err)
}

func wiring(reason string) error {
	return management.Fault{
		Class: management.Internal, Message: "An unexpected error occurred.", Reason: reason,
	}
}

func internal(doing string, err error) error {
	var fault management.Fault
	if errors.As(err, &fault) {
		return err
	}
	return fmt.Errorf("mfaapi: %s: %w", doing, err)
}
