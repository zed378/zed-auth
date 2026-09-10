// Package authn holds credential verification: the code that answers "is this
// the right password", and nothing about what the answer permits.
//
// That boundary is deliberate. Verifying a credential and authorizing a
// request are different questions, and a package that answers both is one
// where "the password matched" quietly becomes "the request is allowed".
// Authorization lives in internal/authz (docs/PLAN/08).
//
// Specification: MEMORY/specs/P1-01-password-hashing.md.
package authn

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id, per docs/PLAN/09 § Passwords & Credentials and docs/PLAN/07 § Cryptography.
//
// The hybrid rather than Argon2i or Argon2d: Argon2d resists GPU attack but
// leaks through memory access patterns, Argon2i is the reverse, and Argon2id
// is the construction that gets both. It is also what OWASP recommends, which
// matters less than the reason but is worth not diverging from without one.
const (
	// The PHC identifier. A stored hash naming anything else is refused rather
	// than attempted — accepting an unexpected algorithm is how a downgrade
	// gets in.
	algorithm = "argon2id"

	// The PHC-encoded Argon2 version. A future library that changes this
	// produces hashes this code refuses, which is the correct failure: a
	// silent version mismatch would verify wrongly.
	version = argon2.Version
)

// Params is one parameter set. Every hash records the parameters it was made
// with, so raising them is a deploy rather than a migration.
type Params struct {
	// Memory in KiB. The main defence: GPUs have thousands of cores and not
	// much memory per core, so memory cost is what makes parallel cracking
	// expensive. Raise this before raising Time.
	Memory uint32

	// Passes over memory.
	Time uint32

	// Lanes computed in parallel. Bounded by the server's cores, and by the
	// fact that memory cost is divided among them.
	Parallelism uint8

	// Salt length in bytes. 16 is the RFC 9106 recommendation.
	SaltLength uint32

	// Derived key length in bytes.
	KeyLength uint32
}

// Current is the parameter set new hashes are made with.
//
// Chosen by measurement rather than by copying a blog post — see
// `BenchmarkHash` and the numbers in the MEMORY record. Three constraints
// bound it:
//
//	Latency: a login should not feel slow. ~50-100ms of hashing is invisible
//	  next to a network round trip.
//	Concurrency: memory cost multiplies by concurrent logins. At 64 MiB, a
//	  hundred simultaneous logins want 6.4 GiB — which is why this is 64 and
//	  not the 1 GiB some recommendations suggest for a machine doing nothing
//	  else. The staging VM does many things.
//	Resistance: within those bounds, as high as possible.
//
// RFC 9106's second recommended option is 64 MiB, t=3, p=4, and this matches
// it. That is a coincidence worth stating: the constraints above were applied
// first and landed on the same place, which is mild evidence the constraints
// were not nonsense.
var Current = Params{
	Memory:      64 * 1024, // 64 MiB
	Time:        3,
	Parallelism: 4,
	SaltLength:  16,
	KeyLength:   32,
}

// Bounds on the salt and key read back from a stored hash.
//
// gosec flagged the int -> uint32 conversion of their decoded lengths (G115),
// and it was right to. The values come from a database column, and a column is
// not trusted input just because it is ours — it can be corrupt, truncated by
// a bad migration, or written by something else.
//
// The fix is bounds rather than a suppression, because an absurd salt or key
// length IS a malformed hash: 4 bytes of salt is too weak to be genuine and
// a megabyte of it is not something this package ever produced. Rejecting both
// closes the conversion concern and an unbounded-allocation path at once.
const (
	minSaltBytes = 8
	maxSaltBytes = 64
	minKeyBytes  = 16
	maxKeyBytes  = 64
)

// maxPasswordBytes caps the input before hashing.
//
// Argon2's cost is dominated by memory and iterations rather than by input
// length, so a long password is not itself expensive. It is still an unbounded
// read from a request an attacker controls, and there is no legitimate
// ten-megabyte password.
const maxPasswordBytes = 1024

