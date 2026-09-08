#!/usr/bin/env node
/**
 * Verifies the public site's light-mode brand tokens still match the console's.
 *
 * `PLAN/20` and `UI-UX/20` allow these two surfaces to share the visual
 * language and nothing else — no codebase, no component library, no pipeline.
 * So the values are duplicated on purpose. Duplication on purpose and
 * duplication by accident look identical six months later, and the failure is
 * quiet: two slightly different blues that nobody notices until a visitor
 * moves from the site into the console and something feels off.
 *
 * `UI-UX/20` § Cross-Page Requirements asks for visual continuity in exactly
 * that transition. This is what keeps it true.
 *
 * Only the LIGHT theme is compared. The console has no dark mode, and the
 * site's dark values are necessarily different — the console's palette is
 * tuned for light backgrounds and measures 2.4:1 to 3.3:1 on a dark one. Those
 * are checked by check-contrast.mjs instead.
 */

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const SITE_CSS = resolve(here, "../src/css/custom.css");
const CONSOLE_CSS = resolve(here, "../../console/src/styles/tokens.css");

/** Tokens that must be identical in both places. */
const SHARED = [
  "color-accent",
  "color-danger",
  "color-warning",
  "color-success",
  "color-text-primary",
  "color-text-secondary",
  "color-bg-base",
  "color-bg-surface",
  "color-border",
];

/**
 * Reads a token from the first block of a stylesheet.
 *
 * `slice` to the first `[data-theme="dark"]` so the site's dark overrides are
 * never mistaken for its light values — without it this script would compare
 * the console's light palette against whichever definition happened to come
 * last.
 */
function readTokens(path) {
  const css = readFileSync(path, "utf8");
  const light = css.split('[data-theme="dark"]')[0];

  const tokens = {};
  for (const name of SHARED) {
    const match = light.match(new RegExp(`^\\s*--${name}:\\s*([^;]+);`, "m"));
    if (match) tokens[name] = match[1].trim();
  }
  return tokens;
}

const site = readTokens(SITE_CSS);
const console_ = readTokens(CONSOLE_CSS);

const problems = [];

for (const name of SHARED) {
  if (!console_[name]) {
    problems.push(`--${name} is not defined in the console's tokens.css`);
    continue;
  }
  if (!site[name]) {
    problems.push(`--${name} is not defined in the public site's custom.css`);
    continue;
  }
  if (site[name] !== console_[name]) {
    problems.push(
      `--${name} has drifted: site ${site[name]}, console ${console_[name]}`,
    );
  }
}

if (problems.length > 0) {
  console.error("Brand tokens have drifted from the console:\n");
  for (const problem of problems) console.error(`  ${problem}`);
  console.error(
    "\nThese two surfaces share the visual language deliberately " +
      "(PLAN/20, UI-UX/20 § Cross-Page Requirements). If the change is " +
      "intended, make it in both places in the same commit.",
  );
  process.exit(1);
}

console.log(`${SHARED.length} brand tokens match the console.`);
