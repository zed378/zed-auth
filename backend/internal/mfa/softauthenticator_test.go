package mfa

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

// A software authenticator (P3-05).
//
// Everything this task claims — that a relayed assertion from a lookalike
// origin fails, that a challenge cannot be replayed, that a regressed counter
// is caught — is a claim about what happens when a REAL signature arrives with
// one thing wrong. None of it can be tested with a fake verifier, because the
// fake would be the thing deciding.
//
// So this produces genuine ES256 assertions that the library verifies, and each
// test breaks exactly one property. When a test here goes red it is because the
// control caught something, not because a stub returned an error.
//
// It implements only what the ceremony needs: ES256 (the algorithm every
// authenticator supports), "none" attestation, and no extensions.

type softAuthenticator struct {
	key *ecdsa.PrivateKey

	// aaguid identifies the authenticator MODEL. Zero for "none" attestation,
	// which is what a platform authenticator reports.
	aaguid [16]byte

	credentialID []byte

	// signCount is what the authenticator reports and increments. A value of
	// zero that never moves is what most platform authenticators do, and the
	// verifier has to accept that without accepting a replay.
	signCount uint32

	// userVerified is the UV flag: a PIN, a fingerprint or a face, rather than
	// mere presence.
	userVerified bool
}

func newSoftAuthenticator(t *testing.T) *softAuthenticator {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating an authenticator key: %v", err)
	}

	id := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		t.Fatalf("generating a credential id: %v", err)
	}

	return &softAuthenticator{
		key:          key,
		credentialID: id,
		signCount:    1,
		userVerified: true,
	}
}

// coseKey encodes the public key the way an authenticator does.
//
// COSE_Key for ES256, per RFC 8152: kty=EC2(2), alg=ES256(-7), crv=P-256(1),
// then the two coordinates. The map keys are negative integers, which is why
// this needs a CBOR encoder rather than JSON.
func (a *softAuthenticator) coseKey(t *testing.T) []byte {
	t.Helper()

	x := make([]byte, 32)
	y := make([]byte, 32)
	a.key.PublicKey.X.FillBytes(x)
	a.key.PublicKey.Y.FillBytes(y)

	encoder, err := cbor.CanonicalEncOptions().EncMode()
	if err != nil {
		t.Fatalf("building a CBOR encoder: %v", err)
	}

	raw, err := encoder.Marshal(map[int]any{
		1:  2,  // kty: EC2
		3:  -7, // alg: ES256
		-1: 1,  // crv: P-256
		-2: x,
		-3: y,
	})
	if err != nil {
		t.Fatalf("encoding a COSE key: %v", err)
	}
	return raw
}

// flags builds the authenticator-data flag byte.
//
// UP (0x01) is user presence — somebody touched it. UV (0x04) is user
// verification — the authenticator checked WHO. AT (0x40) says attested
// credential data follows, which is only true during registration.
func (a *softAuthenticator) flags(attested bool) byte {
	var f byte = 0x01 // UP
	if a.userVerified {
		f |= 0x04 // UV
	}
	if attested {
		f |= 0x40 // AT
	}
	return f
}

// authenticatorData assembles the signed-over structure.
func (a *softAuthenticator) authenticatorData(t *testing.T, rpID string, attested bool) []byte {
	t.Helper()

	rpHash := sha256.Sum256([]byte(rpID))

	data := make([]byte, 0, 128)
	data = append(data, rpHash[:]...)
	data = append(data, a.flags(attested))

	counter := make([]byte, 4)
	binary.BigEndian.PutUint32(counter, a.signCount)
	data = append(data, counter...)

	if attested {
		data = append(data, a.aaguid[:]...)

		idLen := make([]byte, 2)
		binary.BigEndian.PutUint16(idLen, uint16(len(a.credentialID)))
		data = append(data, idLen...)
		data = append(data, a.credentialID...)
		data = append(data, a.coseKey(t)...)
	}

	return data
}

// clientData is what the BROWSER assembles, and the origin in it is the one the
// browser was actually talking to.
//
// That single field is the phishing defence: a lookalike page produces its own
// origin here, the authenticator signs over it faithfully, and the relying
// party sees an origin it never configured.
func clientData(t *testing.T, ceremonyType, challenge, origin string) []byte {
	t.Helper()

	raw, err := json.Marshal(map[string]any{
		"type":        ceremonyType,
		"challenge":   challenge,
		"origin":      origin,
		"crossOrigin": false,
	})
	if err != nil {
		t.Fatalf("encoding client data: %v", err)
	}
	return raw
}

// sign produces the ECDSA signature over authenticatorData || SHA256(clientData).
func (a *softAuthenticator) sign(t *testing.T, authData, clientDataJSON []byte) []byte {
	t.Helper()

	clientHash := sha256.Sum256(clientDataJSON)
	signed := append(append([]byte{}, authData...), clientHash[:]...)
	digest := sha256.Sum256(signed)

	signature, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	return signature
}

// --- registration -------------------------------------------------------------

// register produces a credential-creation response for the given challenge.
func (a *softAuthenticator) register(t *testing.T, rpID, origin, challenge string) []byte {
	t.Helper()

	authData := a.authenticatorData(t, rpID, true)
	clientDataJSON := clientData(t, "webauthn.create", challenge, origin)

	encoder, err := cbor.CanonicalEncOptions().EncMode()
	if err != nil {
		t.Fatalf("building a CBOR encoder: %v", err)
	}

	// "none" attestation: the authenticator makes no claim about what it is.
	// Every platform authenticator does this by default and it is what a
	// relying party that does not check a metadata service should expect.
	attestation, err := encoder.Marshal(map[string]any{
		"fmt":      "none",
		"attStmt":  map[string]any{},
		"authData": authData,
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

// --- authentication ------------------------------------------------------------

// assert produces a credential-assertion response.
//
// The origin is a PARAMETER so a test can produce a genuinely-signed assertion
// for a lookalike page — which is the only way to prove the origin check does
// anything.
func (a *softAuthenticator) assert(t *testing.T, rpID, origin, challenge string) []byte {
	t.Helper()

	authData := a.authenticatorData(t, rpID, false)
	clientDataJSON := clientData(t, "webauthn.get", challenge, origin)
	signature := a.sign(t, authData, clientDataJSON)

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

// challengeFrom pulls the challenge out of the options the ceremony issued, so
// a test signs the real one rather than one it made up.
func challengeFrom(t *testing.T, options []byte) string {
	t.Helper()

	var parsed struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(options, &parsed); err != nil {
		t.Fatalf("reading the ceremony options: %v\n%s", err, options)
	}
	if parsed.PublicKey.Challenge == "" {
		t.Fatalf("the options carry no challenge:\n%s", options)
	}
	return parsed.PublicKey.Challenge
}
