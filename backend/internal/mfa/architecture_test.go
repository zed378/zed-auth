package mfa

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Architecture tests: claims about the source, asserted by reading it (P3-01).
//
// `P3-01`'s Definition of Done asks that "both factor types implement one
// interface, verified by an architecture test". A test that instantiated the
// two and called a method would prove they compile; what needs proving is the
// negative — that no factor type is reachable **without** going through the
// interface, and that `amr` is not written from anywhere but the session.
//
// The same technique `P2-05` and `P2-09` use, and for the same reason: some
// claims are about every line rather than about one behaviour.

// sourceFiles returns the package's own non-test sources.
func sourceFiles(t *testing.T) map[string]string {
	t.Helper()

	out := map[string]string{}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		out[name] = string(body)
	}

	// Without this, an empty read makes every assertion below hold trivially
	// — the failure mode this project keeps finding.
	if len(out) < 3 {
		t.Fatalf("only %d source file(s) found; these assertions would prove nothing", len(out))
	}
	return out
}

// Every factor type the schema allows has a `Type` constant and an `amr` value.
//
// A type in the database CHECK that this package does not know about would be
// storable and unchallengeable — a user could enrol something nothing can
// verify.
func TestEverySchemaFactorTypeIsImplementedHere(t *testing.T) {
	migration := filepath.Join("..", "..", "migrations", "20260912000027_user_factors.up.sql")
	body, err := os.ReadFile(migration)
	if err != nil {
		t.Fatalf("reading the migration: %v", err)
	}

	// The CHECK constraint names the allowed set.
	const marker = "CHECK (type IN ("
	start := strings.Index(string(body), marker)
	if start < 0 {
		t.Fatalf("the migration has no type CHECK, so this test cannot tell what the schema allows")
	}
	clause := string(body)[start+len(marker):]
	clause = clause[:strings.Index(clause, ")")]

	var found int
	for _, quoted := range strings.Split(clause, ",") {
		name := strings.Trim(strings.TrimSpace(quoted), "'")
		if name == "" {
			continue
		}
		found++
		if !Type(name).Valid() {
			t.Errorf("the schema allows factor type %q and this package does not implement it — "+
				"a user could enrol something nothing can verify", name)
		}
	}
	if found == 0 {
		t.Fatal("no factor types parsed out of the migration; this test proves nothing")
	}
}

// Every method a factor needs is on the interface.
//
// Asserted against the interface's own method set rather than a written list,
// so adding a method to `Verifier` without thinking about it fails here.
func TestTheFactorInterfaceIsTheWholeContract(t *testing.T) {
	iface := reflect.TypeOf((*Verifier)(nil)).Elem()

	want := map[string]bool{
		"Type":    true, // which kind this is, for the registry
		"Begin":   true, // enrolment starts
		"Confirm": true, // enrolment is proven
		"Verify":  true, // a live factor answers a challenge
		"Remove":  true, // a factor is removed
	}

	got := map[string]bool{}
	for i := 0; i < iface.NumMethod(); i++ {
		got[iface.Method(i).Name] = true
	}

	for name := range want {
		if !got[name] {
			t.Errorf("Verifier has no %s method — a factor type would have to expose it "+
				"some other way, which is how two systems end up wearing one name", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("Verifier grew a %s method. An interface that gains one per factor type "+
				"is not an interface; decide whether this belongs inside an implementation", name)
		}
	}
}

// `amr` is derived in exactly one place.
//
// The spec names this as a technical risk: the pressure to write `mfa` because
// a user HAS a factor rather than because they USED one will appear the first
// time a flow is awkward. One function means one place to get it right and one
// place to review.
func TestAMRIsBuiltInOnePlace(t *testing.T) {
	var writers []string

	for name, body := range sourceFiles(t) {
		if strings.Contains(body, MethodMultiFactor+`"`) && !strings.Contains(name, "factor.go") {
			writers = append(writers, name)
		}
	}

	if len(writers) > 0 {
		t.Errorf("the %q value appears in %v as well as factor.go — amr must be derived "+
			"by AuthMethods and nowhere else", MethodMultiFactor, writers)
	}
}

// The challenge state never reaches a caller with the user id in it.
//
// The handle is a lookup key, not a container. A version that encoded the user
// into what the client holds would be one edit away from letting a client name
// a different user, which is abuse case A-1 — and it would still pass every
// behavioural test, because the lookup would keep working.
func TestTheHandleIsMintedFromRandomnessAndNothingElse(t *testing.T) {
	files := sourceFiles(t)

	body, ok := files["challenge.go"]
	if !ok {
		t.Fatal("challenge.go is missing")
	}

	// The function that mints a handle must read from crypto/rand and must not
	// take the challenge as an argument.
	const signature = "func NewHandle() (string, error)"
	if !strings.Contains(body, signature) {
		t.Errorf("NewHandle no longer has the signature %q — if it takes a challenge, "+
			"something about the user can end up inside the handle", signature)
	}
	if !strings.Contains(body, "rand.Read") {
		t.Error("NewHandle does not read from crypto/rand")
	}
}

// Nothing in this package logs a factor secret.
//
// `docs/PLAN/13` names tokens, passwords and resource attributes; the spec adds
// factor material. A secret in a log is a second factor in a log.
func TestNoSourceLogsFactorMaterial(t *testing.T) {
	for name, body := range sourceFiles(t) {
		for _, line := range strings.Split(body, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			if !strings.Contains(trimmed, ".Warn(") && !strings.Contains(trimmed, ".Info(") &&
				!strings.Contains(trimmed, ".Error(") && !strings.Contains(trimmed, ".Debug(") {
				continue
			}
			for _, forbidden := range []string{"Secret", "secret", "code", "Code"} {
				if strings.Contains(trimmed, forbidden) {
					t.Errorf("%s logs something named %q:\n  %s", name, forbidden, trimmed)
				}
			}
		}
	}
}
