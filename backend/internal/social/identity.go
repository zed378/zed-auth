// Package social resolves federated identities to existing local accounts.
// Provider callbacks must verify their provider's response before using Store.
package social

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

type Provider string

const (
	Google    Provider = "google"
	Microsoft Provider = "microsoft"
	GitHub    Provider = "github"

	MaxSubjectBytes = 255
)

func (p Provider) Valid() bool {
	return p == Google || p == Microsoft || p == GitHub
}

var (
	ErrInvalidProvider = errors.New("social: unsupported provider")
	ErrInvalidSubject  = errors.New("social: invalid provider subject")
	ErrNotFound        = errors.New("social: identity not found")
)

// Identity contains no provider credentials or email address. UserID is the
// local account; its status, organization policy and MFA still govern sign-in.
type Identity struct {
	ID       string
	UserID   string
	Provider Provider
	Subject  string
	LinkedAt time.Time
}

func validSubject(subject string) bool {
	return len(subject) > 0 && len(subject) <= MaxSubjectBytes &&
		utf8.ValidString(subject) && !strings.ContainsRune(subject, '\x00')
}
