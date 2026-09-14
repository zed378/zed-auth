package organization

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/authn"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// What enabling the MFA mandate would cost (P3-07 step 5).
//
// The one question an administrator needs answered before turning it on.
// Enabling it blind is how an organization discovers, one support ticket at a
// time, that most of its users had never enrolled — and the discovery arrives
// as "nobody can sign in", which is when somebody turns the setting back off
// and leaves it off.
//
// **Counts, never names.** A list of colleagues with no second factor is not
// needed to make this decision, and it is exactly what an attacker holding an
// administrator's token would want: a ready-made set of the accounts in this
// organization for which a stolen password is enough.

// Impact is the answer.
type Impact struct {
	Members       int
	WithoutFactor int
	MFARequired   bool

	// GraceEndsAt is when the grace period closes, zero when the mandate is
	// off. A deadline for a policy nobody enabled would be a number this
	// service made up.
	GraceEndsAt time.Time
}

// MFAImpact counts the members a mandate would affect.
func (s *Store) MFAImpact(ctx context.Context, tx *postgres.Tx, orgID string) (Impact, error) {
	var impact Impact

	// One query rather than two, so the two counts cannot be taken from
	// different moments — a member who enrols between them would otherwise make
	// the numbers disagree with each other.
	//
	// `status = 'active'` on both sides: a deactivated user cannot sign in, so
	// a policy about signing in cannot affect them. Counting them would inflate
	// the number an administrator is deciding on.
	err := tx.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (
		           WHERE NOT EXISTS (
		               SELECT 1 FROM user_mfa_factors f
		                WHERE f.user_id = u.id
		                  AND f.status = 'active'
		           )
		       )
		  FROM users u
		 WHERE u.org_id = $1
		   AND u.status = 'active'`, orgID,
	).Scan(&impact.Members, &impact.WithoutFactor)
	if err != nil {
		return Impact{}, fmt.Errorf("organization: counting the MFA impact: %w", err)
	}

	var settings json.RawMessage
	if err := tx.QueryRow(ctx,
		`SELECT settings FROM organizations WHERE id = $1`, orgID).Scan(&settings); err != nil {
		return Impact{}, fmt.Errorf("organization: reading settings for the MFA impact: %w", err)
	}

	// Through the same parser the login path uses, rather than a second read of
	// the same JSON. Two parsers of one document is two answers waiting to
	// disagree, and the one that mattered would be whichever the login used.
	policy, _ := authn.ParseLoginPolicy(settings)
	impact.MFARequired = policy.MFARequired
	impact.GraceEndsAt = authn.MFADeadline(policy)

	return impact, nil
}

// GetMfaImpact serves the endpoint.
func (h *Handler) GetMfaImpact(
	ctx context.Context, request api.GetMfaImpactRequestObject,
) (api.GetMfaImpactResponseObject, error) {
	var impact Impact

	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		var err error
		impact, err = h.Store.MFAImpact(ctx, tx, request.OrgId.String())
		return err
	}); err != nil {
		return nil, err
	}

	body := api.MfaImpact{
		Members:       impact.Members,
		WithoutFactor: impact.WithoutFactor,
		MfaRequired:   impact.MFARequired,
		// Stated by the service, so the console's "they will have N days"
		// is this constant rather than a copy of it (P3-13).
		GracePeriodDays: int(authn.MFAGracePeriod / (24 * time.Hour)),
	}
	if !impact.GraceEndsAt.IsZero() {
		body.GraceEndsAt.Set(impact.GraceEndsAt)
	}

	return api.GetMfaImpact200JSONResponse(body), nil
}
