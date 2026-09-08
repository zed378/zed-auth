package authn

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestHashAndVerify(t *testing.T) {
	const password = "correct horse battery staple"

	encoded, err := Hash(password)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	result, err := Verify(encoded, password)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !result.Match {
		t.Error("the correct password did not verify")
	}
	if result.NeedsRehash {
		t.Error("a hash written with the current parameters wants a rehash")
	}
}

func TestVerifyRejectsTheWrongPassword(t *testing.T) {
	encoded, err := Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	for _, wrong := range []string{
		"correct horse battery stapl",   // one character short
		"correct horse battery staple ", // one character long
		"Correct horse battery staple",  // case
		"",                              // empty
		"完全に違うパスワード",                    // different alphabet
	} {
		result, err := Verify(encoded, wrong)
		if err != nil {
			t.Errorf("Verify(%q) returned an error; a wrong password is not an error: %v", wrong, err)
		}
		if result.Match {
			t.Errorf("Verify(%q) matched", wrong)
		}
	}
}

// Every hash gets its own salt, so the same password stored twice produces two
// different rows. Without it, a dump reveals which users share a password —
// and one cracked hash cracks all of them.
func TestEveryHashHasItsOwnSalt(t *testing.T) {
	const password = "the same password"

	first, err := Hash(password)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	second, err := Hash(password)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	if first == second {
		t.Fatal("hashing the same password twice produced identical output; the salt is not random")
	}

	// Both must still verify — a test that only checked they differ would pass
	// if hashing were broken and returning noise.
	for i, encoded := range []string{first, second} {
		result, err := Verify(encoded, password)
		if err != nil || !result.Match {
			t.Errorf("hash %d does not verify: match=%v err=%v", i, result.Match, err)
		}
	}
}

// A malformed stored hash is an error, never a panic and never a pass.
//
// The input to Verify is a database column. It could be truncated by a bad
// migration, written by another system, or corrupt — and a parser that panics
// on it is a denial of service reachable from whatever writes that column.
func TestVerifyRejectsMalformedHashes(t *testing.T) {
	valid, err := Hash("password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	tests := map[string]string{
		"empty":               "",
		"not PHC at all":      "hunter2",
		"missing fields":      "$argon2id$v=19$m=65536,t=3,p=4",
		"wrong algorithm":     strings.Replace(valid, "argon2id", "argon2i", 1),
		"bcrypt":              "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy",
		"unknown version":     strings.Replace(valid, "v=19", "v=99", 1),
		"unparseable version": strings.Replace(valid, "v=19", "v=abc", 1),
		"zero memory":         strings.Replace(valid, "m=65536", "m=0", 1),
		"zero time":           strings.Replace(valid, "t=3", "t=0", 1),
		"zero parallelism":    strings.Replace(valid, "p=4", "p=0", 1),
		"unparseable params":  strings.Replace(valid, "m=65536,t=3,p=4", "m=x,t=y,p=z", 1),
		"truncated":           valid[:len(valid)/2],
		"invalid base64 salt": corruptField(valid, 4, "!!!not base64!!!"),
		"invalid base64 key":  corruptField(valid, 5, "!!!not base64!!!"),
		"empty salt":          corruptField(valid, 4, ""),
		"empty key":           corruptField(valid, 5, ""),
		"leading junk":        "junk" + valid,
		"only separators":     "$$$$$",
	}

	for name, encoded := range tests {
		t.Run(name, func(t *testing.T) {
			// A panic here is the failure this test exists for, and Go reports
			// it as a test failure with the stack — no recover needed.
			result, err := Verify(encoded, "password")

			if err == nil {
				t.Errorf("malformed hash accepted without error (match=%v)", result.Match)
			}
			if result.Match {
				t.Error("MALFORMED HASH VERIFIED AS A MATCH")
			}
		})
	}
}

