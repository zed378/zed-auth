package social

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestProviderValues(t *testing.T) {
	for _, p := range []Provider{Google, Microsoft, GitHub} {
		if !p.Valid() {
			t.Errorf("provider %q is invalid", p)
		}
	}
	for _, p := range []Provider{"", "Google", "github.com", "password", "saml"} {
		if p.Valid() {
			t.Errorf("unsupported provider %q is valid", p)
		}
		_, err := NewStore().BySubject(context.Background(), nil, p, "subject")
		if !errors.Is(err, ErrInvalidProvider) {
			t.Errorf("unsupported provider: got %v", err)
		}
	}
}

func TestSubjectValidation(t *testing.T) {
	for _, subject := range []string{"123", "Aa-_:.", "subject@example.test", " Subject ", strings.Repeat("a", MaxSubjectBytes)} {
		if !validSubject(subject) {
			t.Errorf("valid subject rejected: %q", subject)
		}
	}
	for _, subject := range []string{"", "a\x00b", string([]byte{0xff}), strings.Repeat("a", MaxSubjectBytes+1), strings.Repeat("é", 128)} {
		if validSubject(subject) {
			t.Errorf("invalid subject accepted: %q", subject)
		}
		_, err := NewStore().BySubject(context.Background(), nil, Google, subject)
		if !errors.Is(err, ErrInvalidSubject) {
			t.Errorf("invalid subject: got %v", err)
		}
	}
}
