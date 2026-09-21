package saml

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

// Signature wrapping, tested by actually doing it (P4-07 A-3).
//
// These tests sign a real assertion with a real key and then move it, because a
// hand-written "wrapped" document that was never validly signed proves nothing:
// it would be refused by the signature check alone, and the wrapping defence
// would never run. The attack is interesting precisely because the signature IS
// valid.

// keyStore is goxmldsig's X509KeyStore over a key and certificate we hold.
//
// Defined here rather than using dsig.MemoryX509KeyStore, whose fields are
// unexported and whose only constructor generates a 1024-bit key for its own
// tests.
type keyStore struct {
	key *rsa.PrivateKey
	der []byte
}

func (k keyStore) GetKeyPair() (*rsa.PrivateKey, []byte, error) { return k.key, k.der, nil }

// signer builds a throwaway certificate and a goxmldsig signing context.
func signer(t *testing.T) (dsig.X509KeyStore, []*x509.Certificate) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "saml-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating a certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parsing the certificate: %v", err)
	}
	return keyStore{key: key, der: der}, []*x509.Certificate{cert}
}

// assertion builds an Assertion carrying one claim about a subject.
func assertion(id, subject string) *etree.Element {
	el := etree.NewElement("Assertion")
	el.CreateAttr("xmlns", "urn:oasis:names:tc:SAML:2.0:assertion")
	el.CreateAttr("ID", id)
	el.CreateAttr("Version", "2.0")
	el.CreateAttr("IssueInstant", time.Now().UTC().Format(time.RFC3339))
	el.CreateElement("Issuer").SetText("https://auth.example.test")
	subj := el.CreateElement("Subject")
	subj.CreateElement("NameID").SetText(subject)
	return el
}

// sign returns the assertion with an enveloped signature over it.
func sign(t *testing.T, ks dsig.X509KeyStore, el *etree.Element) *etree.Element {
	t.Helper()
	ctx := dsig.NewDefaultSigningContext(ks)
	signed, err := ctx.SignEnveloped(el)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	return signed
}

func serialise(t *testing.T, el *etree.Element) []byte {
	t.Helper()
	doc := etree.NewDocument()
	doc.SetRoot(el.Copy())
	raw, err := doc.WriteToBytes()
	if err != nil {
		t.Fatalf("serialising: %v", err)
	}
	return raw
}

func TestAProperlySignedAssertionVerifies(t *testing.T) {
	ks, certs := signer(t)
	signed := sign(t, ks, assertion("_real", "budi@example.test"))

	v, err := Verify(serialise(t, signed), "Assertion", certs)
	if err != nil {
		t.Fatalf("a legitimately signed assertion was refused: %v", err)
	}
	if v.ID() != "_real" {
		t.Errorf("verified element has ID %q, want _real", v.ID())
	}
	if name := v.Element().FindElement("./Subject/NameID"); name == nil || name.Text() != "budi@example.test" {
		t.Error("the verified element is not the assertion that was signed")
	}
}

// A-3, the attack itself.
//
// The document root is a forged Assertion naming somebody else. The genuinely
// signed assertion is moved inside it, where a naive verifier finds a valid
// signature and a naive consumer reads the root.
func TestSignatureWrappingIsRefused(t *testing.T) {
	ks, certs := signer(t)
	genuine := sign(t, ks, assertion("_real", "budi@example.test"))

	forged := assertion("_forged", "attacker@evil.test")
	// The signed element, intact and still cryptographically valid, parked
	// somewhere the consumer does not look.
	decoy := forged.CreateElement("Extensions")
	decoy.AddChild(genuine.Copy())

	v, err := Verify(serialise(t, forged), "Assertion", certs)
	if err == nil {
		t.Fatalf("a wrapped document was accepted, yielding the assertion for %q",
			v.Element().FindElement("./Subject/NameID").Text())
	}
	if !errors.Is(err, ErrWrapped) && !errors.Is(err, ErrBadSignature) {
		t.Errorf("refused with %v — want ErrWrapped or ErrBadSignature", err)
	}

	// And if anything ever does come back, it must not be the forgery.
	if err == nil && v.ID() == "_forged" {
		t.Error("the FORGED assertion was returned as verified")
	}
}

