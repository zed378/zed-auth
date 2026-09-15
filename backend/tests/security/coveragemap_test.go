//go:build integration

package security

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The coverage map in isolation_test.go names the tests that cover each abuse
// case. A renamed or deleted test leaves the map claiming coverage that no
// longer exists, which is the failure this map was written to prevent — so
// every name in it must still be a test function somewhere (P3-14).
func TestTheCoverageMapNamesTestsThatExist(t *testing.T) {
	source, err := os.ReadFile("isolation_test.go")
	if err != nil {
		t.Fatalf("reading the map: %v", err)
	}
	header, _, found := strings.Cut(string(source), "\npackage security")
	if !found {
		t.Fatal("isolation_test.go has no package clause after its map")
	}
	named := regexp.MustCompile(`\bTest[A-Z][A-Za-z0-9_]*`).FindAllString(header, -1)
	if len(named) < 20 {
		t.Fatalf("found only %d test names in the map — the format changed and this check reads nothing", len(named))
	}

	defined := map[string]bool{}
	declaration := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
	for _, root := range []string{filepath.Join("..", ".."), filepath.Join("..", "..", "..", "demo")} {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() && (info.Name() == "node_modules" || info.Name() == ".git") {
				return filepath.SkipDir
			}
			if info.IsDir() || !strings.HasSuffix(path, "_test.go") {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			for _, m := range declaration.FindAllStringSubmatch(string(body), -1) {
				defined[m[1]] = true
			}
			return nil
		})
	}

	for _, name := range named {
		if !defined[name] {
			t.Errorf("the coverage map names %s, which is not a test anywhere — the coverage it claims is gone", name)
		}
	}
}
