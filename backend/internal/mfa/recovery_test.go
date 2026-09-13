package mfa

import (
	"strings"
	"testing"
)

// Recovery code shape, normalisation and hashing (P3-04).
//
// The property under test throughout is that **a correct code cannot be made
// wrong by how it was typed**. A user reaching this form has lost their phone
// and is reading off paper; a code refused because of a lower-case letter is,
// from where they are standing, indistinguishable from having lost the account.

func TestAGeneratedCodeHasTheEntropyItClaims(t *testing.T) {
	code, err := NewRecoveryCode()
	if err != nil {
		t.Fatalf("NewRecoveryCode: %v", err)
	}

	normalised := NormaliseRecoveryCode(code)
	if !ValidRecoveryCodeShape(normalised) {
		t.Fatalf("a freshly generated code %q does not pass its own shape check", code)
	}

	// 10 bytes base32-encodes to 16 characters. If this changes, the entropy
	// changed with it, and PG-39's argument for a fast hash depends on the
	// number.
	if len(normalised) != 16 {
		t.Errorf("a code is %d characters, want 16 — 80 bits is what makes SHA-256 sufficient here",
			len(normalised))
	}
}

// No generated code contains a digit that a letter could be misread as.
//
// The property is NOT "the alphabet has no confusable characters" — base32 has
// O, I and L, and an earlier version of this test asserted the comment rather
// than the code and duly failed. What holds is the asymmetry: 0, 1, 8 and 9
// never appear, so seeing one means the reader misread a letter, and the repair
// below is unambiguous.
func TestNoGeneratedCodeContainsARepairableDigit(t *testing.T) {
	for i := 0; i < 200; i++ {
		code, err := NewRecoveryCode()
		if err != nil {
			t.Fatalf("NewRecoveryCode: %v", err)
		}
		for bad := range confusable {
			if strings.ContainsRune(NormaliseRecoveryCode(code), bad) {
				t.Fatalf("code %q contains %q, which normalisation would rewrite", code, string(bad))
			}
		}
	}
}

// A misread letter is repaired, so the code the user is looking at is the code
// they get.
func TestMisreadCharactersAreRepaired(t *testing.T) {
	for _, tc := range []struct{ typed, want string }{
		{"0BCD2345EFGH67ZS", "OBCD2345EFGH67ZS"}, // zero for O
		{"1BCD2345EFGH67ZS", "IBCD2345EFGH67ZS"}, // one for I
		{"8CDE2345EFGH67ZS", "BCDE2345EFGH67ZS"}, // eight for B
		{"9CDE2345EFGH67ZS", "GCDE2345EFGH67ZS"}, // nine for G
	} {
		if got := NormaliseRecoveryCode(tc.typed); got != tc.want {
			t.Errorf("NormaliseRecoveryCode(%q) = %q, want %q", tc.typed, got, tc.want)
		}
	}
}

// The genuinely ambiguous pairs are NOT repaired. Both members are valid, so
// "correcting" either would be a guess that could spend the wrong code.
func TestAmbiguousPairsAreLeftAlone(t *testing.T) {
	for _, r := range []rune{'2', 'Z', '5', 'S'} {
		if _, repaired := confusable[r]; repaired {
			t.Errorf("%q is rewritten by normalisation, but both it and its lookalike are valid codes",
				string(r))
		}
	}
}

// A batch has no duplicates. Two identical codes would give a user nine while
// telling them they have ten — and the unique index would refuse the insert.
func TestABatchHasNoDuplicates(t *testing.T) {
	codes, err := NewRecoveryCodes(RecoveryCodeCount)
	if err != nil {
		t.Fatalf("NewRecoveryCodes: %v", err)
	}
	if len(codes) != RecoveryCodeCount {
		t.Fatalf("got %d codes, want %d", len(codes), RecoveryCodeCount)
	}

	seen := map[string]bool{}
	for _, code := range codes {
		if seen[code] {
			t.Fatalf("the batch contains %q twice", code)
		}
		seen[code] = true
	}
}

// How it was typed cannot make a correct code wrong.
func TestTypingVariationsAllReachTheSameCode(t *testing.T) {
	const canonical = "ABCD2345EFGH67ZS"

	for _, typed := range []string{
		"ABCD2345EFGH67ZS",     // as generated
		"abcd2345efgh67zs",     // all lower case
		"ABCD-2345-EFGH-67ZS",  // as displayed
		"abcd-2345-efgh-67zs",  // as displayed, lower case
		"ABCD 2345 EFGH 67ZS",  // spaces instead of dashes
		"  ABCD2345EFGH67ZS  ", // padded by a copy-paste
		"ABCD 2345EFGH67ZS",    // a non-breaking space from a PDF
		"AbCd-2345-eFgH-67zs",  // however it came out of the clipboard
	} {
		if got := NormaliseRecoveryCode(typed); got != canonical {
			t.Errorf("NormaliseRecoveryCode(%q) = %q, want %q", typed, got, canonical)
		}
		if !RecoveryHashesEqual(HashRecoveryCode(typed), HashRecoveryCode(canonical)) {
			t.Errorf("%q does not hash to the same value as %q", typed, canonical)
		}
	}
}