// The wrapping attack that actually VALIDATES (A-3).
//
// This test exists because a mutation survived: deleting the ID comparison left
// TestSignatureWrappingIsRefused green. That document was being refused by the
// signature check — goxmldsig found no signature on the forged root at all — so
// the wrapping guard never ran, and a test that never runs the code it is named
// after proves nothing.
//
// The real attack keeps the signature verifiable. The genuine Signature is moved
// up to be a direct child of the forgery, and the element it references is
// parked where a consumer does not look. goxmldsig then succeeds: it finds the
// signature, resolves the reference, digests the genuine element, and returns
// it. Everything is cryptographically sound. The only thing wrong is that the
// element the signature covers is not the element at the root, and that is the
// one comparison standing between this document and an authenticated attacker.
func TestAValidSignatureOverADifferentElementIsRefused(t *testing.T) {
	ks, certs := signer(t)

	genuine := sign(t, ks, assertion("_real", "budi@example.test"))
	sig := genuine.FindElement("./Signature")
	if sig == nil {
		t.Fatal("the signing context produced no Signature element")
	}
	genuine.RemoveChild(sig)

	forged := assertion("_forged", "attacker@evil.test")
	forged.AddChild(sig)                                 // verifiable signature, attacker's root
	forged.CreateElement("Extensions").AddChild(genuine) // what it actually covers

	v, err := Verify(serialise(t, forged), "Assertion", certs)
	if err == nil {
		t.Fatalf("a wrapped document with a VALID signature was accepted as %q",
			v.Element().FindElement("./Subject/NameID").Text())
	}
	// Refused as a bad signature rather than as wrapping, and that is the
	// stronger outcome: because Verify hands goxmldsig the element it is about
	// to consume, a signature that references something else is not merely
	// noticed afterwards — it never validates in the first place.
	if !errors.Is(err, ErrBadSignature) && !errors.Is(err, ErrWrapped) {
		t.Errorf("refused with %v — want ErrBadSignature or ErrWrapped", err)
	}
}

// A valid signature over some OTHER element in the same document does not
// authenticate the assertion.
//
// The document carries a properly signed Issuer and an unsigned Assertion. A
// verifier that asked "does this document contain a valid signature?" would say
// yes and hand over the assertion.
func TestAValidSignatureElsewhereDoesNotAuthenticateTheAssertion(t *testing.T) {
	ks, certs := signer(t)

	issuer := etree.NewElement("Issuer")
	issuer.CreateAttr("ID", "_iss")
	issuer.SetText("https://auth.example.test")

	response := etree.NewElement("Response")
	response.CreateAttr("xmlns", "urn:oasis:names:tc:SAML:2.0:protocol")
	response.CreateAttr("ID", "_response")
	response.AddChild(sign(t, ks, issuer))
	response.AddChild(assertion("_unsigned", "attacker@evil.test"))

	if _, err := Verify(serialise(t, response), "Assertion", certs); !errors.Is(err, ErrBadSignature) {
		t.Errorf("an unsigned assertion beside a signed issuer gave %v, want ErrBadSignature", err)
	}
}

// A signed Assertion inside a Response, which is the ordinary SAML shape.
//
// The property under test is that Verify returns the element the signature
// COVERS, not the document it arrived in. A mutation returning the root instead
// survived every other test here, because in the legitimate case the root and
// the signed element carry the same content — this is the case where they do
// not.
func TestTheVerifiedElementIsTheSignedOneNotTheEnvelope(t *testing.T) {
	ks, certs := signer(t)
	genuine := sign(t, ks, assertion("_real", "budi@example.test"))

	response := etree.NewElement("Response")
	response.CreateAttr("xmlns", "urn:oasis:names:tc:SAML:2.0:protocol")
	response.CreateAttr("ID", "_response")
	response.CreateElement("Issuer").SetText("https://auth.example.test")
	response.AddChild(genuine)

	v, err := Verify(serialise(t, response), "Assertion", certs)
	if err != nil {
		t.Fatalf("a signed assertion inside a response was refused: %v", err)
	}
	if v.ID() != "_real" {
		t.Errorf("returned the element with ID %q, want _real — the envelope is not what was signed", v.ID())
	}
	if v.Element().Tag != "Assertion" {
		t.Errorf("returned a %q, want the Assertion", v.Element().Tag)
	}
	if name := v.Element().FindElement("./Subject/NameID"); name == nil || name.Text() != "budi@example.test" {
		t.Error("the returned element is not the signed assertion")
	}
}

