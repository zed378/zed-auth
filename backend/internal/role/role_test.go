package role

import (
	"errors"
	"strings"
	"testing"
)

// The permission key format is a one-way door: consumer applications will
// store these strings and branch on them, so widening the pattern later is
// harmless and narrowing it breaks deployed code that cannot be seen from
// here. That asymmetry is why the accepted set is enumerated rather than
// sampled.
func TestPermissionKeyAccepts(t *testing.T) {
	for _, key := range []string{
		"user:read",
		"billing:write",
		"billing.invoice:read",
		"a:b",
		"deeply.nested.resource.name:action",
		"user_profile:read_all",
		"x1:y2",
		strings.Repeat("a", 60) + ":read",
	} {
		if err := ValidatePermissionKey(key); err != nil {
			t.Errorf("%q was refused: %v", key, err)
		}
	}
}

func TestPermissionKeyRejects(t *testing.T) {
	cases := map[string]string{
		"":                                 "empty",
		"user":                             "no action",
		"user:":                            "empty action",
		":read":                            "empty resource",
		"User:read":                        "upper case resource",
		"user:Read":                        "upper case action",
		"user:read:write":                  "two colons",
		"user.:read":                       "trailing dot",
		".user:read":                       "leading dot",
		"user..profile:read":               "double dot",
		"1user:read":                       "resource starts with a digit",
		"user:1read":                       "action starts with a digit",
		"user-profile:read":                "hyphen is not in the resource set",
		"user:read-all":                    "hyphen is not in the action set",
		"user :read":                       "space",
		"user:read ":                       "trailing space",
		"user:read\n":                      "newline",
		"*":                                "bare wildcard",
		"user:*":                           "wildcard action",
		"*:read":                           "wildcard resource",
		"billing.*:read":                   "wildcard segment",
		"user:read;DROP TABLE":             "punctuation",
		"user:read'":                       "quote",
		strings.Repeat("a", 200) + ":read": "too long",
	}
	for key, why := range cases {
		if err := ValidatePermissionKey(key); err == nil {
			t.Errorf("%q (%s) was accepted", key, why)
		}
	}
}

// A wildcard gets its own message, because it is the mistake somebody makes on
// purpose and a restatement of the regex does not tell them why the answer is
// no.
func TestAWildcardIsRefusedWithItsReason(t *testing.T) {
	err := ValidatePermissionKey("user:*")
	if err == nil {
		t.Fatal("a wildcard was accepted")
	}
	if !strings.Contains(err.Error(), "wildcard") {
		t.Errorf("the refusal does not mention the wildcard: %v", err)
	}
	if !strings.Contains(err.Error(), "decision") {
		t.Errorf("the refusal does not say where matching belongs: %v", err)
	}
}

func TestRoleKeyAccepts(t *testing.T) {
	for _, key := range []string{"admin", "read-only", "billing_admin", "a", "9lives", "x", strings.Repeat("a", 63)} {
		if err := ValidateKey(key); err != nil {
			t.Errorf("%q was refused: %v", key, err)
		}
	}
}

func TestRoleKeyRejects(t *testing.T) {
	cases := map[string]string{
		"":                      "empty",
		"Admin":                 "upper case",
		"-leading-hyphen":       "starts with a hyphen",
		"_leading_underscore":   "starts with an underscore",
		"has space":             "space",
		"has.dot":               "dot",
		"has:colon":             "colon",
		"emoji🙂":                "non-ascii",
		strings.Repeat("a", 64): "too long",
	}
	for key, why := range cases {
		if err := ValidateKey(key); err == nil {
			t.Errorf("%q (%s) was accepted", key, why)
		}
	}
}

// A project role named `org_admin` would arrive in a token beside a manager
// role of the same name meaning something entirely different, and the consumer
// reading it would have no way to tell which it got (`docs/PLAN/08` Part C).
func TestManagerRoleNamesAreReserved(t *testing.T) {
	for _, key := range []string{"instance_owner", "org_owner", "org_admin", "project_owner", "project_grant_owner"} {
		err := ValidateKey(key)
		if err == nil {
			t.Errorf("%q was accepted as a project role key", key)
			continue
		}
		if !strings.Contains(err.Error(), "reserved") {
			t.Errorf("%q was refused for the wrong reason: %v", key, err)
		}
	}
}

// `admin` is the most natural name a consumer application will reach for.
// Reserving it to prevent a confusion nobody has yet would trade a real need
// for a theoretical one — so this asserts the absence of a rule, which is the
// kind of decision that gets quietly reversed without a test.
func TestAdminIsNotReserved(t *testing.T) {
	if err := ValidateKey("admin"); err != nil {
		t.Errorf("admin was refused: %v", err)
	}
	if err := ValidateKey("administrator"); err != nil {
		t.Errorf("administrator was refused: %v", err)
	}
}

func TestDuplicatePermissionKeysAreRefusedNotFolded(t *testing.T) {
	err := ValidatePermissionKeys([]string{"user:read", "billing:write", "user:read"})
	if err == nil {
		t.Fatal("a duplicate was accepted")
	}
	var fe FieldError
	if !errors.As(err, &fe) {
		t.Fatalf("not a field error: %v", err)
	}
	// The index of the SECOND occurrence, naming where the first one was.
	if fe.Field != "permission_keys[2]" {
		t.Errorf("field is %q, want permission_keys[2]", fe.Field)
	}
	if !strings.Contains(fe.Detail, "index 0") {
		t.Errorf("the message does not point at the first occurrence: %q", fe.Detail)
	}
}

