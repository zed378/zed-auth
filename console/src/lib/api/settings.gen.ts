// Code generated from openapi/openapi.yaml by console/scripts/gen-patterns.mjs.
// DO NOT EDIT — change the pattern in the spec and run `npm run patterns:generate`.
// scripts/check.sh fails if this file has drifted from the spec.

/**
 * The ranges the service enforces on an organization's settings.
 *
 * Generated from the OpenAPI `OrganizationSettings` schema, so a value the
 * form accepts is a value the server accepts.
 */
export const SETTINGS_BOUNDS = {
  min_length: { min: 12, max: 128 },
  max_age_days: { min: 0, max: 3650 },
  session_lifetime_hours: { min: 1, max: 720 },
} as const;

/**
 * What the service applies when an organization's settings say nothing.
 *
 * Shown beside each field so an administrator can see what they are changing
 * FROM — including for a setting their organization has never touched, which
 * has no stored value at all.
 */
export const SETTINGS_DEFAULTS = {
  min_length: 12,
  require_uppercase: true,
  max_age_days: 90,
  mfa_required: false,
  session_lifetime_hours: 12,
  allowed_login_methods: ["password"],
} as const;

/** Every login method the service implements today. */
export const LOGIN_METHODS = ["password"] as const;