// A decoy assertion prepended to a document with a genuine one is refused, not
// skipped.
//
// Returning the genuine assertion here would not be exploitable through this
// package's API — Verify hands back only the element it validated, so nothing
// downstream can read the decoy. The loud failure is still the right behaviour:
// a document carrying an assertion nobody signed is not a document to serve
// half of, and an operator should hear about it.
//
// This also pins the selection rule. "Take the first" fails closed; "take
// whichever happens to verify" is the wrapping bug written as a loop, and it is
// the shape that becomes exploitable the moment any caller sees the document.
func TestADecoyAssertionAheadOfTheGenuineOneIsRefused(t *testing.T) {
	ks, certs := signer(t)

	response := etree.NewElement("Response")
	response.CreateAttr("xmlns", "urn:oasis:names:tc:SAML:2.0:protocol")
	response.CreateAttr("ID", "_response")
	response.AddChild(assertion("_forged", "attacker@evil.test")) // first, unsigned
	response.AddChild(sign(t, ks, assertion("_real", "budi@example.test")))

	v, err := Verify(serialise(t, response), "Assertion", certs)
	if err == nil {
		t.Fatalf("a response with a decoy assertion was served, returning %q", v.ID())
	}
	if !errors.Is(err, ErrBadSignature) {
		t.Errorf("refused with %v, want ErrBadSignature", err)
	}
}

// The subtler shape: the signature is valid and covers an element of a
// different kind. A signature over the Issuer does not authenticate an
// Assertion.
func TestASignatureOverADifferentElementIsRefused(t *testing.T) {
	ks, certs := signer(t)

	issuer := etree.NewElement("Issuer")
	issuer.CreateAttr("ID", "_iss")
	issuer.SetText("https://auth.example.test")
	signedIssuer := sign(t, ks, issuer)

	// There is no Assertion in this document at all, so there is nothing to
	// consume — refused before any signature is considered.
	if _, err := Verify(serialise(t, signedIssuer), "Assertion", certs); err == nil {
		t.Error("a signed Issuer was accepted when an Assertion was asked for")
	}
}

func TestAnUnsignedDocumentIsRefused(t *testing.T) {
	_, certs := signer(t)

	_, err := Verify(serialise(t, assertion("_plain", "budi@example.test")), "Assertion", certs)
	if !errors.Is(err, ErrNoSignature) {
		t.Errorf("an unsigned assertion gave %v, want ErrNoSignature", err)
	}
}

func TestASignatureFromAnotherKeyIsRefused(t *testing.T) {
	ks, _ := signer(t)
	_, otherCerts := signer(t) // a different authority entirely

	signed := sign(t, ks, assertion("_real", "budi@example.test"))

	if _, err := Verify(serialise(t, signed), "Assertion", otherCerts); !errors.Is(err, ErrBadSignature) {
		t.Errorf("an assertion signed by an unknown key gave %v, want ErrBadSignature", err)
	}
}

// Verifying against nothing verifies nothing. The dangerous default would be to
// treat an empty trust store as "accept".
func TestVerificationWithNoCertificateIsRefused(t *testing.T) {
	ks, _ := signer(t)
	signed := sign(t, ks, assertion("_real", "budi@example.test"))

	if _, err := Verify(serialise(t, signed), "Assertion", nil); !errors.Is(err, ErrBadSignature) {
		t.Errorf("verification with no trust anchor gave %v, want a refusal", err)
	}
}

// A tampered assertion must fail even though its signature block is present and
// well-formed — this is the check that the signature covers the CONTENT.
func TestATamperedAssertionIsRefused(t *testing.T) {
	ks, certs := signer(t)
	signed := sign(t, ks, assertion("_real", "budi@example.test"))

	if name := signed.FindElement("./Subject/NameID"); name != nil {
		name.SetText("attacker@evil.test")
	}

	if _, err := Verify(serialise(t, signed), "Assertion", certs); !errors.Is(err, ErrBadSignature) {
		t.Errorf("an assertion edited after signing gave %v, want ErrBadSignature", err)
	}
}

// The bounds from xmlsafe.go apply here too: Verify reads the document through
// ReadDocument, so a hostile document is refused before any signature work.
func TestVerifyAppliesTheDocumentBounds(t *testing.T) {
	_, certs := signer(t)
	const xxe = `<?xml version="1.0"?>
<!DOCTYPE foo [ <!ENTITY xxe SYSTEM "file:///etc/passwd"> ]>
<Assertion xmlns="urn:oasis:names:tc:SAML:2.0:assertion" ID="_x">&xxe;</Assertion>`

	if _, err := Verify([]byte(xxe), "Assertion", certs); err == nil {
		t.Fatal("Verify accepted an XXE document — the bounds are not in its path")
	}
}