func TestAnEmptyPermissionSetIsAllowed(t *testing.T) {
	// A role with no permissions is a label, and labels are useful before the
	// permissions exist.
	if err := ValidatePermissionKeys(nil); err != nil {
		t.Errorf("nil was refused: %v", err)
	}
	if err := ValidatePermissionKeys([]string{}); err != nil {
		t.Errorf("empty was refused: %v", err)
	}
}

func TestPermissionKeyCountIsBounded(t *testing.T) {
	keys := make([]string, MaxPermissionKeys+1)
	for i := range keys {
		keys[i] = "resource" + itoa(i) + ":read"
	}
	if err := ValidatePermissionKeys(keys); err == nil {
		t.Errorf("%d permission keys were accepted", len(keys))
	}
	if err := ValidatePermissionKeys(keys[:MaxPermissionKeys]); err != nil {
		t.Errorf("%d permission keys were refused: %v", MaxPermissionKeys, err)
	}
}

// The offending entry is named by index, so a form can point at the row the
// user got wrong rather than at the whole field.
func TestTheOffendingPermissionKeyIsNamedByIndex(t *testing.T) {
	err := ValidatePermissionKeys([]string{"user:read", "nope", "billing:write"})
	var fe FieldError
	if !errors.As(err, &fe) {
		t.Fatalf("not a field error: %v", err)
	}
	if fe.Field != "permission_keys[1]" {
		t.Errorf("field is %q, want permission_keys[1]", fe.Field)
	}
}

func TestDisplayNameIsTrimmedAndBounded(t *testing.T) {
	if err := ValidateDisplayName("   "); err == nil {
		t.Error("whitespace was accepted as a display name")
	}
	if err := ValidateDisplayName("  Billing Administrator  "); err != nil {
		t.Errorf("a padded name was refused: %v", err)
	}
	if err := ValidateDisplayName(strings.Repeat("a", MaxDisplayNameLength+1)); err == nil {
		t.Error("an over-long display name was accepted")
	}
}

// The generated constants must be what the spec says. If the generator is
// wrong or was not re-run, everything above still passes against whatever
// pattern happens to be compiled in — so the pattern itself is asserted, not
// only its behaviour.
func TestTheGeneratedPatternsAreTheOnesTheSpecDocuments(t *testing.T) {
	const wantPermission = `^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*:[a-z][a-z0-9_]*$`
	const wantRole = `^[a-z0-9][a-z0-9_-]{0,62}$`

	if PermissionKeyPattern != wantPermission {
		t.Errorf("PermissionKeyPattern is %q, want %q — regenerate with npm run patterns:generate", PermissionKeyPattern, wantPermission)
	}
	if RoleKeyPattern != wantRole {
		t.Errorf("RoleKeyPattern is %q, want %q", RoleKeyPattern, wantRole)
	}
	if MaxPermissionKeyLength != 128 || MaxRoleKeyLength != 63 {
		t.Errorf("lengths are %d and %d, want 128 and 63", MaxPermissionKeyLength, MaxRoleKeyLength)
	}
}

// Nothing outside the pattern is ever accepted, and nothing panics. `P1-27`'s
// FuzzVerify found nothing across 944k executions, and finding nothing is the
// result a fuzz target is for.
func FuzzValidatePermissionKey(f *testing.F) {
	for _, seed := range []string{"user:read", "billing.invoice:write", "", "*", "a:b", "A:B", "user:", ":x", "user::read"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, key string) {
		err := ValidatePermissionKey(key)
		if err != nil {
			return
		}
		// Accepted. It must then satisfy every property the format promises.
		if strings.Contains(key, "*") {
			t.Fatalf("accepted a wildcard: %q", key)
		}
		if len(key) > MaxPermissionKeyLength {
			t.Fatalf("accepted an over-long key: %d characters", len(key))
		}
		if strings.Count(key, ":") != 1 {
			t.Fatalf("accepted %q with %d colons", key, strings.Count(key, ":"))
		}
		if key != strings.ToLower(key) {
			t.Fatalf("accepted upper case: %q", key)
		}
		if strings.ContainsAny(key, " \t\r\n\"'\\;()[]{}<>") {
			t.Fatalf("accepted punctuation or whitespace: %q", key)
		}
	})
}

func FuzzValidateKey(f *testing.F) {
	for _, seed := range []string{"admin", "read-only", "", "Admin", "org_admin", "-x", strings.Repeat("a", 64)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, key string) {
		if err := ValidateKey(key); err != nil {
			return
		}
		if len(key) == 0 || len(key) > MaxRoleKeyLength {
			t.Fatalf("accepted a key of length %d", len(key))
		}
		if key != strings.ToLower(key) {
			t.Fatalf("accepted upper case: %q", key)
		}
		if _, reserved := ReservedKeys[key]; reserved {
			t.Fatalf("accepted a reserved key: %q", key)
		}
	})
}

func itoa(n int) string {
	if n == 0 {
		return "a"
	}
	out := []byte{}
	for n > 0 {
		out = append([]byte{byte('a' + n%26)}, out...)
		n /= 26
	}
	return string(out)
}