// Salt and key lengths are bounded, and a value outside the range is treated
// as a malformed hash rather than attempted.
//
// gosec found the unbounded int -> uint32 conversion of these lengths (G115).
// Bounds are the fix rather than a suppression, because an absurd length IS a
// malformed hash: four bytes of salt is too weak to be genuine, and a
// megabyte of it is not something this package ever wrote.
func TestSaltAndKeyLengthsAreBounded(t *testing.T) {
	b64 := func(n int) string {
		return base64.RawStdEncoding.EncodeToString(make([]byte, n))
	}
	build := func(saltBytes, keyBytes int) string {
		return "$argon2id$v=19$m=65536,t=3,p=4$" + b64(saltBytes) + "$" + b64(keyBytes)
	}

	tests := map[string]struct {
		salt, key int
		valid     bool
	}{
		"salt too short":  {4, 32, false},
		"salt at minimum": {minSaltBytes, 32, true},
		"salt at maximum": {maxSaltBytes, 32, true},
		"salt too long":   {maxSaltBytes + 1, 32, false},
		"salt absurd":     {100000, 32, false},
		"key too short":   {16, 8, false},
		"key at minimum":  {16, minKeyBytes, true},
		"key at maximum":  {16, maxKeyBytes, true},
		"key too long":    {16, maxKeyBytes + 1, false},
		"key absurd":      {16, 100000, false},
		"both in range":   {16, 32, true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// The password will not match — these are zero-byte keys. What is
			// under test is whether decoding rejects the hash, so the error is
			// the signal and the match is not.
			_, err := Verify(build(tc.salt, tc.key), "any password")

			if tc.valid && err != nil {
				t.Errorf("salt=%d key=%d rejected, want accepted: %v", tc.salt, tc.key, err)
			}
			if !tc.valid && err == nil {
				t.Errorf("salt=%d key=%d accepted, want rejected", tc.salt, tc.key)
			}
		})
	}
}

// corruptField replaces one $-separated field, for building malformed inputs.
func corruptField(encoded string, index int, replacement string) string {
	parts := strings.Split(encoded, "$")
	if index >= len(parts) {
		return encoded
	}
	parts[index] = replacement
	return strings.Join(parts, "$")
}

// P1-01 step 3: transparent rehash-on-login. A parameter increase is worthless
// if existing passwords stay at the old cost forever, and the only moment the
// plaintext is available to rehash is a successful login.
func TestRehashOnLogin(t *testing.T) {
	const password = "a real password"

	weak := Params{
		Memory:      8 * 1024, // deliberately below Current
		Time:        1,
		Parallelism: 1,
		SaltLength:  16,
		KeyLength:   32,
	}

	encoded, err := HashWith(password, weak)
	if err != nil {
		t.Fatalf("HashWith: %v", err)
	}

	result, err := Verify(encoded, password)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !result.Match {
		t.Fatal("a hash written with weaker parameters must still verify")
	}
	if !result.NeedsRehash {
		t.Error("a hash weaker than current did not ask to be rehashed")
	}

	// The upgrade the caller performs, and the state afterwards.
	upgraded, err := Hash(password)
	if err != nil {
		t.Fatalf("rehash: %v", err)
	}
	after, err := Verify(upgraded, password)
	if err != nil {
		t.Fatalf("Verify after rehash: %v", err)
	}
	if !after.Match || after.NeedsRehash {
		t.Errorf("after rehashing: match=%v needsRehash=%v, want true/false",
			after.Match, after.NeedsRehash)
	}
}

// A hash STRONGER than current must not be flagged for rehash.
//
// Otherwise a deploy that lowers parameters silently weakens every password
// that logs in afterwards — a downgrade nobody chose, applied one user at a
// time, invisible in any diff.
func TestStrongerHashesAreNotDowngraded(t *testing.T) {
	const password = "a real password"

	strong := Current
	strong.Memory = Current.Memory * 2
	strong.Time = Current.Time + 1

	encoded, err := HashWith(password, strong)
	if err != nil {
		t.Fatalf("HashWith: %v", err)
	}

	result, err := Verify(encoded, password)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !result.Match {
		t.Fatal("a stronger hash did not verify")
	}
	if result.NeedsRehash {
		t.Error("a hash STRONGER than current was flagged for rehash; that is a downgrade")
	}
}

