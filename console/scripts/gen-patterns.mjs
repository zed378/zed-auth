// Generates the shared validation patterns from the OpenAPI spec (P2-01).
//
// `docs/PLAN/08` Part A defines permission keys, and `P2-01` step 2 requires
// the format to be "documented and shared with the console's form validation".
// Shared, for a regular expression, usually means copied — and a copied regex
// is one that diverges the first time somebody widens one side to accept a
// customer's role name.
//
// So there is one definition, in `openapi/openapi.yaml`, which is already the
// contract's source of truth (ADR-013). This writes the Go and TypeScript
// forms of it. `scripts/check.sh` regenerates and diffs, the same discipline
// the generated server interface and API client already live under: a
// reviewer sees the contract change and its consequences in one diff.
//
//   npm run patterns:generate
//
// It lives under `console/` because that is where `js-yaml` is installed, not
// because it is a console concern.

import { readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import yaml from "js-yaml";

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, "..", "..");

const spec = yaml.load(readFileSync(resolve(repo, "openapi/openapi.yaml"), "utf8"));
const schemas = spec?.components?.schemas ?? {};

/** The schemas whose `pattern` both surfaces must agree on. */
const SHARED = [
  {
    schema: "PermissionKey",
    go: "PermissionKeyPattern",
    ts: "PERMISSION_KEY_PATTERN",
    what: "a permission a role carries, in resource:action form",
  },
  {
    schema: "RoleKey",
    go: "RoleKeyPattern",
    ts: "ROLE_KEY_PATTERN",
    what: "a role's stable identifier, unique within its project",
  },
];

const found = SHARED.map((entry) => {
  const schema = schemas[entry.schema];
  if (!schema) {
    throw new Error(`openapi.yaml has no components.schemas.${entry.schema}`);
  }
  if (typeof schema.pattern !== "string" || schema.pattern.length === 0) {
    throw new Error(`components.schemas.${entry.schema} has no pattern`);
  }
  // A pattern that is not a valid regex here would be a pattern the console
  // silently never applies.
  new RegExp(schema.pattern);
  return { ...entry, pattern: schema.pattern, min: schema.minLength, max: schema.maxLength };
});

const banner = (comment) =>
  [
    `${comment} Code generated from openapi/openapi.yaml by console/scripts/gen-patterns.mjs.`,
    `${comment} DO NOT EDIT — change the pattern in the spec and run \`npm run patterns:generate\`.`,
    `${comment} scripts/check.sh fails if this file has drifted from the spec.`,
  ].join("\n");

// --- Go --------------------------------------------------------------------

const goLines = [
  banner("//"),
  "",
  "package role",
  "",
  'import "regexp"',
  "",
];

for (const e of found) {
  goLines.push(
    `// ${e.go} is ${e.what}.`,
    "//",
    `// Generated from the OpenAPI \`${e.schema}\` schema, which carries the`,
    "// authoritative definition and the reasoning behind it.",
    `const ${e.go} = ${JSON.stringify(e.pattern)}`,
    "",
    `// Max${e.go.replace("Pattern", "Length")} bounds the value, from the same schema.`,
    `const Max${e.go.replace("Pattern", "Length")} = ${e.max}`,
    "",
    `var ${e.go[0].toLowerCase()}${e.go.slice(1, -7)} = regexp.MustCompile(${e.go})`,
    "",
  );
}

writeFileSync(resolve(repo, "backend/internal/role/pattern.gen.go"), goLines.join("\n"), "utf8");

// --- TypeScript ------------------------------------------------------------

const tsLines = [banner("//"), ""];

for (const e of found) {
  tsLines.push(
    `/** ${e.what[0].toUpperCase()}${e.what.slice(1)}. */`,
    `export const ${e.ts} = ${JSON.stringify(e.pattern)};`,
    "",
    `/** Maximum length, from the same schema. */`,
    `export const ${e.ts.replace("_PATTERN", "_MAX_LENGTH")} = ${e.max};`,
    "",
  );
}

tsLines.push(
  "/** Compiled once. A new RegExp per keystroke is a form that gets slower as it gets longer. */",
  "const compiled = {",
  ...found.map((e) => `  ${e.ts}: new RegExp(${e.ts}),`),
  "} as const;",
  "",
  ...found.flatMap((e) => [
    `export function isValid${e.schema}(value: string): boolean {`,
    `  return value.length <= ${e.ts.replace("_PATTERN", "_MAX_LENGTH")} && compiled.${e.ts}.test(value);`,
    "}",
    "",
  ]),
);

writeFileSync(resolve(repo, "console/src/lib/api/patterns.gen.ts"), tsLines.join("\n"), "utf8");

// --- organization settings: bounds and defaults ----------------------------
//
// `P2-14`'s Policies screen has to validate against the server's own ranges
// and show an administrator what they are changing FROM — including for a
// setting their organization has never touched, where the value in force is
// the service default.
//
// Both come out of the same schema for the same reason the patterns do: a
// console restating `min_length >= 12` is a console that keeps saying 12 after
// the floor moves, and a console restating the defaults is a third copy of a
// number that already exists twice (the column default and `authn`).

