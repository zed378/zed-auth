package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeSecretFile(t *testing.T, contents string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatalf("write secret fixture: %v", err)
	}
	// WriteFile applies the umask, so set the mode explicitly.
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod secret fixture: %v", err)
	}
	return path
}

func TestSecretResolver_File(t *testing.T) {
	r := NewSecretResolver(true)
	path := writeSecretFile(t, "s3cr3t-value", 0o600)

	got, err := r.Resolve(SecretRef("file:" + path))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if string(got) != "s3cr3t-value" {
		t.Errorf("value = %q, want %q", got, "s3cr3t-value")
	}
}

// The classic secret-file bug: an editor appends a newline, the value stops
// matching, and it surfaces as an authentication failure far from its cause.
func TestSecretResolver_TrimsTrailingNewlines(t *testing.T) {
	r := NewSecretResolver(true)

	for _, suffix := range []string{"\n", "\r\n", "\n\n", "\r\n\r\n"} {
		path := writeSecretFile(t, "value"+suffix, 0o600)

		got, err := r.Resolve(SecretRef("file:" + path))
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if string(got) != "value" {
			t.Errorf("trailing %q not trimmed: got %q", suffix, got)
		}
	}
}

// The most common way a secret leaks on a self-managed host is not an attacker
// reading /etc — it is a file created with the shell's default umask and then
// read by an unrelated service running as another user (ADR-011).
func TestSecretResolver_RefusesBroadFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows; deployment is Linux")
	}

	r := NewSecretResolver(true)

	for _, mode := range []os.FileMode{0o644, 0o640, 0o604, 0o666, 0o777} {
		path := writeSecretFile(t, "value", mode)

		_, err := r.Resolve(SecretRef("file:" + path))
		if err == nil {
			t.Errorf("mode %04o was accepted; a group- or world-readable secret must be refused", mode)
			continue
		}
		if !errors.Is(err, ErrSecretPermissions) {
			t.Errorf("mode %04o: want ErrSecretPermissions, got %v", mode, err)
		}
	}

	for _, mode := range []os.FileMode{0o600, 0o400} {
		path := writeSecretFile(t, "value", mode)
		if _, err := r.Resolve(SecretRef("file:" + path)); err != nil {
			t.Errorf("mode %04o must be accepted, got %v", mode, err)
		}
	}
}

// Local development uses a checked-out repository where enforcing 0600 on every
// fixture is friction without benefit.
func TestSecretResolver_PermissionCheckIsSkippableForLocalDevelopment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}

	lenient := NewSecretResolver(false)
	path := writeSecretFile(t, "value", 0o644)

	if _, err := lenient.Resolve(SecretRef("file:" + path)); err != nil {
		t.Errorf("lenient resolver must accept a 0644 file locally, got %v", err)
	}
}

func TestSecretResolver_Env(t *testing.T) {
	t.Setenv("AUTH_TEST_SECRET_VALUE", "from-env")

	r := NewSecretResolver(true)
	got, err := r.Resolve("env:AUTH_TEST_SECRET_VALUE")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if string(got) != "from-env" {
		t.Errorf("value = %q, want %q", got, "from-env")
	}
}

// An empty secret must be an error, not an empty value. A service that starts
// with an empty signing key is worse than one that refuses to start.
func TestSecretResolver_RejectsEmptySecrets(t *testing.T) {
	r := NewSecretResolver(true)

	t.Run("empty file", func(t *testing.T) {
		path := writeSecretFile(t, "", 0o600)
		_, err := r.Resolve(SecretRef("file:" + path))
		if !errors.Is(err, ErrSecretEmpty) {
			t.Errorf("want ErrSecretEmpty, got %v", err)
		}
	})

	t.Run("file containing only a newline", func(t *testing.T) {
		path := writeSecretFile(t, "\n", 0o600)
		_, err := r.Resolve(SecretRef("file:" + path))
		if !errors.Is(err, ErrSecretEmpty) {
			t.Errorf("want ErrSecretEmpty, got %v", err)
		}
	})

	t.Run("unset environment variable", func(t *testing.T) {
		_, err := r.Resolve("env:AUTH_DEFINITELY_UNSET_VARIABLE")
		if !errors.Is(err, ErrSecretNotFound) {
			t.Errorf("want ErrSecretNotFound, got %v", err)
		}
	})
}

