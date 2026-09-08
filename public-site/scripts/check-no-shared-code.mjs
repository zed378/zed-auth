#!/usr/bin/env node
/**
 * Verifies the public site shares no code with the console.
 *
 * `PLAN/20` § Why a Separate Surface and `UI-UX/20` both require these two to
 * be separate projects: different audience, different auth, different SEO
 * requirements, different update cadence. "Sharing a codebase between the two
 * would force compromises in both directions."
 *
 * The visual language IS shared, as values — `check-brand-tokens.mjs` keeps
 * those in step. Everything else must not be.
 *
 * This is a rule that erodes by convenience rather than by decision. Nobody
 * ever proposes coupling the two; someone imports one useful component,
 * because it is right there and it works, and the boundary is gone. So it is
 * checked rather than trusted.
 */

import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, extname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const SITE_ROOT = resolve(here, "..");

const SEARCHABLE = new Set([".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".css", ".json"]);
const SKIP_DIRS = new Set(["node_modules", "build", ".docusaurus", ".git"]);

/**
 * Patterns that mean the site is reaching into the console.
 *
 * A relative path climbing out of `public-site/` into `console/`, or a package
 * import of the console's workspace name.
 */
const FORBIDDEN = [
  { pattern: /\.\.[/\\](?:\.\.[/\\])*console[/\\]/, what: "a relative import from console/" },
  { pattern: /@zed-auth\/console/, what: "a package import of the console" },
];

/**
 * `check-brand-tokens.mjs` reads the console's stylesheet on purpose — that is
 * the mechanism keeping the shared values in step, and it is a build-time
 * check rather than shipped code.
 */
const EXEMPT = new Set(["scripts/check-brand-tokens.mjs", "scripts/check-no-shared-code.mjs"]);

function* walk(dir) {
  for (const entry of readdirSync(dir)) {
    if (SKIP_DIRS.has(entry)) continue;

    const full = join(dir, entry);
    if (statSync(full).isDirectory()) {
      yield* walk(full);
    } else if (SEARCHABLE.has(extname(entry))) {
      yield full;
    }
  }
}

const problems = [];
let scanned = 0;

for (const file of walk(SITE_ROOT)) {
  const relative = file.slice(SITE_ROOT.length + 1).replace(/\\/g, "/");
  if (EXEMPT.has(relative)) continue;

  scanned++;
  const contents = readFileSync(file, "utf8");

  for (const { pattern, what } of FORBIDDEN) {
    if (pattern.test(contents)) {
      problems.push(`${relative}: ${what}`);
    }
  }
}

if (problems.length > 0) {
  console.error("The public site is reaching into the console:\n");
  for (const problem of problems) console.error(`  ${problem}`);
  console.error(
    "\nThese are deliberately separate projects (PLAN/20 § Why a Separate " +
      "Surface). The visual language is shared as duplicated token VALUES, " +
      "checked by check-brand-tokens.mjs — code is not shared at all.",
  );
  process.exit(1);
}

console.log(`${scanned} files scanned; no code shared with the console.`);
