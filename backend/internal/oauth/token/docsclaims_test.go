package token

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The published docs describe the claims this file emits (P2-15).
//
// `P2-15`'s Definition of Done asks that "the claim format in the docs matches
// the emitted tokens byte for byte". That is a claim about two files agreeing,
// and a claim about two files agreeing is worth a test rather than a careful
// read — a consumer team writes `claims["urn:authservice:..."]` against what
// the documentation says, and a documentation typo is an integration that
// silently finds no roles and therefore denies everything.
//
// This walks the public site's own pages and asserts the exact strings this
// package produces appear in them.

// docsRoot locates the published documentation from the package directory.
func docsRoot(t *testing.T) string {
	t.Helper()

	root := filepath.Join("..", "..", "..", "..", "public-site", "docs")
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("the published docs are not where this test expects them (%s): %v", root, err)
	}
	return root
}

// readDocs returns every markdown page under the docs root, by path.
func readDocs(t *testing.T) map[string]string {
	t.Helper()

	pages := map[string]string{}
	root := docsRoot(t)

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		pages[path] = string(body)
		return nil
	})
	if err != nil {
		t.Fatalf("reading the docs: %v", err)
	}

	// Without this, a docs folder that moved or emptied would make every
	// assertion below pass against nothing — the failure mode this project
	// keeps finding.
	if len(pages) < 5 {
		t.Fatalf("only %d documentation page(s) found; this test would prove nothing", len(pages))
	}
	return pages
}

// The project role claim key appears in the docs exactly as it is emitted.
func TestDocsQuoteTheProjectRoleClaimExactly(t *testing.T) {
	pages := readDocs(t)

	// The real key, with the project id left as the placeholder the docs use.
	// Built from the function under test rather than typed out, so a change to
	// the namespace fails here instead of silently diverging.
	emitted := RoleClaimNamespace("PROJECT_ID")

	if !strings.Contains(emitted, "urn:authservice:iam:org:project:") {
		t.Fatalf("the claim namespace changed shape: %q", emitted)
	}

	var found []string
	for path, body := range pages {
		if strings.Contains(body, emitted) {
			found = append(found, path)
		}
	}

	if len(found) == 0 {
		t.Errorf("no published page quotes the role claim key %q.\n"+
			"A consumer reads the docs to write this string; a page that spells it "+
			"differently produces an integration that finds no roles and denies everything.",
			emitted)
	}
}

// The manager role claim appears exactly as it is emitted.
func TestDocsQuoteTheManagerRoleClaimExactly(t *testing.T) {
	pages := readDocs(t)

	var found []string
	for path, body := range pages {
		if strings.Contains(body, ManagerRoleClaim) {
			found = append(found, path)
		}
	}

	if len(found) == 0 {
		t.Errorf("no published page quotes the manager role claim key %q", ManagerRoleClaim)
	}
}

// The docs state the role-claim bound, and state the number this code enforces.
//
// `MaxRoleClaims` is the kind of limit a consumer plans around, and a page
// naming a different number is worse than one naming none.
func TestDocsStateTheRoleClaimBound(t *testing.T) {
	pages := readDocs(t)

	bound := itoa(MaxRoleClaims)
	var mentions int
	for _, body := range pages {
		// "at most 64 role" / "at most 64 roles" — the phrasing both pages use.
		if strings.Contains(body, "at most "+bound+" role") {
			mentions++
		}
	}

	if mentions == 0 {
		t.Errorf("no published page states the %d-role claim bound this package enforces", MaxRoleClaims)
	}
}

// No published page describes Project Grants or ABAC as available.
//
// `P2-15` step 7 and `docs/UI-UX/21`'s governance rule: documentation never
// describes a capability beyond the current roadmap phase. Both are named on
// the concepts page deliberately — with the phase they arrive in — so this
// checks for the sentence shapes that would claim they are here now.
func TestDocsDoNotClaimUnshippedAuthorizationFeatures(t *testing.T) {
	pages := readDocs(t)

	// Phrases that would only appear if a page presented one as usable. Each
	// is matched case-insensitively against the page's own text.
	forbidden := []string{
		"create a project grant",
		"delegate a project to",
		"define an attribute-based policy",
		"write a policy in rego",
	}

	for path, body := range pages {
		lowered := strings.ToLower(body)
		for _, phrase := range forbidden {
			if !strings.Contains(lowered, phrase) {
				continue
			}
			// A planned-work table naming the phase is fine; an instruction is
			// not. The distinction is whether the page also says which phase.
			if strings.Contains(lowered, "phase 4") {
				continue
			}
			t.Errorf("%s says %q without naming the phase it arrives in", path, phrase)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
