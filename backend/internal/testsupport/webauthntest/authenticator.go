// Package webauthntest is a software WebAuthn authenticator for tests outside
// the mfa package (P3-10).
//
// The registration half of `internal/mfa/softauthenticator_test.go`, which a
// `_test.go` file cannot share. It produces a genuinely signed ES256 credential
// with "none" attestation — what a platform authenticator sends — so a hosted
// page's registration can be driven end to end against the real relying party
// library rather than a fake that accepts anything.
//
// Test code only: nothing in the service imports it, and an architecture test
// would be the place to enforce that if anything ever did.
package webauthntest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

// credentialIDLength is the size of every credential id this authenticator
// makes. A constant rather than len() of the slice, so the length written into
// the authenticator data needs no int-to-uint16 conversion.
const credentialIDLength = 32

// Authenticator holds one credential's key.
type Authenticator struct {
	key          *ecdsa.PrivateKey
	credentialID []byte
	signCount    uint32
}

// New creates an authenticator with a fresh P-256 key and credential id.
func New(t testing.TB) *Authenticator {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating an authenticator key: %v", err)
	}
	id := make([]byte, credentialIDLength)
	if _, err := rand.Read(id); err != nil {
		t.Fatalf("generating a credential id: %v", err)
	}
	return &Authenticator{key: key, credentialID: id}
}

// Register produces the JSON a browser posts after navigator.credentials.create.
//
// The origin is a parameter so a test can show a registration made on a
// lookalike origin is refused.
func (a *Authenticator) Register(t testing.TB, rpID, origin, challenge string) []byte {
	t.Helper()

	encoder, err := cbor.CanonicalEncOptions().EncMode()
	if err != nil {
		t.Fatalf("building a CBOR encoder: %v", err)
	}

	x := make([]byte, 32)
	y := make([]byte, 32)
	a.key.PublicKey.X.FillBytes(x)
	a.key.PublicKey.Y.FillBytes(y)
	cose, err := encoder.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: x, -3: y})
	if err != nil {
		t.Fatalf("encoding a COSE key: %v", err)
	}

	rpHash := sha256.Sum256([]byte(rpID))
	authData := append([]byte{}, rpHash[:]...)
	authData = append(authData, 0x01|0x04|0x40) // UP, UV, AT
	counter := make([]byte, 4)
	binary.BigEndian.PutUint32(counter, 1)
	authData = append(authData, counter...)
	authData = append(authData, make([]byte, 16)...) // AAGUID: none
	idLen := make([]byte, 2)
	binary.BigEndian.PutUint16(idLen, credentialIDLength)
	authData = append(authData, idLen...)
	authData = append(authData, a.credentialID...)
	authData = append(authData, cose...)

	clientDataJSON, err := json.Marshal(map[string]any{
		"type": "webauthn.create", "challenge": challenge, "origin": origin, "crossOrigin": false,
	})
	if err != nil {
		t.Fatalf("encoding client data: %v", err)
	}

	attestation, err := encoder.Marshal(map[string]any{
		"fmt": "none", "attStmt": map[string]any{}, "authData": authData,
	})
	if err != nil {
		t.Fatalf("encoding an attestation object: %v", err)
	}

	response, err := json.Marshal(map[string]any{
		"id":    base64.RawURLEncoding.EncodeToString(a.credentialID),
		"rawId": base64.RawURLEncoding.EncodeToString(a.credentialID),
		"type":  "public-key",
		"response": map[string]any{
			"attestationObject": base64.RawURLEncoding.EncodeToString(attestation),
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientDataJSON),
		},
	})
	if err != nil {
		t.Fatalf("encoding a registration response: %v", err)
	}
	return response
}

// ChallengeFrom reads the challenge out of creation options.
func ChallengeFrom(t testing.TB, options []byte) string {
	t.Helper()
	var parsed struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(options, &parsed); err != nil || parsed.PublicKey.Challenge == "" {
		t.Fatalf("the options carry no challenge (%v):\n%s", err, options)
	}
	return parsed.PublicKey.Challenge
}

// Assert produces the JSON a browser posts after navigator.credentials.get
// (P3-14), signed by this authenticator's key over the given challenge.
//
// Added so a sign-in with a passkey can be driven through the real login
// handler and relying party. Until then the only assertions outside the mfa
// package came from fake ceremonies that accepted anything, so nothing above
// the verifier had ever checked a real signature on the login path.
//
// The signature counter advances on every call, as a real authenticator's
// does; the relying party treats a counter that fails to advance as a possible
// clone.
func (a *Authenticator) Assert(t testing.TB, rpID, origin, challenge string) []byte {
	t.Helper()

	a.signCount++
	rpHash := sha256.Sum256([]byte(rpID))
	authData := append([]byte{}, rpHash[:]...)
	authData = append(authData, 0x01|0x04) // UP, UV
	counter := make([]byte, 4)
	binary.BigEndian.PutUint32(counter, a.signCount+1) // registration used 1
	authData = append(authData, counter...)

	clientDataJSON, err := json.Marshal(map[string]any{
		"type": "webauthn.get", "challenge": challenge, "origin": origin, "crossOrigin": false,
	})
	if err != nil {
		t.Fatalf("encoding client data: %v", err)
	}

	clientHash := sha256.Sum256(clientDataJSON)
	signed := sha256.Sum256(append(append([]byte{}, authData...), clientHash[:]...))
	signature, err := ecdsa.SignASN1(rand.Reader, a.key, signed[:])
	if err != nil {
		t.Fatalf("signing an assertion: %v", err)
	}

	response, err := json.Marshal(map[string]any{
		"id":    base64.RawURLEncoding.EncodeToString(a.credentialID),
		"rawId": base64.RawURLEncoding.EncodeToString(a.credentialID),
		"type":  "public-key",
		"response": map[string]any{
			"authenticatorData": base64.RawURLEncoding.EncodeToString(authData),
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientDataJSON),
			"signature":         base64.RawURLEncoding.EncodeToString(signature),
			"userHandle":        "",
		},
	})
	if err != nil {
		t.Fatalf("encoding an assertion: %v", err)
	}
	return response
}
