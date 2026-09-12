// Code generated from openapi/openapi.yaml by console/scripts/gen-patterns.mjs.
// DO NOT EDIT — change the pattern in the spec and run `npm run patterns:generate`.
// scripts/check.sh fails if this file has drifted from the spec.

package role

import "regexp"

// PermissionKeyPattern is a permission a role carries, in resource:action form.
//
// Generated from the OpenAPI `PermissionKey` schema, which carries the
// authoritative definition and the reasoning behind it.
const PermissionKeyPattern = "^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*:[a-z][a-z0-9_]*$"

// MaxPermissionKeyLength bounds the value, from the same schema.
const MaxPermissionKeyLength = 128

var permissionKey = regexp.MustCompile(PermissionKeyPattern)

// RoleKeyPattern is a role's stable identifier, unique within its project.
//
// Generated from the OpenAPI `RoleKey` schema, which carries the
// authoritative definition and the reasoning behind it.
const RoleKeyPattern = "^[a-z0-9][a-z0-9_-]{0,62}$"

// MaxRoleKeyLength bounds the value, from the same schema.
const MaxRoleKeyLength = 63

var roleKey = regexp.MustCompile(RoleKeyPattern)
