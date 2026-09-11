package verify

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// verifySignature checks the RS256 signature over header.payload.
//
// A separate file because it is the one part a reader should be able to check
// without reading anything else: the signed input is the first two segments
// joined by a dot, exactly as they arrived — **not re-encoded**.
//
// Re-encoding is the subtle bug. A verifier that decodes the payload, marshals
// it again and hashes the result is checking a signature over a string the
// issuer never signed: JSON key order, whitespace and escaping all differ, and
// the only reason it ever appears to work is that the implementation happens
// to round-trip. `P1-03` found the same property from the other side — sixteen
// distinct token strings can decode to identical claims — which is why reuse
// detection there keys on the token rather than on what it says.
func verifySignature(key *rsa.PublicKey, parts []string) error {
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return fmt.Errorf("%w: the signature is not base64url", ErrInvalid)
	}

	signed := parts[0] + "." + parts[1]
	digest := sha256.Sum256([]byte(signed))

	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		return fmt.Errorf("%w: the signature does not check out", ErrInvalid)
	}
	return nil
}
