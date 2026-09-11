package signing

import (
	"encoding/base64"
	"strings"
	"testing"
)

// Fuzzing the token parser (`P1-27` step 5, `docs/PLAN/11` § Fuzz Testing).
//
// `Verify` is reached by anything that presents a bearer token, which is every
// authenticated request in the system, and its input is entirely attacker
// controlled. The properties below are the ones that must hold for EVERY
// input, not just the ones somebody thought to write down:
//
//	1. It never panics. A panic here is a denial of service reachable by
//	   anyone who can send a header, and `Recover` turning it into a 500
//	   is a mitigation rather than an answer.
//	2. It never accepts a token this service did not sign. There is exactly
//	   one way to get a payload out, and it goes through the key set.
//
// The corpus is seeded with the shapes that have historically broken JWT
// parsers, so the fuzzer starts from the interesting places rather than
// discovering base64 from scratch.

func FuzzVerify(f *testing.F) {
	cache := testCache(f, newKey(f, RS256, StatusCurrent))
	verifier := NewVerifier(cache)

	// A real token, so the corpus contains at least one input that reaches
	// every stage of the parser rather than failing at the first.
	valid, err := NewSigner(cache).Sign([]byte(`{"sub":"usr_1"}`))
	if err != nil {
		f.Fatalf("signing a seed token: %v", err)
	}
	f.Add(valid)

	segment := func(raw string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(raw))
	}

	for _, seed := range []string{
		"",
		".",
		"..",
		"...",
		"a.b.c",
		"a.b",
		"a.b.c.d",

		// `alg: none`, the oldest one.
		segment(`{"alg":"none","typ":"JWT"}`) + "." + segment(`{"sub":"x"}`) + ".",

		// Algorithm confusion: an RSA issuer asked to treat its public key as
		// an HMAC secret.
		segment(`{"alg":"HS256","typ":"JWT"}`) + "." + segment(`{"sub":"x"}`) + "." + segment("sig"),

		// A header that is valid base64 and not JSON.
		segment("not json") + "." + segment(`{"sub":"x"}`) + "." + segment("sig"),

		// Deeply nested JSON, which has exhausted stacks in other parsers.
		segment(`{"alg":"RS256","typ":"JWT"}`) + "." +
			segment(strings.Repeat(`{"a":`, 200)+`1`+strings.Repeat(`}`, 200)) + "." + segment("sig"),

		// Enormous, and enormous in one field.
		strings.Repeat("A", 100_000),
		segment(`{"alg":"RS256","kid":"`+strings.Repeat("k", 10_000)+`","typ":"JWT"}`) +
			".e30." + segment("sig"),

		// Characters a base64url decoder must refuse rather than tolerate.
		"+.+.+",
		"=.=.=",
		"\x00.\x00.\x00",
		"ü.ü.ü",

		// A JWS with two signatures. Legal JWS, meaningless as a JWT, and a
		// verifier that checks only the first accepts a token whose second
		// signature is the one a consumer validates.
		`{"payload":"e30","signatures":[{"protected":"e30","signature":"x"},{"protected":"e30","signature":"y"}]}`,
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, token string) {
		// Both types, because `wantType` steers a real branch and a fuzzer
		// exercising only one leaves the other unvisited.
		for _, want := range []string{TypeJWT, TypeAccessToken} {
			payload, err := verifier.Verify(token, want)

			if err != nil {
				// The only contract on the error path: nothing comes back with
				// it. A parser that returns a payload AND an error is one
				// whose caller will eventually use the payload.
				if payload != nil {
					t.Errorf("Verify(%q, %q) returned a payload with an error: %q", token, want, payload)
				}
				continue
			}

			// Accepting anything at all is only correct for the token this
			// test signed. Reaching here with any other input means a token
			// this service did not issue was verified.
			if token != valid {
				t.Fatalf("Verify accepted a token this service never signed: %q", token)
			}
		}
	})
}
