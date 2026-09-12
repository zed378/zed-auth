//go:build integration

package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Nothing a caller sends can change which organization a request belongs to
// (P2-09 step 4, ADR-023).
//
// The tenant is resolved from the OIDC client, which means there is nothing to
// forge — and the way that property gets lost is somebody adding a convenience:
// an `X-Org-Id` header for a support tool, an `org` query parameter for a
// debugging session, a `Host`-based override for a demo. Each looks harmless in
// isolation and each turns the tenant into something the request asserts.
//
// So this reads the source for any sign that a tenant is being taken from the
// request, rather than trying to send every header somebody might one day
// honour. A behavioural test can only probe the names we thought of.
func TestNoRequestInputCanNameTheTenant(t *testing.T) {
	// Patterns that mean "an organization is being read out of the request".
	//
	// `chi.URLParam(r, "org_id")` is deliberately NOT here: the Management API
	// takes the organization in the path BY DESIGN and checks it against the
	// caller's manager roles (`P1-15`). The path is part of the contract; a
	// header is not.
	forbidden := []struct {
		needle string
		why    string
	}{
		{`Header.Get("X-Org`, "a header naming the tenant is a tenant the caller chooses"},
		{`Header.Get("X-Organization`, "likewise"},
		{`Header.Get("X-Tenant`, "likewise"},
		{`FormValue("org_id")`, "a form field naming the tenant is a tenant the caller chooses"},
		{`Query().Get("org_id")`, "a query parameter naming the tenant is a tenant the caller chooses"},
		{`Query().Get("organization")`, "likewise"},
		{`Query().Get("tenant")`, "likewise"},
	}

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolving the source root: %v", err)
	}

	var findings []string
	scanned := 0

	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "node_modules" || info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		scanned++

		rel, _ := filepath.Rel(root, path)
		for _, line := range strings.Split(string(source), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			for _, f := range forbidden {
				if strings.Contains(line, f.needle) {
					findings = append(findings,
						filepath.ToSlash(rel)+": "+trimmed+"\n      "+f.why)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the source: %v", err)
	}

	if scanned < 50 {
		t.Fatalf("only %d source files were scanned, too few to have searched anything", scanned)
	}

	for _, f := range findings {
		t.Errorf("the tenant is being read from the request:\n    %s", f)
	}
}

// The trusted-header exception, stated so it stays an exception.
//
// `AUTH_TRUST_PROXY_HEADERS` lets the deployment assert that `X-Forwarded-For`
// and `X-Request-Id` come from a proxy it controls (`P1-13`). That is a claim
// about topology the service cannot verify, which is why it is opt-in and
// logged — and it is about the CLIENT'S ADDRESS, never about which tenant the
// request belongs to.
//
// This asserts the distinction holds: whatever the proxy is trusted for, it is
// not trusted to name an organization.
func TestTrustingProxyHeadersDoesNotExtendToTheTenant(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolving the source root: %v", err)
	}

	source, err := os.ReadFile(filepath.Join(root, "internal/httpserver/middleware.go"))
	if err != nil {
		// The file moved; find whatever reads the forwarded header instead.
		t.Skip("middleware.go not found at the expected path")
	}

	text := string(source)
	if !strings.Contains(text, "X-Forwarded-For") {
		t.Skip("the forwarded-header handling is elsewhere")
	}
	for _, tenantish := range []string{"org_id", "orgID", "OrgID", "tenant"} {
		if strings.Contains(text, tenantish) {
			t.Errorf("the proxy-header middleware mentions %q; trusting a proxy for an address must not extend to trusting it for a tenant", tenantish)
		}
	}
}