func TestSecretResolver_MissingFile(t *testing.T) {
	r := NewSecretResolver(true)

	_, err := r.Resolve("file:/nonexistent/path/to/secret")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("want ErrSecretNotFound, got %v", err)
	}
}

// A stub that silently returned empty bytes would let a misconfigured
// deployment start with no signing key. Planned schemes must fail loudly and
// say they are planned, so a reader can tell a gap from a typo.
func TestSecretResolver_PlannedSchemesFailLoudly(t *testing.T) {
	r := NewSecretResolver(true)

	for _, ref := range []SecretRef{
		"vault:secret/data/zed-auth/jwt#private_key",
		"awssm:arn:aws:secretsmanager:eu-west-1:123:secret:jwt",
		"gcpsm:projects/p/secrets/jwt/versions/latest",
		"azurekv:https://vault.vault.azure.net/secrets/jwt",
	} {
		_, err := r.Resolve(ref)
		if !errors.Is(err, ErrUnsupportedScheme) {
			t.Errorf("%s: want ErrUnsupportedScheme, got %v", ref, err)
		}
		if !strings.Contains(err.Error(), "not implemented") {
			t.Errorf("%s: the error should say the scheme is planned but unimplemented, got: %v", ref, err)
		}
	}
}

func TestSecretResolver_MalformedReferences(t *testing.T) {
	r := NewSecretResolver(true)

	for _, ref := range []SecretRef{"", "   ", "no-scheme-here", "ftp:/tmp/x"} {
		if _, err := r.Resolve(ref); err == nil {
			t.Errorf("malformed reference %q was accepted", ref)
		}
	}
}

// A reference is not itself a secret, but a file path discloses host layout and
// an env var name discloses what the deployment holds. Neither belongs in a log
// shipped off the host (docs/SECURITY/02 §12).
func TestSecretRef_StringDoesNotDiscloseTheReference(t *testing.T) {
	ref := SecretRef("file:/etc/zed-auth/secrets/jwt-signing-key.pem")

	s := ref.String()
	if strings.Contains(s, "/etc/zed-auth") || strings.Contains(s, "jwt-signing-key") {
		t.Errorf("SecretRef.String() disclosed the path: %s", s)
	}
	if !strings.Contains(s, "file") {
		t.Errorf("SecretRef.String() should still identify the scheme for debugging, got: %s", s)
	}
}

// The database has the same check as a constraint on
// signing_keys.private_key_ref (P0-07). This catches it one layer earlier,
// where the error can explain itself.
func TestIsSecretMaterial(t *testing.T) {
	// Built by concatenation rather than written literally, so this repository
	// contains no PEM private key block anywhere. That keeps the pre-commit
	// hook and gitleaks honest: any literal block that ever appears is a real
	// finding rather than a known exception someone has to remember to
	// allowlist.
	material := []string{
		pemFixture("RSA PRIVATE KEY"),
		pemFixture("EC PRIVATE KEY"),
		pemFixture("PRIVATE KEY"),
		pemFixture("OPENSSH PRIVATE KEY"),
		pemFixture("CERTIFICATE"),
	}
	for _, s := range material {
		if !IsSecretMaterial(s) {
			t.Errorf("key material was not detected: %.40s...", s)
		}
	}

	references := []string{
		"file:/etc/zed-auth/secrets/jwt-signing-key.pem",
		"vault:secret/data/zed-auth/jwt#private_key",
		"env:AUTH_JWT_SIGNING_KEY",
		"arn:aws:secretsmanager:eu-west-1:123:secret:jwt-AbCdEf",
	}
	for _, s := range references {
		if IsSecretMaterial(s) {
			t.Errorf("reference %q was misdetected as key material", s)
		}
	}
}

// pemFixture assembles a PEM armour block at runtime. See the comment in
// TestIsSecretMaterial for why these are not string literals.
func pemFixture(kind string) string {
	dashes := strings.Repeat("-", 5)
	return dashes + "BEGIN " + kind + dashes + "\nQUJDREVG\n" + dashes + "END " + kind + dashes
}