// PLAN/09: never log passwords, even failed attempts. Errors are the easiest
// place for one to escape, because an error message is written to be helpful.
func TestNoErrorContainsThePassword(t *testing.T) {
	const password = "SuperSecret!Passphrase#12345"

	var errs []error

	if _, err := Hash(strings.Repeat("x", maxPasswordBytes+1)); err != nil {
		errs = append(errs, err)
	}
	if _, err := Hash(""); err != nil {
		errs = append(errs, err)
	}
	if _, err := Verify("$argon2id$broken", password); err != nil {
		errs = append(errs, err)
	}
	if _, err := Verify("", password); err != nil {
		errs = append(errs, err)
	}
	if _, err := NeedsRehash("garbage"); err != nil {
		errs = append(errs, err)
	}

	if len(errs) < 4 {
		t.Fatalf("expected several errors to inspect, got %d — this test is not exercising anything", len(errs))
	}

	for _, err := range errs {
		if strings.Contains(err.Error(), password) {
			t.Errorf("an error message contains the password: %q", err.Error())
		}
		// Also catch a prefix, which is how a "helpful" error leaks slowly.
		if strings.Contains(err.Error(), password[:8]) {
			t.Errorf("an error message contains a prefix of the password: %q", err.Error())
		}
	}
}

func TestHashRejectsEmptyAndOversizedPasswords(t *testing.T) {
	if _, err := Hash(""); err != ErrEmptyPassword {
		t.Errorf("Hash(\"\") = %v, want ErrEmptyPassword", err)
	}

	if _, err := Hash(strings.Repeat("a", maxPasswordBytes+1)); err != ErrPasswordTooLong {
		t.Errorf("Hash(oversized) = %v, want ErrPasswordTooLong", err)
	}

	// Exactly at the limit is allowed: an off-by-one here locks out a user
	// with a long passphrase for no reason.
	if _, err := Hash(strings.Repeat("a", maxPasswordBytes)); err != nil {
		t.Errorf("a password of exactly %d bytes was rejected: %v", maxPasswordBytes, err)
	}
}

// SECURITY/02 §12, Enumeration. If a login for an unregistered address returns
// faster than one for a registered address, the response time is an oracle for
// which addresses have accounts — a password-reset list, a phishing list, and
// confirmation that a person works somewhere.
//
// The tolerance is wide on purpose. This runs on shared CI hardware, and a
// tight bound would fail for reasons that have nothing to do with the code.
// What it catches is the failure that matters: an early return, which is
// faster by three orders of magnitude rather than by a few per cent.
func TestNonexistentUserCostsTheSameAsARealOne(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test; skipped under -short")
	}

	const password = "a plausible password"

	encoded, err := Hash(password)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	// Warm up, so the first measurement does not carry one-off costs.
	for i := 0; i < 2; i++ {
		_, _ = Verify(encoded, password)
		_ = VerifyDummy(password)
	}

	const runs = 5

	real := medianDuration(runs, func() {
		_, _ = Verify(encoded, "a wrong password")
	})
	dummy := medianDuration(runs, func() {
		_ = VerifyDummy("a wrong password")
	})

	ratio := float64(dummy) / float64(real)
	const lower, upper = 0.5, 2.0

	t.Logf("real user: %v, nonexistent user: %v, ratio %.2f", real, dummy, ratio)

	if ratio < lower || ratio > upper {
		t.Errorf("verifying a nonexistent user took %v against %v for a real one "+
			"(ratio %.2f, want between %.1f and %.1f).\n"+
			"A large difference means the not-found path is skipping the hash, "+
			"which makes response time an oracle for which addresses are "+
			"registered (SECURITY/02 §12).",
			dummy, real, ratio, lower, upper)
	}
}

func medianDuration(runs int, fn func()) time.Duration {
	samples := make([]time.Duration, runs)
	for i := range samples {
		start := time.Now()
		fn()
		samples[i] = time.Since(start)
	}

	// Median rather than mean: one scheduler hiccup should not decide the
	// result.
	for i := 1; i < len(samples); i++ {
		for j := i; j > 0 && samples[j] < samples[j-1]; j-- {
			samples[j], samples[j-1] = samples[j-1], samples[j]
		}
	}
	return samples[len(samples)/2]
}

// P1-01 step 6: the parameter choice is a measured decision, and can be
// re-measured on new hardware.
//
//	go test -bench=BenchmarkHash -benchtime=10x ./internal/authn/
func BenchmarkHash(b *testing.B) {
	const password = "a representative password"

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Hash(password); err != nil {
			b.Fatalf("Hash: %v", err)
		}
	}
}

func BenchmarkVerify(b *testing.B) {
	const password = "a representative password"

	encoded, err := Hash(password)
	if err != nil {
		b.Fatalf("Hash: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Verify(encoded, password); err != nil {
			b.Fatalf("Verify: %v", err)
		}
	}
}
