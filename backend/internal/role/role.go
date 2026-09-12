// Package role implements the project-scoped role model (P2-01).
//
// A role is a named bundle of permission keys belonging to exactly one
// project. The scoping is the point rather than an implementation detail:
// `docs/PLAN/08` Part A opens with it, and the failure it prevents is the one
// nobody notices until it is an incident — an `admin` role created for the
// internal tools project quietly granting `admin` in the billing project,
// because both happen to be called `admin`.
//
// Nothing consumes roles yet. `P2-03` grants them to users, `P2-04` puts them
// in the access token, `P2-06` answers questions about them. This package is
// the vocabulary those three depend on.
//
// # Where the rules live
//
// Validation here is pure and has no database. The same rules exist again as
// constraints and triggers in `20260912000022_role_rules`, and that duplication
// is deliberate: this layer gives the caller a field-level error it can act on,
// and the database layer holds against writers this package does not mediate —
// a migration, a support script, a future service. `P1-20` found `events`
// writable on staging while the code that was supposed to prevent it read
// perfectly, which is the argument for both.
//
// The two layers are kept honest by `TestTheDatabaseAgreesWithTheValidator`,
// which feeds one table of cases to each.
package role

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Role is a bundle of permission keys inside one project.
type Role struct {
	ID             string
	ProjectID      string
	Key            string
	DisplayName    string
	PermissionKeys []string
	IsBuiltin      bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// MaxDisplayNameLength bounds the human-readable label.
const MaxDisplayNameLength = 128

// MaxPermissionKeys bounds how many a single role may carry.
//
// A role with thousands of permission keys is not a role: it is a denial of
// service against every token that embeds it (`P2-04`) and every decision that
// scans it (`P2-06`). The same bound is a CHECK constraint, so the limit holds
// even for a writer that never comes through this package.
const MaxPermissionKeys = 256

// ReservedKeys are role keys that would shadow something else.
//
// These are the five `manager_roles` values in lower case. A manager role
// governs who may administer the Auth Service itself; a role here governs
// access inside a consumer application (`docs/PLAN/08` Part C is explicit that
// they are different things). A project role keyed `org_admin` would appear in
// a token beside a manager role of the same name meaning something entirely
// different, and the consumer reading it has no way to tell which it got.
//
// `admin` is deliberately NOT reserved. It is the single most natural name a
// consumer application will reach for, and reserving it to prevent a confusion
// nobody has yet would trade a real need for a theoretical one.
var ReservedKeys = map[string]struct{}{
	"instance_owner":      {},
	"org_owner":           {},
	"org_admin":           {},
	"project_owner":       {},
	"project_grant_owner": {},
}

// FieldError names the field that was wrong, so the API can answer with
// `docs/PLAN/05`'s per-field error shape rather than one opaque sentence.
type FieldError struct {
	Field  string
	Detail string
}

func (e FieldError) Error() string { return e.Field + ": " + e.Detail }

// ValidateKey checks a role key.
func ValidateKey(key string) error {
	switch {
	case key == "":
		return FieldError{"key", "A role key is required."}
	case len(key) > MaxRoleKeyLength:
		return FieldError{"key", fmt.Sprintf("A role key may be at most %d characters.", MaxRoleKeyLength)}
	case !roleKey.MatchString(key):
		return FieldError{"key", "A role key may contain only lower-case letters, digits, underscores and hyphens, and must start with a letter or digit."}
	}
	if _, reserved := ReservedKeys[key]; reserved {
		// Named explicitly. "That key is reserved" with no list is a message
		// that sends somebody to the source code.
		return FieldError{"key", fmt.Sprintf(
			"%q is reserved: it names an administrative role of this service, which is a different thing from a role inside your application.", key)}
	}
	return nil
}

// ValidateDisplayName checks the label, after trimming.
func ValidateDisplayName(name string) error {
	trimmed := strings.TrimSpace(name)
	switch {
	case trimmed == "":
		return FieldError{"display_name", "A display name is required."}
	case len(trimmed) > MaxDisplayNameLength:
		return FieldError{"display_name", fmt.Sprintf("A display name may be at most %d characters.", MaxDisplayNameLength)}
	}
	return nil
}

// ValidatePermissionKey checks one permission key.
func ValidatePermissionKey(key string) error {
	switch {
	case key == "":
		return errors.New("a permission key must not be empty")
	case len(key) > MaxPermissionKeyLength:
		return fmt.Errorf("a permission key may be at most %d characters", MaxPermissionKeyLength)
	case strings.Contains(key, "*"):
		// Called out separately from the general pattern failure, because it
		// is the mistake somebody makes on purpose and deserves the reason
		// rather than a restatement of the regex.
		return errors.New("a permission key may not contain a wildcard: matching rules belong in the authorization decision, not in stored data, or every consumer reimplements them differently")
	case !permissionKey.MatchString(key):
		return errors.New("a permission key must be resource:action, lower-case, with an optionally dotted resource — for example user:read or billing.invoice:write")
	}
	return nil
}

// ValidatePermissionKeys checks the whole set, reporting the first problem by
// index so the caller can point at the offending entry rather than the field.
func ValidatePermissionKeys(keys []string) error {
	if len(keys) > MaxPermissionKeys {
		return FieldError{"permission_keys", fmt.Sprintf("A role may carry at most %d permission keys.", MaxPermissionKeys)}
	}

	seen := make(map[string]int, len(keys))
	for i, k := range keys {
		if err := ValidatePermissionKey(k); err != nil {
			return FieldError{fmt.Sprintf("permission_keys[%d]", i), capitalise(err.Error()) + "."}
		}
		if first, duplicate := seen[k]; duplicate {
			// Refused rather than de-duplicated. Silently changing what was
			// sent means the response does not match the request, and the
			// caller learns about it from a later read.
			return FieldError{fmt.Sprintf("permission_keys[%d]", i), fmt.Sprintf(
				"%q is already listed at index %d.", k, first)}
		}
		seen[k] = i
	}
	return nil
}

// Validate checks everything a caller supplies about a role.
func Validate(key, displayName string, permissionKeys []string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	if err := ValidateDisplayName(displayName); err != nil {
		return err
	}
	return ValidatePermissionKeys(permissionKeys)
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
