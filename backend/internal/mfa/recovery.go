package mfa

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"fmt"
	"strings"
)

// Recovery codes (P3-04).
//
// The path back for a user whose factor is gone. Everything about their shape
// is decided by the two situations they are used in: written on paper or pasted
// from a password manager, by somebody who is already having a bad day.
//
// So they are case-insensitive, separator-insensitive, and forgiving of the
// characters a reader can only have misread. A correct code that is refused
// because it was typed in lower case is indistinguishable, to the person typing
// it, from having lost the account.

const (
	// RecoveryCodeBytes is the entropy in one code.
	//
	// Ten bytes — 80 bits. Far beyond what the online bound needs (`P3-03`'s
	// ten guesses per fifteen minutes makes even 40 bits unreachable), and the
	// size is chosen for the OFFLINE case instead: this is the number that
	// makes a stolen database of hashes worthless, and it is the reason the
	// hash below does not need to be slow. See PG-39.
	RecoveryCodeBytes = 10

	// RecoveryCodeCount is how many are issued in a batch.
	//
	// Ten. Enough that a user who spends one every few months is not back for a
	// new batch within the year, and few enough to fit on something a person
	// will actually keep.
	RecoveryCodeCount = 10

	// RecoveryLowWaterMark is when a user should be warned (F-4).
	//
	// Three. Early enough to act on while they still have working codes, and
	// late enough that it is not noise for somebody who has spent one.
	RecoveryLowWaterMark = 3
)

// recoveryAlphabet is base32 without padding: A-Z and 2-7.
//
// Standard RFC 4648 base32 rather than a bespoke alphabet, so the encoding is
// one the standard library already gets right.
//
// **It does contain `O`, `I` and `L`.** An earlier comment here claimed
// otherwise and was simply wrong; a test asserting the claim is what found it.
// What the alphabet lacks is the DIGITS those letters are confused with — there
// is no `0`, `1`, `8` or `9` in any generated code — and that asymmetry is what
// `confusable` below turns into a usability win rather than a hazard: a reader
// who sees `0` and types `0` can be given the `O` that was printed, because `0`
// could not have been there.
//
// Two genuinely ambiguous pairs remain, `2`/`Z` and `5`/`S`, where both
// characters are valid and neither can be corrected without guessing. They are
// why the code is printed in groups of four in a monospaced context rather than
// why it is re-encoded: the honest mitigation for those is legibility, and the
// user always has nine more codes.
var recoveryAlphabet = base32.StdEncoding.WithPadding(base32.NoPadding)

// confusable maps a character that CANNOT appear in a generated code onto the
// one it was almost certainly misread from.
//
// Only unambiguous repairs. Each key is absent from the alphabet, so mapping it
// cannot turn one valid code into a different valid code — it can only turn an
// impossible string into the code the person was looking at. `2`/`Z` and
// `5`/`S` are deliberately absent from this table: both members are valid, so
// "correcting" either would be a guess that could silently spend the wrong
// code.
var confusable = map[rune]rune{
	'0': 'O',
	'1': 'I',
	'8': 'B',
	'9': 'G',
}

// recoveryGroup is how many characters are printed between separators.
const recoveryGroup = 4

// NewRecoveryCode mints one code, formatted for a human.
func NewRecoveryCode() (string, error) {
	raw := make([]byte, RecoveryCodeBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("mfa: generating a recovery code: %w", err)
	}
	return FormatRecoveryCode(recoveryAlphabet.EncodeToString(raw)), nil
}

// NewRecoveryCodes mints a batch.
func NewRecoveryCodes(count int) ([]string, error) {
	if count <= 0 {
		return nil, fmt.Errorf("mfa: a recovery batch of %d codes is not a batch", count)
	}

	codes := make([]string, 0, count)
	seen := map[string]bool{}

	for len(codes) < count {
		code, err := NewRecoveryCode()
		if err != nil {
			return nil, err
		}
		// A duplicate inside one batch is a CSPRNG failure rather than bad
		// luck — at 80 bits it will not happen — but a batch containing the
		// same code twice would give the user nine codes while telling them
		// they have ten, and the unique index would refuse the insert anyway.
		if seen[code] {
			continue
		}
		seen[code] = true
		codes = append(codes, code)
	}
	return codes, nil
}

// FormatRecoveryCode groups a code for display.
//
// `ABCD-EFGH-IJKL-MNOP`. The separators are for the eye and the fingers; they
// are stripped again by NormaliseRecoveryCode, so a user may type them, omit
// them, or use spaces instead.
func FormatRecoveryCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))

	var out strings.Builder
	for i, r := range code {
		if i > 0 && i%recoveryGroup == 0 {
			out.WriteByte('-')
		}
		out.WriteRune(r)
	}
	return out.String()
}

// NormaliseRecoveryCode reduces what somebody typed to what was generated.
//
// Upper-cases, repairs the four unambiguous misreadings, and drops separators —
// dashes, spaces, the non-breaking space a PDF copy-paste introduces.
//
// **Nothing here can make one valid code into another.** The repairs are only
// of characters absent from the alphabet, and everything else that is dropped
// could not have been generated either, so the worst this can do to a wrong
// submission is leave it wrong at a different length — which the shape check
// then refuses.
func NormaliseRecoveryCode(submitted string) string {
	var out strings.Builder
	out.Grow(len(submitted))

	for _, r := range strings.ToUpper(submitted) {
		if repaired, ok := confusable[r]; ok {
			r = repaired
		}
		switch {
		case r >= 'A' && r <= 'Z':
			out.WriteRune(r)
		case r >= '2' && r <= '7':
			out.WriteRune(r)
		}
	}
	return out.String()
}

// recoveryCodeLength is how many characters an encoded code has.
var recoveryCodeLength = recoveryAlphabet.EncodedLen(RecoveryCodeBytes)

// ValidRecoveryCodeShape reports whether a normalised code could be one of ours.
//
// Checked BEFORE any store read, so a malformed submission costs no query — the
// same reason `P1-13` checks its limiter before Argon2. It is not a security
// control on its own: a value of the right length is still refused unless it
// hashes to something stored.
func ValidRecoveryCodeShape(normalised string) bool {
	return len(normalised) == recoveryCodeLength
}

// HashRecoveryCode is how a code is stored and looked up.
//
// **SHA-256, not Argon2**, deviating visibly from `docs/PLAN/04`'s "hashed with
// the same rigor as a password". The reasoning is in the migration and in
// `PG-39`, and the short form is that a slow KDF defends low-entropy inputs
// against enumeration, this input has 80 bits and no candidate list, and ten
// memory-hard computations per attempt would be a denial of service on a path
// an attacker holding a password can drive.
//
// The code is normalised first, so what is hashed is what was generated rather
// than what was typed.
func HashRecoveryCode(code string) []byte {
	sum := sha256.Sum256([]byte(NormaliseRecoveryCode(code)))
	return sum[:]
}

// RecoveryHashesEqual compares two stored hashes in constant time.
//
// The comparison is not obviously timing-sensitive — both sides are hashes of
// values the attacker does not hold — but a lookup that scans a user's ten
// rows compares repeatedly, and doing it properly costs nothing.
func RecoveryHashesEqual(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}
