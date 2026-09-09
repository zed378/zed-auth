package login

import (
	"os"
	"testing"
)

// Writes the rendered page to disk when AUTH_DUMP_PAGE is set, so a browser
// can be pointed at the exact bytes the handler serves.
//
// Guarded by the variable rather than always writing: a test that leaves files
// behind is a test that fails on a read-only checkout.
func TestDumpPage(t *testing.T) {
	path := os.Getenv("AUTH_DUMP_PAGE")
	if path == "" {
		t.Skip("AUTH_DUMP_PAGE is not set")
	}

	page := samplePage()
	page.Error = MsgCredentials
	page.Style, page.StyleHash = renderStyle(page.Branding.AccentColor)

	body, err := render(pageTemplate, page)
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}
}