var (
	// ErrInvalidHash means the STORED value is unusable — truncated, wrong
	// algorithm, corrupt encoding. Distinct from a wrong password on purpose:
	// a wrong password is a user error, and this is data corruption that
	// should reach somebody.
	ErrInvalidHash = errors.New("authn: stored password hash is malformed")

	// ErrIncompatibleVersion means the hash was made by a different Argon2
	// version. Refused rather than attempted.
	ErrIncompatibleVersion = errors.New("authn: password hash has an incompatible argon2 version")

	// ErrPasswordTooLong is returned by Hash, never by Verify. Verify accepts
	// any input, because rejecting one early would be a timing signal.
	ErrPasswordTooLong = fmt.Errorf("authn: password exceeds %d bytes", maxPasswordBytes)

	// ErrEmptyPassword is returned by Hash. Verify has no equivalent: an empty
	// password is verified normally and fails, because short-circuiting it
	// would be measurably faster than a real attempt.
	ErrEmptyPassword = errors.New("authn: password is empty")
)

// Hash derives a PHC-encoded Argon2id hash with a fresh random salt.
//
// The returned string carries the parameters, so a future increase is
// detectable per row and old hashes keep verifying at their own cost.
//
// No error returned by this function contains the password or any part of it
// (docs/PLAN/09: never log passwords, even failed attempts). That is asserted by a
// test, because it is the kind of property a helpful error message
// accidentally removes.
func Hash(password string) (string, error) {
	return HashWith(password, Current)
}

// HashWith is Hash against explicit parameters. Exported for the tests that
// need a deliberately weak hash to exercise the upgrade path, and for a future
// migration that has to match another system's cost.
func HashWith(password string, p Params) (string, error) {
	if password == "" {
		return "", ErrEmptyPassword
	}
	if len(password) > maxPasswordBytes {
		return "", ErrPasswordTooLong
	}

	salt := make([]byte, p.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		// Never fall back to a weaker source. A predictable salt makes every
		// hash in the database attackable with one rainbow table, and a
		// failure to read randomness means something is wrong that hashing a
		// password will not fix.
		return "", fmt.Errorf("authn: reading salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Parallelism, p.KeyLength)

	return encode(p, salt, key), nil
}

// Result is the outcome of a verification.
type Result struct {
	// Match is whether the password is correct.
	Match bool

	// NeedsRehash is true when Match is true and the stored hash used weaker
	// parameters than Current. The caller should rehash and store.
	//
	// False when the stored hash is STRONGER than current — downgrading a hash
	// is worse than leaving it, and a deploy that lowers parameters must not
	// quietly weaken every password that logs in afterwards.
	NeedsRehash bool
}

// Verify checks a password against a PHC-encoded hash.
//
// It honours the parameters recorded in the hash, not the current ones, so a
// hash written before a parameter increase still verifies.
//
// A wrong password is `(Result{}, nil)` — not an error. An error means the
// stored hash could not be used at all.
func Verify(encoded, password string) (Result, error) {
	p, salt, want, err := decode(encoded)
	if err != nil {
		return Result{}, err
	}

	got := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Parallelism, p.KeyLength)

	// Constant time. A byte-by-byte comparison leaks how much of the derived
	// key matched, and while extracting a key that way is slow and noisy, the
	// constant-time version costs nothing and removes the question.
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return Result{}, nil
	}

	return Result{Match: true, NeedsRehash: weakerThanCurrent(p)}, nil
}

// VerifyDummy performs a hash of equivalent cost and always reports no match.
//
// This is the enumeration defence (docs/SECURITY/02 §12). A login for an address
// with no account must cost what a real one costs — otherwise the response
// time tells an attacker which addresses are registered, and that is a
// password-reset list, a phishing list, and confirmation that a person works
// somewhere.
//
// The caller must invoke this on the not-found path rather than returning
// early. It is a real Argon2 computation, not a sleep: a sleep has to guess
// the right duration and gets it wrong as parameters change, and it does not
// consume the CPU that makes the timings match under load.
func VerifyDummy(password string) Result {
	// A fixed salt is fine and is not a secret. Nothing is stored, nothing is
	// compared, and the only property that matters is that the work is
	// identical to a real verification.
	salt := make([]byte, Current.SaltLength)

	_ = argon2.IDKey([]byte(password), salt,
		Current.Time, Current.Memory, Current.Parallelism, Current.KeyLength)

	return Result{}
}