// Normalisation does not make a WRONG code right. It only removes characters
// that could not have been generated.
func TestNormalisationCannotRepairAWrongCode(t *testing.T) {
	const right = "ABCD2345EFGH67ZS"

	for _, wrong := range []string{
		"ABCD2345EFGH67ZA", // one character different
		"ABCD2345EFGH67Z",  // one character short
		"ABCD2345EFGH67ZSA",
		"",
	} {
		if NormaliseRecoveryCode(wrong) == right {
			t.Errorf("%q normalised to the correct code", wrong)
		}
		if RecoveryHashesEqual(HashRecoveryCode(wrong), HashRecoveryCode(right)) {
			t.Errorf("%q hashes to the same value as the correct code", wrong)
		}
	}
}

// A digit that is not ASCII is dropped, not accepted and not repaired.
//
// This is the digit range's remaining job. For ASCII it is now redundant with
// `confusable`, which rewrites 0, 1, 8 and 9 into letters before the switch
// sees them — a mutation run found that widening the range to '9' changed
// nothing, which is what sent me looking for what it still decides.
//
// It decides this: a fullwidth or Arabic-Indic digit, which a paste from a
// document can carry, is corruption rather than a misreading. Dropping it makes
// the length wrong and the shape check refuses, which is honest. Mapping it to
// an ASCII digit would be a guess, and a guess here can spend the wrong code.
func TestNonASCIIDigitsAreDropped(t *testing.T) {
	for _, tc := range []struct{ name, typed string }{
		{"fullwidth", "ABCD2345EFGH67Z５"},
		{"arabic-indic", "ABCD2345EFGH67Z٥"},
		{"devanagari", "ABCD2345EFGH67Z५"},
	} {
		got := NormaliseRecoveryCode(tc.typed)
		if got != "ABCD2345EFGH67Z" {
			t.Errorf("%s: NormaliseRecoveryCode(%q) = %q, want the ASCII characters only",
				tc.name, tc.typed, got)
		}
		if ValidRecoveryCodeShape(got) {
			t.Errorf("%s: a code with a non-ASCII digit passed the shape check", tc.name)
		}
	}
}

// A code of the wrong length is refused by the shape check, before any store
// read — the reason P1-13 checks its limiter before Argon2.
func TestTheShapeCheckRefusesWhatCouldNotHaveBeenGenerated(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  bool
	}{
		{"a real code", "ABCD2345EFGH67ZS", true},
		{"too short", "ABCD2345", false},
		{"too long", "ABCD2345EFGH67ZSABCD", false},
		{"empty", "", false},
	} {
		if got := ValidRecoveryCodeShape(tc.value); got != tc.want {
			t.Errorf("%s: ValidRecoveryCodeShape(%q) = %v, want %v", tc.name, tc.value, got, tc.want)
		}
	}
}

// The display format groups the code without changing it.
func TestFormattingIsReversibleByNormalisation(t *testing.T) {
	code, err := NewRecoveryCode()
	if err != nil {
		t.Fatalf("NewRecoveryCode: %v", err)
	}

	if !strings.Contains(code, "-") {
		t.Errorf("a displayed code %q has no grouping; it is 16 characters to copy by eye", code)
	}
	if got := len(NormaliseRecoveryCode(code)); got != 16 {
		t.Errorf("normalising a formatted code gave %d characters, want 16", got)
	}
}

// The hash is 32 bytes, which the migration's CHECK constraint requires. A
// different length would be refused by the database at insert, which is a
// worse place to find out.
func TestTheHashIsTheLengthTheColumnAccepts(t *testing.T) {
	if got := len(HashRecoveryCode("ABCD2345EFGH67ZS")); got != 32 {
		t.Errorf("HashRecoveryCode returned %d bytes; the column's CHECK requires 32", got)
	}
}

// Two different codes do not collide. Obvious for SHA-256, asserted because
// the whole single-use guarantee rests on a hash identifying one code.
func TestDifferentCodesHashDifferently(t *testing.T) {
	codes, err := NewRecoveryCodes(50)
	if err != nil {
		t.Fatalf("NewRecoveryCodes: %v", err)
	}

	seen := map[string]string{}
	for _, code := range codes {
		key := string(HashRecoveryCode(code))
		if other, clash := seen[key]; clash {
			t.Fatalf("%q and %q hash to the same value", code, other)
		}
		seen[key] = code
	}
}

// A batch of zero is refused rather than returning an empty set that would read
// as "this user has recovery codes" everywhere downstream.
func TestAnEmptyBatchIsRefused(t *testing.T) {
	if _, err := NewRecoveryCodes(0); err == nil {
		t.Error("a batch of zero codes was allowed")
	}
	if _, err := NewRecoveryCodes(-1); err == nil {
		t.Error("a batch of minus one codes was allowed")
	}
}
