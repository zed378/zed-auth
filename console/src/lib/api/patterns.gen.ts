// Code generated from openapi/openapi.yaml by console/scripts/gen-patterns.mjs.
// DO NOT EDIT — change the pattern in the spec and run `npm run patterns:generate`.
// scripts/check.sh fails if this file has drifted from the spec.

/** A permission a role carries, in resource:action form. */
export const PERMISSION_KEY_PATTERN = "^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*:[a-z][a-z0-9_]*$";

/** Maximum length, from the same schema. */
export const PERMISSION_KEY_MAX_LENGTH = 128;

/** A role's stable identifier, unique within its project. */
export const ROLE_KEY_PATTERN = "^[a-z0-9][a-z0-9_-]{0,62}$";

/** Maximum length, from the same schema. */
export const ROLE_KEY_MAX_LENGTH = 63;

/** Compiled once. A new RegExp per keystroke is a form that gets slower as it gets longer. */
const compiled = {
  PERMISSION_KEY_PATTERN: new RegExp(PERMISSION_KEY_PATTERN),
  ROLE_KEY_PATTERN: new RegExp(ROLE_KEY_PATTERN),
} as const;

export function isValidPermissionKey(value: string): boolean {
  return value.length <= PERMISSION_KEY_MAX_LENGTH && compiled.PERMISSION_KEY_PATTERN.test(value);
}

export function isValidRoleKey(value: string): boolean {
  return value.length <= ROLE_KEY_MAX_LENGTH && compiled.ROLE_KEY_PATTERN.test(value);
}
