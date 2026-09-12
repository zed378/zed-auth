//go:build integration

package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every query path carries tenant context, and the exceptions are named.
//
// `P2-08` step 3 asks for an architecture test that "fails if a repository
// method can execute without tenant context". It cannot forbid the bypass
// outright, because a handful of reads genuinely happen BEFORE a tenant is
// known — resolving a client id, a refresh token, a session cookie. Each of
// those is the read that DECIDES which tenant the request belongs to, so
// requiring a tenant first would be circular.
//
// What it can do is make every one of them deliberate. `postgres.DB.SQL()`
// returns the raw handle with no scope; this pins the list of places that use
// it, so a seventh appears in a diff with a reason beside it rather than as a
// convenience somebody reached for at 5pm.
//
// The alternative — trusting review — is what `P1-20` was doing when `events`
// turned out to be writable on staging while the code that prevented it read
// perfectly.
func TestOnlyNamedPlacesBypassTheTenantScope(t *testing.T) {
	// path → why this one is allowed to run unscoped.
	allowed := map[string]string{
		"internal/oauth/client/store.go":  "resolving a client_id before any tenant is known (P1-29's SECURITY DEFINER lookups)",
		"internal/oauth/token/refresh.go": "resolving a refresh token by hash, which is what tells us the organization",
		"internal/session/manager.go":     "resolving a session cookie, likewise",
		"internal/session/store.go":       "resolving a session cookie, likewise",
		"cmd/authservice/main.go":         "infrastructure: connection-pool metrics and the signing key store, neither of which is tenant data",
	}

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolving the source root: %v", err)
	}

	found := map[string]int{}
	scanned := 0

	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Generated code and vendored dependencies are not ours to police.
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

		rel := filepath.ToSlash(mustRel(t, root, path))
		for _, line := range strings.Split(string(source), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			if strings.Contains(line, ".SQL()") {
				found[rel]++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the source: %v", err)
	}

	// The guard against the guard. An assertion satisfied by a walk that read
	// nothing is the shape this project has shipped twice.
	if scanned < 50 {
		t.Fatalf("only %d source files were scanned, too few to have searched anything", scanned)
	}

	for path := range found {
		if _, named := allowed[path]; !named {
			t.Errorf("%s runs a query outside any tenant scope and is not in the allowed list.\n"+
				"    If it genuinely resolves a tenant, add it with the reason. If it does not, use WithTenant.", path)
		}
	}

	// And the list does not rot: an entry for a file that no longer bypasses
	// anything is an exception nobody is checking.
	for path, why := range allowed {
		if found[path] == 0 {
			t.Errorf("%s is listed as bypassing the tenant scope (%q) and no longer does — remove the exception", path, why)
		}
	}
}

func mustRel(t *testing.T, base, path string) string {
	t.Helper()
	rel, err := filepath.Rel(base, path)
	if err != nil {
		t.Fatalf("relative path: %v", err)
	}
	return rel
}
