package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// SecretRef is a URI naming where a secret lives, not the secret itself.
//
// The indirection exists because the deployment target changes and the code
// should not (ADR-011). Today secrets are files on a self-managed VM; the plan
// is Kubernetes with Vault or a cloud secret manager. Both are the same
// operation — resolve a reference to bytes — so the scheme changes and nothing
// else does.
//
// It also keeps secret *values* out of places references are safe to appear:
// signing_keys.private_key_ref stores one of these (docs/PLAN/04), and the column
// has a CHECK constraint refusing anything that looks like key material.
//
// Supported today:
//
//	file:/etc/zed-auth/secrets/jwt-signing-key.pem
//	env:AUTH_SOME_SECRET
//
// Planned, and deliberately unimplemented rather than stubbed:
//
//	vault:secret/data/zed-auth/jwt#private_key
//	awssm:arn:aws:secretsmanager:...
//
// An unimplemented scheme returns ErrUnsupportedScheme naming what is
// supported. A stub that silently returned empty bytes would let a
// misconfigured deployment start with no signing key.
type SecretRef string

// Common secret resolution failures.
var (
	ErrUnsupportedScheme = errors.New("unsupported secret reference scheme")
	ErrSecretNotFound    = errors.New("secret not found")
	ErrSecretPermissions = errors.New("secret file permissions are too broad")
	ErrSecretEmpty       = errors.New("secret is empty")
)

// SecretResolver turns a SecretRef into its value.
type SecretResolver interface {
	Resolve(ref SecretRef) ([]byte, error)
}

// NewSecretResolver returns the resolver for this deployment.
//
// strictPermissions should be true everywhere except local development. It
// makes the resolver refuse a secret file that is group- or world-readable,
// which is the single most common way a secret leaks on a self-managed host:
// not an attacker reading /etc, but a file created with the shell's default
// umask and then read by an unrelated service running as another user.
func NewSecretResolver(strictPermissions bool) SecretResolver {
	return &defaultResolver{strictPermissions: strictPermissions}
}

type defaultResolver struct{ strictPermissions bool }

func (r *defaultResolver) Resolve(ref SecretRef) ([]byte, error) {
	raw := strings.TrimSpace(string(ref))
	if raw == "" {
		return nil, fmt.Errorf("%w: reference is empty", ErrSecretNotFound)
	}

	scheme, rest, found := strings.Cut(raw, ":")
	if !found {
		return nil, fmt.Errorf("%w: %q has no scheme; expected one of file:, env:",
			ErrUnsupportedScheme, raw)
	}

	switch strings.ToLower(scheme) {
	case "file":
		return r.resolveFile(rest)
	case "env":
		return r.resolveEnv(rest)
	case "vault", "awssm", "gcpsm", "azurekv":
		// Named explicitly so the error says "not implemented yet" rather than
		// "unknown scheme" — the difference tells a reader whether they made a
		// typo or hit a gap.
		return nil, fmt.Errorf("%w: %q is planned for the Kubernetes migration but is not implemented (ADR-011)",
			ErrUnsupportedScheme, scheme)
	default:
		return nil, fmt.Errorf("%w: %q; expected one of file:, env:", ErrUnsupportedScheme, scheme)
	}
}

func (r *defaultResolver) resolveFile(path string) ([]byte, error) {
	path = filepath.Clean(strings.TrimPrefix(path, "//"))

	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrSecretNotFound, path)
		}
		return nil, fmt.Errorf("stat secret file %s: %w", path, err)
	}

	if info.IsDir() {
		return nil, fmt.Errorf("%w: %s is a directory", ErrSecretNotFound, path)
	}

	if r.strictPermissions {
		// Windows does not carry POSIX permission bits, so the check would
		// always pass there and give false assurance. Deployment is Linux
		// (ADR-011); this only skips the check on a developer machine.
		if runtime.GOOS != "windows" {
			if perm := info.Mode().Perm(); perm&0o077 != 0 {
				return nil, fmt.Errorf("%w: %s is %04o, must be 0600 or 0400 — "+
					"a group- or world-readable secret is readable by every other service on this host",
					ErrSecretPermissions, path, perm)
			}
		}
	}

	// #nosec G304 -- the path comes from this deployment's own configuration,
	// which is exactly what a secret reference is for. Treating operator-set
	// configuration as untrusted input would make every configurable path
	// unreachable.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read secret file %s: %w", path, err)
	}

	// Trailing newlines are the classic secret-file bug: an editor adds one,
	// the value no longer matches, and the error surfaces as an authentication
	// failure a long way from its cause.
	data = trimTrailingNewline(data)

	if len(data) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrSecretEmpty, path)
	}
	return data, nil
}

func (r *defaultResolver) resolveEnv(name string) ([]byte, error) {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return nil, fmt.Errorf("%w: environment variable %s is unset or empty", ErrSecretNotFound, name)
	}
	return []byte(v), nil
}

func trimTrailingNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

// Redacted implements fmt.Stringer so a SecretRef can be logged safely.
//
// A reference is not itself a secret — that is the point of the indirection —
// but a file path still discloses host layout, and an env var name discloses
// what the deployment holds. Neither belongs in a log shipped off the host
// (docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md §12).
func (s SecretRef) String() string {
	raw := string(s)
	scheme, _, found := strings.Cut(raw, ":")
	if !found {
		return "SecretRef(malformed)"
	}
	return "SecretRef(" + scheme + ":…)"
}

// IsSecretMaterial reports whether a string looks like a private key or
// certificate rather than a reference to one.
//
// Used at startup to refuse a configuration that pastes key material where a
// reference belongs. The database has the same check as a constraint on
// signing_keys.private_key_ref (P0-07); this catches it one layer earlier,
// with an error message that can explain itself.
func IsSecretMaterial(s string) bool {
	upper := strings.ToUpper(s)
	for _, marker := range []string{
		"BEGIN RSA PRIVATE KEY",
		"BEGIN EC PRIVATE KEY",
		"BEGIN PRIVATE KEY",
		"BEGIN ENCRYPTED PRIVATE KEY",
		"BEGIN OPENSSH PRIVATE KEY",
		"BEGIN PGP PRIVATE KEY",
		"BEGIN CERTIFICATE",
	} {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}
