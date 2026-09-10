package login

import (
	"strings"
	"testing"
)

// PG-16: `settings.branding` is the documented shape. A settings document
// without the key must produce the instance defaults rather than an error,
// because that is every organization today — nothing writes branding until
// P2-14.
func TestBrandingDefaultsWhenAbsent(t *testing.T) {
	settings := []byte(`{"password_policy":{"min_length":12},"mfa_required":false}`)

	branding, rejected := ParseBranding(settings)

	if len(rejected) != 0 {
		t.Errorf("an organization with no branding key produced rejections: %v", rejected)
	}
	if branding.AccentColor != DefaultAccent {
		t.Errorf("accent = %q, want the instance default", branding.AccentColor)
	}
	if branding.LogoURL != "" {
		t.Errorf("a logo appeared from nowhere: %q", branding.LogoURL)
	}
}

func TestBrandingIsRead(t *testing.T) {
	settings := []byte(`{"branding":{"accent_color":"#046C4E","logo_url":"https://cdn.example/logo.svg"}}`)

	branding, rejected := ParseBranding(settings)

	if len(rejected) != 0 {
		t.Fatalf("valid branding was rejected: %v", rejected)
	}
	if branding.AccentColor != "#046c4e" {
		t.Errorf("accent = %q, want the normalised value", branding.AccentColor)
	}
	if branding.LogoURL != "https://cdn.example/logo.svg" {
		t.Errorf("logo = %q", branding.LogoURL)
	}
}

// A settings document that cannot be read must not stop the login page
// rendering. Refusing to serve it over a malformed logo URL would turn a
// cosmetic problem into an outage on the one page nobody can work around.
func TestUnreadableSettingsStillRender(t *testing.T) {
	for name, settings := range map[string][]byte{
		"not JSON":      []byte(`{{{`),
		"empty":         nil,
		"branding null": []byte(`{"branding":null}`),
	} {
		t.Run(name, func(t *testing.T) {
			branding, _ := ParseBranding(settings)
			if branding.AccentColor != DefaultAccent {
				t.Errorf("accent = %q, want the default", branding.AccentColor)
			}
		})
	}
}

// docs/UI-UX/05 § Color: an organization overrides the accent and the logo, and
// nothing else. The struct has nowhere to put a third value, so the test that
// this holds is that a settings document naming other tokens changes nothing.
func TestOnlyTwoValuesAreBrandable(t *testing.T) {
	settings := []byte(`{"branding":{
		"accent_color":"#046c4e",
		"danger_color":"#00ff00",
		"color-danger":"#00ff00",
		"background":"#000000"
	}}`)

	branding, _ := ParseBranding(settings)

	if branding.AccentColor != "#046c4e" {
		t.Errorf("accent = %q", branding.AccentColor)
	}
	// The rendered stylesheet is where it would show if any of the others had
	// been honoured.
	css, _ := renderStyle(branding.AccentColor)
	if strings.Contains(string(css), "#00ff00") || strings.Contains(string(css), "#000000") {
		t.Error("a token an organization may not override reached the stylesheet")
	}
	if !strings.Contains(string(css), "--color-danger:#b42318") {
		t.Error("color-danger is not the reserved value; docs/UI-UX/05 requires it to be " +
			"the same in every tenant so red keeps meaning irreversible")
	}
}

// --- the accent -------------------------------------------------------------------

func TestAccentContrastIsEnforced(t *testing.T) {
	// The accent is a button background with white text on it. A pale brand
	// colour produces a primary action nobody can read, on the page where not
	// finding the button means not signing in.
	if _, reason := ValidateAccent("#ffe600"); reason == "" {
		t.Error("a yellow accent was accepted; white text on it is unreadable")
	}
	if accent, reason := ValidateAccent("#1d4ed8"); reason != "" || accent != "#1d4ed8" {
		t.Errorf("the instance accent was refused by its own rule: %q", reason)
	}
}

// Not a decorative check. The value is interpolated into a stylesheet, so
// admitting anything but a hex triple is admitting a string that can close the
// declaration and open another.
func TestAccentRefusesAnythingButHex(t *testing.T) {
	for _, value := range []string{
		"red",
		"rgb(29,78,216)",
		"var(--color-danger)",
		"#1d4ed8;}body{display:none",
		"#1d4ed",
		"#gggggg",
		"",
		"1d4ed8",
	} {
		if accent, reason := ValidateAccent(value); reason == "" {
			t.Errorf("ValidateAccent(%q) accepted it as %q", value, accent)
		}
	}
}

func TestShorthandAccentIsExpanded(t *testing.T) {
	accent, reason := ValidateAccent("#00A")
	if reason != "" {
		t.Fatalf("#00A was refused: %s", reason)
	}
	if accent != "#0000aa" {
		t.Errorf("accent = %q, want the expanded form", accent)
	}
}

// A refused accent must fall back rather than reach the stylesheet, and
// renderStyle re-checks rather than trusting its caller — because it is the
// last point before the value becomes CSS.
func TestRenderStyleRefusesAnUnvalidatedAccent(t *testing.T) {
	css, _ := renderStyle("#1d4ed8;}body{display:none")

	if strings.Contains(string(css), "display:none") {
		t.Fatal("an unvalidated accent was interpolated into the stylesheet")
	}
	if !strings.Contains(string(css), "--color-accent:"+DefaultAccent) {
		t.Error("the fallback accent was not applied")
	}
}

// --- the logo ---------------------------------------------------------------------

// Abuse case A-8. The value becomes an src attribute.
func TestLogoURLRefusesDangerousSchemes(t *testing.T) {
	for _, value := range []string{
		"javascript:alert(1)",
		"data:image/svg+xml;base64,PHN2Zz48L3N2Zz4=",
		"http://cdn.example/logo.svg",
		"//cdn.example/logo.svg",
		"https://",
		"",
	} {
		if reason := ValidateLogoURL(value); reason == "" {
			t.Errorf("ValidateLogoURL(%q) accepted it", value)
		}
	}
}

func TestLogoURLAcceptsHTTPS(t *testing.T) {
	if reason := ValidateLogoURL("https://cdn.example/logo.svg"); reason != "" {
		t.Errorf("a plain https logo was refused: %s", reason)
	}
}

// A refused value is reported, not silently dropped. An administrator who set
// a logo that never appears has a setting they believe is in force and is not.
func TestRefusedBrandingIsReported(t *testing.T) {
	settings := []byte(`{"branding":{"accent_color":"#ffe600","logo_url":"javascript:alert(1)"}}`)

	branding, rejected := ParseBranding(settings)

	if len(rejected) != 2 {
		t.Fatalf("got %d rejections, want one per refused field: %v", len(rejected), rejected)
	}
	if branding.AccentColor != DefaultAccent || branding.LogoURL != "" {
		t.Error("a refused value was applied anyway")
	}

	fields := map[string]bool{}
	for _, r := range rejected {
		fields[r.Field] = true
		if r.Reason == "" {
			t.Errorf("the rejection of %q carries no reason", r.Field)
		}
	}
	if !fields["accent_color"] || !fields["logo_url"] {
		t.Errorf("rejections do not name both fields: %v", rejected)
	}
}
