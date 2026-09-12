package mfa

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

// Encrypting a factor secret at rest (P3-02 step 1).
//
// `docs/PLAN/04` calls the column `secret_encrypted` and `docs/PLAN/09`
// § Transport & Storage requires it, and the reason is worth stating plainly:
// **a database read that yields TOTP secrets yields the second factor for
// every user at once.** That is the difference between a disclosure — bad — and
// a total compromise of the control the disclosure was supposed to be stopped
// by.
//
// The threat this closes is specific and does not include a compromised
// application host. A key the service can read is a key an attacker who owns
// the service can read. What it closes is everything that reaches the DATA
// without reaching the process: a stolen backup, a replica, a `pg_dump` in a
// ticket, a misconfigured read-only role, an operator with SQL access and no
// business reading secrets. Those are the realistic paths, and they are the
// ones `PG-25` (the owner role can rewrite the audit log) is about too.
//
// AES-256-GCM, key from the deployment's secret resolver. Authenticated, so a
// tampered ciphertext is an error rather than a secret that verifies nothing —
// which would present as "your authenticator app stopped working" and send
// somebody to their recovery codes.

// ErrNoSealKey is returned when encryption is not configured.
//
// **Refused rather than falling back to plaintext.** A service that stores
// secrets unencrypted because a key was missing is one whose security depends
// on nobody having made a configuration mistake — and the mistake is invisible,
// because everything keeps working.
var ErrNoSealKey = errors.New("mfa: no encryption key is configured, so a factor secret cannot be stored")

// ErrSealed is a ciphertext that will not open: wrong key, or tampered.
var ErrSealed = errors.New("mfa: a stored factor secret could not be decrypted")

// Sealer encrypts and decrypts factor secrets.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer derives a sealer from the deployment's key material.
//
// The key is HASHED into 32 bytes rather than required to be exactly 32, so an
// operator can supply a passphrase, a base64 blob, or a file of random bytes
// without the service refusing to start over a length. SHA-256 of the material
// is not a key-derivation function and is not pretending to be one: the input
// is expected to be high-entropy machine-generated material, the same
// assumption `deploy/vm/secrets.sh` already makes for every other secret it
// writes.
func NewSealer(keyMaterial []byte) (*Sealer, error) {
	// One check, not two. An earlier version tested for empty separately and
	// the mutation run showed why that was worth removing: the length check
	// already covers it, so the empty case could be deleted without a single
	// test noticing. A guard no test can distinguish is a guard nobody can
	// maintain.
	//
	// 16 bytes is the smallest thing anybody could call random; below that it
	// is a passphrase somebody typed, and a passphrase is not what this
	// expects — `deploy/vm/secrets.sh` generates machine material for every
	// other secret it writes and this is no different.
	if len(keyMaterial) < 16 {
		return nil, fmt.Errorf("%w: the key material is %d bytes, which is too short to be random",
			ErrNoSealKey, len(keyMaterial))
	}

	sum := sha256.Sum256(keyMaterial)
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, fmt.Errorf("mfa: building the cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("mfa: building the AEAD: %w", err)
	}
	return &Sealer{aead: aead}, nil
}

// Seal encrypts a secret for storage.
//
// The nonce is prepended to the ciphertext, which is the conventional layout
// and means the column holds one self-describing value rather than two columns
// somebody could get out of step.
func (s *Sealer) Seal(plaintext []byte) ([]byte, error) {
	if s == nil || s.aead == nil {
		return nil, ErrNoSealKey
	}
	if len(plaintext) == 0 {
		return nil, fmt.Errorf("mfa: refusing to seal an empty secret")
	}

	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("mfa: generating a nonce: %w", err)
	}

	// A fresh nonce per seal, never a counter. GCM's security collapses if a
	// nonce repeats under one key, and a counter shared across replicas of a
	// stateless service is a counter that repeats.
	return s.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Open decrypts a stored secret.
func (s *Sealer) Open(sealed []byte) ([]byte, error) {
	if s == nil || s.aead == nil {
		return nil, ErrNoSealKey
	}

	size := s.aead.NonceSize()
	if len(sealed) < size+1 {
		return nil, fmt.Errorf("%w: the stored value is %d bytes, shorter than a nonce",
			ErrSealed, len(sealed))
	}

	plaintext, err := s.aead.Open(nil, sealed[:size], sealed[size:], nil)
	if err != nil {
		// Deliberately not wrapping the cipher's own error. It says
		// "cipher: message authentication failed", which is correct and
		// unhelpful, and repeating it in a log invites somebody to conclude
		// the data is corrupt when the likelier cause is a rotated key.
		return nil, fmt.Errorf("%w: wrong key, or the value was altered", ErrSealed)
	}
	return plaintext, nil
}

// Configured reports whether secrets can be stored at all.
//
// Read at startup so a deployment that cannot encrypt fails to enable the
// factor rather than failing at the first enrolment — the difference between an
// operator seeing a problem and a user seeing one.
func (s *Sealer) Configured() bool { return s != nil && s.aead != nil }
