#!/usr/bin/env node
/**
 * The capability audit, as a check rather than a one-time read.
 *
 * `P0-19`'s Definition of Done asks that "a capability audit confirms every
 * claim on every published page maps to something either shipped or explicitly
 * labelled as planned". `UI-UX/21` § Content Governance and `CLAUDE.md` both
 * make it a standing rule rather than a launch task.
 *
 * A read confirms it today. The rule has to hold in six months, when someone
 * tightens a sentence and a hedge disappears — which is how a roadmap item
 * becomes a claim without anybody deciding to make one.
 *
 * Two things are checkable mechanically, and they are the two that matter:
 *
 *   1. Every capability described on the landing page carries a phase label.
 *      A capability card without one reads as available.
 *   2. Nothing labelled with a future phase is a task PROGRESS.md already
 *      marks DONE, and nothing described as available is a task that is not.
 *      A stale label is the same lie as a missing one.
 *
 * What it cannot check is prose. "Zed Auth provides SSO" in a paragraph will
 * pass this and be wrong. That is what CLAIMS.md and human review are for, and
 * saying so plainly here is better than implying the script is the whole
 * control.
 *
 * Run against the built site, because that is what a visitor sees — a check
 * over the source would miss anything a component interpolates.
 */

import { readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const BUILD = resolve(here, "../build");
const PROGRESS = resolve(here, "../../TASKS/PROGRESS.md");

/**
 * Capabilities the landing page describes, and the roadmap task that ships
 * each one.
 *
 * Kept here rather than inferred from the page, so that adding a capability
 * card means making a claim about its status in a file a reviewer reads —
 * which is the point of the audit.
 */
/**
 * Each capability lists EVERY task that has to ship before a visitor could use
 * it, not just the most representative one.
 *
 * This started as one task per capability and was wrong the first time it
 * mattered. `P1-06` shipped the authorization endpoint and the audit declared
 * single sign-on shipped — but a consumer application still could not complete
 * a login, because there was no token endpoint to exchange the code at and no
 * page to log in on. Marking the card "available" then would have been exactly
 * the false claim this check exists to prevent, produced by the check itself.
 *
 * So a capability is shipped when all of its tasks are, and the audit says
 * which ones are outstanding.
 */
const CAPABILITY_CLAIMS = [
  {
    label: "Single sign-on",
    phase: "Phase 1",
    // The list grew a second time, and for the same reason it grew the first.
    //
    // `P1-06` issues the code, `P1-07` exchanges it, `P1-12` is where a user
    // without a session actually logs in. With those three the PROTOCOL is
    // complete — and this check fired anyway on the day `P1-12` landed, saying
    // the capability had shipped and the card should stop saying "Phase 1".
    //
    // It had not shipped. A visitor cannot register an application (`P1-18`)
    // or create a user with a password (`P1-19`), so there is nothing to sign
    // in to and nobody to sign in as. "A consumer can complete a login" was
    // being measured against a database somebody else populated by hand.
    //
    // The distinction the list has to encode is *usable by a visitor*, not
    // *implemented*. That is what a capability card promises.
    tasks: ["P1-06", "P1-07", "P1-12", "P1-18", "P1-19"],
  },
  {
    label: "A complete REST API",
    phase: "Phase 1",
    tasks: ["P1-15"],
  },
  {
    label: "Roles that scale to delegation",
    phase: "Phase 4",
    tasks: ["P4-01"],
  },
  {
    label: "Policies when roles are not enough",
    phase: "Phase 4b",
    tasks: ["P4B-02"],
  },
];

function fail(lines) {
  console.error("Capability audit failed:\n");
  for (const line of lines) console.error(`  ${line}`);
  console.error(
    "\nUI-UX/21 § Content Governance: copy never describes a capability " +
      "beyond the shipped phase. See public-site/CLAIMS.md.",
  );
  process.exit(1);
}

// --- the landing page ------------------------------------------------------

let landing;
try {
  landing = readFileSync(join(BUILD, "index.html"), "utf8");
} catch {
  fail(["build/index.html not found — run `npm run build` first"]);
}

const problems = [];

// Text content, with tags stripped, so a label split across elements still
// matches.
const landingText = landing.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ");

/**
 * The capability cards, split apart and keyed by their title.
 *
 * Split rather than searched as one string, because two cards share the label
 * "Phase 1": a check that asked whether "Phase 1" appears ANYWHERE on the page
 * was satisfied by either of them, so deleting the label from one card would
 * have passed. The label has to be found inside the card it belongs to.
 */
function capabilityCards(html) {
  const cards = new Map();
  for (const [, block] of html.matchAll(/<article[^>]*class="[^"]*site-card[^"]*"[^>]*>([\s\S]*?)<\/article>/g)) {
    const text = block.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ").trim();
    const title = (block.match(/class="[^"]*site-card__title[^"]*"[^>]*>([^<]*)</) || [])[1];
    if (title) cards.set(title.trim(), text);
  }
  return cards;
}

const cards = capabilityCards(landing);

if (cards.size === 0) {
  problems.push(
    "no capability cards were found on the landing page — the markup changed " +
      "and this audit is now checking nothing",
  );
}

for (const claim of CAPABILITY_CLAIMS) {
  if (!landingText.includes(claim.label)) {
    problems.push(
      `"${claim.label}" is in CLAIMS.md but not on the landing page — ` +
        `remove it from this manifest, or the manifest is describing a page that changed`,
    );
    continue;
  }

  const card = cards.get(claim.label);
  if (card === undefined) {
    problems.push(
      `"${claim.label}" is on the page but not as a capability card, so its ` +
        `phase label cannot be checked against it`,
    );
    continue;
  }

  if (!card.includes(claim.phase)) {
    problems.push(
      `the "${claim.label}" card does not carry its phase label "${claim.phase}" — ` +
        `an unlabelled capability reads as available`,
    );
  }
}

// --- labels against the actual roadmap -------------------------------------

const progress = readFileSync(PROGRESS, "utf8");

/** Reads a task's status from the PROGRESS board. */
function statusOf(taskId) {
  const row = progress
    .split("\n")
    .find((line) => line.startsWith(`| ${taskId} |`));
  if (!row) return null;
  return /\*\*DONE\*\*/.test(row) ? "DONE" : "NOT DONE";
}

for (const claim of CAPABILITY_CLAIMS) {
  const missing = claim.tasks.filter((task) => statusOf(task) === null);
  if (missing.length > 0) {
    problems.push(
      `"${claim.label}" maps to ${missing.join(", ")}, which is not on the PROGRESS board`,
    );
    continue;
  }

  const outstanding = claim.tasks.filter((task) => statusOf(task) !== "DONE");

  if (outstanding.length === 0) {
    problems.push(
      `"${claim.label}" is labelled "${claim.phase}" but every task it needs ` +
        `(${claim.tasks.join(", ")}) is DONE — it has shipped, and describing ` +
        `it as planned is now the inaccuracy`,
    );
  }
}

// --- the site's own status statement ---------------------------------------

// The landing page says the service is in development. That has to stop being
// true at some point, and the moment it does this line is the one that lies.
if (!landingText.includes("Status: in development")) {
  problems.push(
    'the landing page no longer carries its "Status: in development" statement — ' +
      "if that is deliberate, update this check and CLAIMS.md together",
  );
}

if (problems.length > 0) fail(problems);

const remaining = CAPABILITY_CLAIMS.map(
  (claim) =>
    `${claim.label} (${claim.tasks.filter((t) => statusOf(t) !== "DONE").join(", ")})`,
);

console.log(
  `${CAPABILITY_CLAIMS.length} capability claims audited; each labelled and unshipped.\n` +
    `  still outstanding: ${remaining.join("; ")}`,
);
