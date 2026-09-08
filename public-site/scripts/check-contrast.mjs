#!/usr/bin/env node
/**
 * Checks every colour token against WCAG 2.1 AA, in both themes.
 *
 * `UI-UX/13` applies to this site as much as to the console, and `UI-UX/20`
 * says so explicitly: "a public marketing site failing basic accessibility is
 * both an exclusion problem and, in many jurisdictions, a compliance risk".
 *
 * The dark theme is why this exists as a script rather than a comment. The
 * console's palette is tuned for light backgrounds; carried over unchanged it
 * measured accent 2.4:1, danger 2.7:1, warning 3.3:1 and success 2.8:1 against
 * the dark surface — a dark mode that looks deliberate and is unreadable,
 * which is worse than not offering one. Every one of those was found by
 * measuring rather than by looking.
 *
 * Ratios are computed from custom.css, so changing a value here runs the check
 * against the new value rather than against this file's assumptions.
 */

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const CSS = readFileSync(resolve(here, "../src/css/custom.css"), "utf8");

/** WCAG 2.1 AA: normal text. */
const TEXT_MIN = 4.5;
/** WCAG 2.1 AA (1.4.11): the boundary of a UI component. */
const NON_TEXT_MIN = 3;

const [lightBlock, darkBlock = ""] = CSS.split('[data-theme="dark"]');

function tokensFor(theme) {
  // The dark block overrides a subset; anything it does not redefine is
  // inherited from :root. Modelling that here rather than assuming the dark
  // block is complete is the difference between checking the real cascade and
  // checking a list.
  const base = parse(lightBlock);
  if (theme === "light") return base;
  return { ...base, ...parse(darkBlock.split("}")[0]) };
}

function parse(block) {
  const out = {};
  for (const match of block.matchAll(/^\s*--(color-[a-z-]+):\s*(#[0-9a-fA-F]{3,8})\s*;/gm)) {
    out[match[1]] = match[2];
  }
  return out;
}

function luminance(hex) {
  const h = hex.replace("#", "");
  const full = h.length === 3 ? [...h].map((c) => c + c).join("") : h;
  const [r, g, b] = [0, 2, 4].map((i) => {
    const c = parseInt(full.slice(i, i + 2), 16) / 255;
    return c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function ratio(a, b) {
  const [x, y] = [luminance(a), luminance(b)];
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
}

const TEXT_TOKENS = [
  "color-text-primary",
  "color-text-secondary",
  "color-accent",
  "color-danger",
  "color-warning",
  "color-success",
];

const problems = [];
let checked = 0;

for (const theme of ["light", "dark"]) {
  const tokens = tokensFor(theme);
  const backgrounds = ["color-bg-base", "color-bg-surface"];

  for (const bg of backgrounds) {
    if (!tokens[bg]) {
      problems.push(`${theme}: --${bg} is not defined`);
      continue;
    }

    for (const name of TEXT_TOKENS) {
      if (!tokens[name]) {
        problems.push(`${theme}: --${name} is not defined`);
        continue;
      }
      checked++;
      const r = ratio(tokens[name], tokens[bg]);
      if (r < TEXT_MIN) {
        problems.push(
          `${theme}: --${name} (${tokens[name]}) on --${bg} (${tokens[bg]}) ` +
            `is ${r.toFixed(2)}:1, below AA's ${TEXT_MIN}:1`,
        );
      }
    }

    checked++;
    const borderRatio = ratio(tokens["color-border"], tokens[bg]);
    if (borderRatio < NON_TEXT_MIN) {
      problems.push(
        `${theme}: --color-border (${tokens["color-border"]}) on --${bg} ` +
          `is ${borderRatio.toFixed(2)}:1, below 1.4.11's ${NON_TEXT_MIN}:1`,
      );
    }
  }
}

if (problems.length > 0) {
  console.error("Contrast failures (UI-UX/13 targets WCAG 2.1 AA):\n");
  for (const problem of problems) console.error(`  ${problem}`);
  process.exit(1);
}

console.log(`${checked} contrast checks passed across both themes.`);
