package login

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Per-organization branding, read from organizations.settings.
//
// PG-16 is the gap this closes: PLAN/01 puts branding in scope, UI-UX/05
// bounds it to exactly two things, UI-UX/08 gives it a settings screen, and
// console/src/branding/branding.ts already implements applying it — with no
// column, key or documented shape anywhere for it to be read from. The
// resolution was to specify `settings.branding` rather than add a table:
//
//	"branding": { "logo_url": "https://…", "accent_color": "#1d4ed8" }
//
// Nothing writes it yet. P2-14 makes it editable, so today every organization
// renders with the defaults below — which is stated here rather than left to
// be discovered when a logo does not appear.
//
// ParseBranding is exported and pure so P2-14 can validate on write with the
// same rules that apply on read. A value accepted by the settings screen and
// silently discarded by the login page would be a control that appears to work.

// DefaultAccent is the instance accent, from console/src/styles/tokens.css.
//
// Duplicated rather than imported: this page must render when the console is
// broken, undeployed, or mid-rollout (FR-1), so it cannot take a build-time
// dependency on the console's stylesheet. The duplication is deliberate and is
// the same trade check-brand-tokens.mjs already governs for the public site.
const DefaultAccent = "#1d4ed8"

// minAccentContrast is the ratio an accent must reach against white.
//
// 4.5:1 — WCAG 2.1 AA for normal text (1.4.3), because the accent is a button
// background with white label text on it. An organization that sets a pale
// brand colour would otherwise produce a primary action nobody can read, on
// the one page where failing to find the button means failing to sign in.
const minAccentContrast = 4.5

// Branding is what an organization may change about this page.
//
// Two fields, and that is the whole of it. UI-UX/05 is explicit that
// `color-danger` and `color-warning` are never overridable, so that red keeps
// meaning "irreversible" in every tenant — a rule this struct enforces by not
// having anywhere to put a third colour.
type Branding struct {
	AccentColor string
	LogoURL     string
}

// DefaultBranding is an unbranded page.
var DefaultBranding = Branding{AccentColor: DefaultAccent}

// Rejection records a branding value that was refused, and why.
//
// Returned rather than logged from inside the parser, so the caller decides
// where it goes: a WARN on the login path, and a validation error on P2-14's
// write path. Same rules, two audiences.
type Rejection struct {
	Field  string
	Value  string
	Reason string
}

// ParseBranding reads the branding key out of an organization's settings.
//
// Never fails. A settings document that cannot be read, or a value that cannot
// be used, yields the default with the reason reported — because the login
// page must render. A malformed logo URL is a cosmetic problem, and refusing
// to serve the page over one would turn it into an outage.
func ParseBranding(settings []byte) (Branding, []Rejection) {
	out := DefaultBranding

	if len(settings) == 0 {
		return out, nil
	}

	var doc struct {
		Branding *struct {
			AccentColor *string `json:"accent_color"`
			LogoURL     *string `json:"logo_url"`
		} `json:"branding"`
	}
	if err := json.Unmarshal(settings, &doc); err != nil {
		return out, []Rejection{{
			Field:  "settings",
			Reason: "the settings document is not valid JSON",
		}}
	}
	if doc.Branding == nil {
		return out, nil
	}

	var rejected []Rejection

	if doc.Branding.AccentColor != nil {
		value := strings.TrimSpace(*doc.Branding.AccentColor)
		accent, reason := ValidateAccent(value)
		if reason == "" {
			out.AccentColor = accent
		} else {
			rejected = append(rejected, Rejection{Field: "accent_color", Value: value, Reason: reason})
		}
	}

	if doc.Branding.LogoURL != nil {
		value := strings.TrimSpace(*doc.Branding.LogoURL)
		if reason := ValidateLogoURL(value); reason == "" {
			out.LogoURL = value
		} else {
			rejected = append(rejected, Rejection{Field: "logo_url", Value: value, Reason: reason})
		}
	}

	return out, rejected
}

// ValidateAccent checks a colour and returns it normalised, or a reason.
//
// Only #rgb and #rrggbb are accepted. Named colours, rgb(), hsl() and the rest
// are all valid CSS and are refused anyway, because the value is interpolated
// into a stylesheet and the set of strings that are safe there is far easier
// to state than the set that is valid.
func ValidateAccent(value string) (string, string) {
	if value == "" {
		return "", "empty"
	}

	hex := strings.ToLower(value)
	if !strings.HasPrefix(hex, "#") {
		return "", "must be a hex colour beginning with #"
	}
	digits := hex[1:]

	switch len(digits) {
	case 3:
		// Expanded, so the contrast check and the emitted CSS see one form.
		digits = string([]byte{
			digits[0], digits[0],
			digits[1], digits[1],
			digits[2], digits[2],
		})
	case 6:
	default:
		return "", "must be #rgb or #rrggbb"
	}

	for _, c := range digits {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return "", "must be #rgb or #rrggbb"
		}
	}

	if ratio := contrastWithWhite(digits); ratio < minAccentContrast {
		return "", fmt.Sprintf(
			"contrast against white text is %.1f:1, below the %.1f:1 WCAG 2.1 AA requires",
			ratio, minAccentContrast)
	}

	return "#" + digits, ""
}

// ValidateLogoURL returns why a logo URL cannot be used, or "".
//
// Abuse case A-8. The URL becomes an src attribute, so the scheme is the
// control that matters: a data: URL would let an organization's settings carry
// arbitrary bytes into the page, and anything but https would make the login
// page mixed content — on the one page where a browser's padlock is the thing
// a careful user checks before typing a password.
func ValidateLogoURL(value string) string {
	if value == "" {
		return "empty"
	}
	if len(value) > 2048 {
		return "too long"
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "is not a URL"
	}
	if parsed.Scheme != "https" {
		return "must be an https:// URL"
	}
	if parsed.Host == "" {
		return "has no host"
	}
	return ""
}

// contrastWithWhite is WCAG 2.1's contrast ratio against #ffffff.
func contrastWithWhite(digits string) float64 {
	channel := func(offset int) float64 {
		v, _ := strconv.ParseUint(digits[offset:offset+2], 16, 8)
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}

	luminance := 0.2126*channel(0) + 0.7152*channel(2) + 0.0722*channel(4)
	return 1.05 / (luminance + 0.05)
}

// ErrOrganizationNotFound means no such organization is visible in scope.
var ErrOrganizationNotFound = errors.New("login: organization not found")

// BrandingStore reads branding for one organization.
type BrandingStore struct{}

func NewBrandingStore() *BrandingStore { return &BrandingStore{} }

// Branding reads and parses one organization's branding.
//
// Takes the caller's transaction, like authn.PolicyStore.Policy, so the read
// joins the transaction of the login it decorates rather than opening a second
// one for a logo.
func (s *BrandingStore) Branding(
	ctx context.Context, tx *postgres.Tx, orgID string,
) (Branding, []Rejection, error) {
	var settings []byte

	err := tx.QueryRow(ctx, `SELECT settings FROM organizations WHERE id = $1`, orgID).Scan(&settings)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return DefaultBranding, nil, fmt.Errorf("%w: %s", ErrOrganizationNotFound, orgID)
	case err != nil:
		return DefaultBranding, nil, fmt.Errorf("login: reading branding: %w", err)
	}

	branding, rejected := ParseBranding(settings)
	return branding, rejected, nil
}
