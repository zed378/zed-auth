#!/usr/bin/env node
/**
 * Fails if the built site contains material from documents that must never be
 * published.
 *
 * `PLAN/20` § What Never Gets Published names three:
 *
 *   SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md — attack scenarios and
 *     mitigation mechanics. Defensive documentation, not marketing content:
 *     publishing it hands an attacker the list of things that were considered
 *     and, by omission, the ones that were not.
 *   PLAN/14-DEPLOYMENT.md — infrastructure topology.
 *   PLAN/18-RISK-REGISTER.md — anything at all.
 *
 * `P0-19`'s Definition of Done restates it as a gate.
 *
 * **How this checks, and why that way.** It looks for verbatim phrases from
 * those documents rather than for keywords. Keywords produce false positives
 * on words a public site legitimately uses — "authorization", "session",
 * "token" — and a check that cries wolf gets disabled, which this project has
 * already watched happen once with a PEM scanner.
 *
 * Verbatim phrases have the opposite property. A run of eight or more words
 * appearing in both an internal document and the public site is not a
 * coincidence; it is a paste, which is the realistic failure mode. Somebody
 * writing the security page from memory does not reproduce a sentence.
 *
 * A short denylist covers the few things that are dangerous even paraphrased.
 */

import { readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, extname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const BUILD = resolve(here, "../build");
const REPO = resolve(here, "../..");

const FORBIDDEN_SOURCES = [
  "SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md",
  "PLAN/14-DEPLOYMENT.md",
  "PLAN/18-RISK-REGISTER.md",
];

/** Words in a run before it counts as a paste rather than a coincidence. */
const PHRASE_WORDS = 8;

/**
 * Dangerous even when paraphrased, so matched directly.
 *
 * Private addresses and risk-register identifiers have no business on a public
 * page under any phrasing.
 */
const DENY = [
  { pattern: /\b10\.\d{1,3}\.\d{1,3}\.\d{1,3}\b/, what: "a private IP address" },
  { pattern: /\b192\.168\.\d{1,3}\.\d{1,3}\b/, what: "a private IP address" },
  { pattern: /\bR-\d{2}\b/, what: "a PLAN/18 risk register identifier" },
  { pattern: /\bDV-\d{2}\b/, what: "a deviation identifier from TASKS/BACKLOG" },
];

/** Normalises text for comparison: tags out, whitespace collapsed, lowercased. */
function normalise(text) {
  return text
    .replace(/<script[\s\S]*?<\/script>/gi, " ")
    .replace(/<style[\s\S]*?<\/style>/gi, " ")
    .replace(/<[^>]+>/g, " ")
    .replace(/&[a-z]+;/gi, " ")
    .replace(/[^\p{L}\p{N}\s]/gu, " ")
    .replace(/\s+/g, " ")
    .toLowerCase()
    .trim();
}

/** Every run of PHRASE_WORDS consecutive words in a document. */
function phrasesOf(text) {
  const words = normalise(text).split(" ").filter(Boolean);
  const phrases = new Set();

  for (let i = 0; i + PHRASE_WORDS <= words.length; i++) {
    phrases.add(words.slice(i, i + PHRASE_WORDS).join(" "));
  }
  return phrases;
}

// --- build the forbidden phrase set ---------------------------------------

const forbidden = new Map();

for (const source of FORBIDDEN_SOURCES) {
  let contents;
  try {
    contents = readFileSync(resolve(REPO, source), "utf8");
  } catch {
    console.error(`Cannot read ${source} — the check cannot run without it.`);
    process.exit(1);
  }

  for (const phrase of phrasesOf(contents)) {
    if (!forbidden.has(phrase)) forbidden.set(phrase, source);
  }
}

// --- scan the built site ---------------------------------------------------

function* htmlFiles(dir) {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) yield* htmlFiles(full);
    else if (extname(entry) === ".html") yield full;
  }
}

const problems = [];
let pages = 0;

for (const file of htmlFiles(BUILD)) {
  pages++;
  const relative = file.slice(BUILD.length + 1).replace(/\\/g, "/");
  const raw = readFileSync(file, "utf8");

  for (const { pattern, what } of DENY) {
    const match = raw.match(pattern);
    if (match) problems.push(`${relative}: ${what} — "${match[0]}"`);
  }

  for (const phrase of phrasesOf(raw)) {
    const source = forbidden.get(phrase);
    if (source) {
      problems.push(`${relative}: verbatim from ${source} — "${phrase}"`);
      break; // One report per page is enough to act on.
    }
  }
}

if (pages === 0) {
  console.error("No HTML found in build/ — run `npm run build` first.");
  process.exit(1);
}

if (problems.length > 0) {
  console.error("Internal material found on the public site:\n");
  for (const problem of problems) console.error(`  ${problem}`);
  console.error(
    "\nPLAN/20 § What Never Gets Published: the public /security page " +
      "communicates posture at a trust level and never exposes attack " +
      "scenarios, infrastructure topology, or the risk register.",
  );
  process.exit(1);
}

console.log(
  `${pages} pages scanned against ${forbidden.size} phrases from ` +
    `${FORBIDDEN_SOURCES.length} internal documents; nothing leaked.`,
);
