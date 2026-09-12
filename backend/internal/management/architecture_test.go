package management

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every permission decision goes through Authorize, and nothing else decides
// one (P2-05 step 5, and its definition of done: "verified by review and by an
// architecture test").
//
// The failure this prevents is not dramatic and is very hard to see in review:
// a handler that needs one extra condition writes `for _, g := range
// caller.Grants { if g.Role == OrgAdmin ... }` inline, which looks reasonable,
// works, and quietly skips the scope match, the INSTANCE_OWNER path, the
// invisible-versus-forbidden distinction, and every future change to the
// hierarchy. One endpoint ends up subtly more permissive than the rest, and
// the difference is a single `==` in a file nobody rereads.
//
// Reading the source is the only way to check a NEGATIVE like this. A
// behavioural test can show that the endpoints we thought of are correct; it
// cannot show that no other file decides permissions.
func TestNothingOutsideThisPackageDecidesPermissions(t *testing.T) {
	// Patterns that mean "I am making an authorization decision myself".
	forbidden := []struct {
		needle string
		why    string
	}{
		{".Satisfies(", "role comparison belongs to the hierarchy table, not to a handler"},
		{"caller.Grants", "iterating a caller's grants is Authorize's job"},
		{"Caller.Grants", "iterating a caller's grants is Authorize's job"},
		{".Role == ", "comparing a role by name skips the scope match entirely"},
	}

	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolving the source root: %v", err)
	}

	var findings []string
	walked := 0

	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		// This package is where the decision lives, and a test may say
		// anything it likes about it.
		if strings.Contains(filepath.ToSlash(path), "/management/") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		walked++

		for _, line := range strings.Split(string(source), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			for _, f := range forbidden {
				if strings.Contains(line, f.needle) {
					rel, _ := filepath.Rel(root, path)
					findings = append(findings, rel+": "+strings.TrimSpace(line)+"\n      "+f.why)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the source: %v", err)
	}

	// The guard against the guard: if the walk found nothing to read, the
	// assertion below is satisfied by a search that never happened. This
	// project has shipped that shape of check twice.
	if walked < 20 {
		t.Fatalf("only %d source files were scanned, which is too few to have searched anything", walked)
	}

	for _, f := range findings {
		t.Errorf("a permission decision outside internal/management:\n    %s", f)
	}
}
