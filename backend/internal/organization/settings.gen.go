// Code generated from openapi/openapi.yaml by console/scripts/gen-patterns.mjs.
// DO NOT EDIT — change the pattern in the spec and run `npm run patterns:generate`.
// scripts/check.sh fails if this file has drifted from the spec.

package organization

// SpecDefaults are the defaults the OpenAPI contract publishes.
//
// NOT the values the service enforces — `authn.DefaultPolicy` and
// `authn.DefaultLoginPolicy` are. This exists so
// `TestSpecDefaultsMatchTheService` can fail when the contract and the
// service stop agreeing, which is otherwise a silent lie to every consumer
// that reads the published schema.
var SpecDefaults = struct {
	MinLength            int
	RequireUppercase     bool
	MaxAgeDays           int
	MFARequired          bool
	SessionLifetimeHours int
	AllowedLoginMethods  []string
}{
	MinLength:            12,
	RequireUppercase:     true,
	MaxAgeDays:           90,
	MFARequired:          false,
	SessionLifetimeHours: 12,
	AllowedLoginMethods:  []string{"password"},
}