// NeedsRehash reports whether a stored hash was made with weaker parameters
// than current, without verifying a password.
//
// Useful for a background audit of how much of the table is below current
// cost. Rehashing still requires the plaintext, so it can only happen at login.
func NeedsRehash(encoded string) (bool, error) {
	p, _, _, err := decode(encoded)
	if err != nil {
		return false, err
	}
	return weakerThanCurrent(p), nil
}

// weakerThanCurrent compares the cost-bearing parameters only.
//
// Salt and key length are not cost. A hash with a shorter salt is not weaker
// in the sense that matters here, and treating it as such would rehash the
// whole table for no security gain.
func weakerThanCurrent(p Params) bool {
	return p.Memory < Current.Memory ||
		p.Time < Current.Time ||
		p.Parallelism < Current.Parallelism ||
		p.KeyLength < Current.KeyLength
}

// encode produces the PHC string format:
//
//	$argon2id$v=19$m=65536,t=3,p=4$<b64 salt>$<b64 key>
//
// The standard format rather than something bespoke, so the parameters are
// readable by anything that understands PHC — including a person looking at a
// row and asking what cost it was written at.
func encode(p Params, salt, key []byte) string {
	return fmt.Sprintf("$%s$v=%d$m=%d,t=%d,p=%d$%s$%s",
		algorithm,
		version,
		p.Memory, p.Time, p.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
}

// decode parses a PHC string.
//
// Every failure path returns an error. None panics, and none reveals anything
// about the password — the input to this function is the STORED value, but a
// caller could pass anything, and a parser that panics on malformed input is a
// denial of service reachable from whatever writes that column.
func decode(encoded string) (p Params, salt, key []byte, err error) {
	parts := strings.Split(encoded, "$")

	// Leading empty field from the initial "$", then five fields.
	if len(parts) != 6 || parts[0] != "" {
		return p, nil, nil, ErrInvalidHash
	}

	if parts[1] != algorithm {
		return p, nil, nil, fmt.Errorf("%w: algorithm is %q, expected %q",
			ErrInvalidHash, parts[1], algorithm)
	}

	var v int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &v); err != nil {
		return p, nil, nil, ErrInvalidHash
	}
	if v != version {
		return p, nil, nil, ErrIncompatibleVersion
	}

	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Time, &p.Parallelism); err != nil {
		return p, nil, nil, ErrInvalidHash
	}

	// Zero parameters would make argon2.IDKey panic. A stored row is not
	// trusted input just because it is ours — it could be corrupt, or written
	// by something else.
	if p.Memory == 0 || p.Time == 0 || p.Parallelism == 0 {
		return p, nil, nil, fmt.Errorf("%w: parameters must be non-zero", ErrInvalidHash)
	}

	salt, err = base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(salt) < minSaltBytes || len(salt) > maxSaltBytes {
		return p, nil, nil, fmt.Errorf("%w: salt length is out of range", ErrInvalidHash)
	}

	key, err = base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil || len(key) < minKeyBytes || len(key) > maxKeyBytes {
		return p, nil, nil, fmt.Errorf("%w: key length is out of range", ErrInvalidHash)
	}

	// #nosec G115 -- both lengths are bounded to [8,64] and [16,64] by the two
	// checks immediately above, so neither conversion can overflow uint32.
	// gosec cannot follow the bound into the conversion; the bound is the real
	// fix and this annotation records why it is sufficient rather than waiving
	// an unexamined finding.
	p.SaltLength = uint32(len(salt))
	// #nosec G115 -- see above.
	p.KeyLength = uint32(len(key))

	return p, salt, key, nil
}
