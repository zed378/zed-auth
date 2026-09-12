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

console.log(`patterns: wrote ${found.length} pattern(s) to Go and TypeScript`);