const settings = schemas.OrganizationSettings?.properties;
if (!settings) {
  throw new Error("openapi.yaml has no components.schemas.OrganizationSettings.properties");
}

const password = settings.password_policy?.properties;
if (!password) {
  throw new Error("OrganizationSettings has no password_policy.properties");
}

/** Reads one numeric setting's range and default, failing loudly if any is absent. */
function numeric(name, schema) {
  for (const key of ["minimum", "maximum", "default"]) {
    if (typeof schema?.[key] !== "number") {
      throw new Error(`OrganizationSettings.${name} has no numeric ${key}`);
    }
  }
  return { min: schema.minimum, max: schema.maximum, fallback: schema.default };
}

const bounds = {
  min_length: numeric("password_policy.min_length", password.min_length),
  max_age_days: numeric("password_policy.max_age_days", password.max_age_days),
  session_lifetime_hours: numeric("session_lifetime_hours", settings.session_lifetime_hours),
};

if (typeof password.require_uppercase?.default !== "boolean") {
  throw new Error("OrganizationSettings.password_policy.require_uppercase has no default");
}
if (typeof settings.mfa_required?.default !== "boolean") {
  throw new Error("OrganizationSettings.mfa_required has no default");
}
const methods = settings.allowed_login_methods?.items?.enum;
const methodDefault = settings.allowed_login_methods?.default;
if (!Array.isArray(methods) || methods.length === 0) {
  throw new Error("OrganizationSettings.allowed_login_methods has no item enum");
}
if (!Array.isArray(methodDefault) || methodDefault.length === 0) {
  throw new Error("OrganizationSettings.allowed_login_methods has no default");
}

const settingsTs = [
  banner("//"),
  "",
  "/**",
  " * The ranges the service enforces on an organization's settings.",
  " *",
  " * Generated from the OpenAPI `OrganizationSettings` schema, so a value the",
  " * form accepts is a value the server accepts.",
  " */",
  "export const SETTINGS_BOUNDS = {",
  ...Object.entries(bounds).map(([name, b]) => `  ${name}: { min: ${b.min}, max: ${b.max} },`),
  "} as const;",
  "",
  "/**",
  " * What the service applies when an organization's settings say nothing.",
  " *",
  " * Shown beside each field so an administrator can see what they are changing",
  " * FROM — including for a setting their organization has never touched, which",
  " * has no stored value at all.",
  " */",
  "export const SETTINGS_DEFAULTS = {",
  `  min_length: ${bounds.min_length.fallback},`,
  `  require_uppercase: ${password.require_uppercase.default},`,
  `  max_age_days: ${bounds.max_age_days.fallback},`,
  `  mfa_required: ${settings.mfa_required.default},`,
  `  session_lifetime_hours: ${bounds.session_lifetime_hours.fallback},`,
  `  allowed_login_methods: ${JSON.stringify(methodDefault)},`,
  "} as const;",
  "",
  "/** Every login method the service implements today. */",
  `export const LOGIN_METHODS = ${JSON.stringify(methods)} as const;`,
  "",
];

writeFileSync(resolve(repo, "console/src/lib/api/settings.gen.ts"), settingsTs.join("\n"), "utf8");

// The same values in Go, as a DRIFT GATE rather than a second source of truth.
//
// `authn.DefaultPolicy` and `authn.DefaultLoginPolicy` remain what the service
// enforces. This file exists so a test can assert they still match what the
// contract tells the world — the direction that matters, because the contract
// is what the console and every consumer read.
const settingsGo = [
  banner("//"),
  "",
  "package organization",
  "",
  "// SpecDefaults are the defaults the OpenAPI contract publishes.",
  "//",
  "// NOT the values the service enforces — `authn.DefaultPolicy` and",
  "// `authn.DefaultLoginPolicy` are. This exists so",
  "// `TestSpecDefaultsMatchTheService` can fail when the contract and the",
  "// service stop agreeing, which is otherwise a silent lie to every consumer",
  "// that reads the published schema.",
  "var SpecDefaults = struct {",
  "\tMinLength            int",
  "\tRequireUppercase     bool",
  "\tMaxAgeDays           int",
  "\tMFARequired          bool",
  "\tSessionLifetimeHours int",
  "\tAllowedLoginMethods  []string",
  "}{",
  `\tMinLength:            ${bounds.min_length.fallback},`,
  `\tRequireUppercase:     ${password.require_uppercase.default},`,
  `\tMaxAgeDays:           ${bounds.max_age_days.fallback},`,
  `\tMFARequired:          ${settings.mfa_required.default},`,
  `\tSessionLifetimeHours: ${bounds.session_lifetime_hours.fallback},`,
  `\tAllowedLoginMethods:  []string{${methodDefault.map((m) => JSON.stringify(m)).join(", ")}},`,
  "}",
  "",
];

writeFileSync(
  resolve(repo, "backend/internal/organization/settings.gen.go"),
  settingsGo.join("\n"),
  "utf8",
);

console.log(
  `patterns: wrote ${found.length} pattern(s) and the settings bounds/defaults to Go and TypeScript`,
);
