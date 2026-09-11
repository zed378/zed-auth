#!/usr/bin/env node
/**
 * The capability audit, as a check rather than a one-time read.
 *
 * `P0-19`'s Definition of Done asks that "a capability audit confirms every
 * claim on every published page maps to something either shipped or explicitly
 * labelled as planned". `docs/UI-UX/21` § Content Governance and `CLAUDE.md` both
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
/**
 * The label a shipped capability carries instead of a phase, mirroring
 * `src/pages/index.tsx`. A literal here rather than an import: this script
 * reads the BUILT html, so the check should depend on what a visitor sees and
 * not on whether a TSX module compiles.
 */
const SHIPPED = "Shipped — Phase 1";

const CAPABILITY_CLAIMS = [
  {
    label: "Single sign-on",
    phase: SHIPPED,
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
    // A fourth revision, and this one closes the loop rather than widening it.
    // `P1-26` is here because it is the proof: two separate applications, two
    // client IDs, one login, verified against staging. Until something had
    // actually done that, "single sign-on" described the endpoints rather than
    // the capability.
    tasks: ["P1-06", "P1-07", "P1-12", "P1-18", "P1-19", "P1-26"],
  },
  {
    label: "A complete REST API",
    // `P1-15` is the envelope and the bearer middleware. The four resource
    // groups are what make "everything the console can do" true, and the audit
    // log is the read the console's own screen depends on.
    phase: SHIPPED,
    tasks: ["P1-15", "P1-16", "P1-17", "P1-18", "P1-19", "P1-20"],
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
      `the "${claim.label}" card does not carry its label "${claim.phase}" — ` +
        `an unlabelled capability reads as available`,
    );
  }

  // A shipped card must not ALSO carry a bare phase label, and a planned card
  // must not carry the shipped marker. Either mixture reads as the other thing
  // to somebody skimming the grid.
  if (claim.phase === SHIPPED && /Phase [0-9]/.test(card.split(SHIPPED).join(""))) {
    problems.push(
      `the "${claim.label}" card is marked shipped and still carries a bare ` +
        `phase label, which reads as planned`,
    );
  }
  if (claim.phase !== SHIPPED && card.includes("Shipped")) {
    problems.push(
      `the "${claim.label}" card is labelled "${claim.phase}" and also says "Shipped"`,
    );
  }
}

// --- labels against the actual roadmap -------------------------------------

const progress = readFileSync(PROGRESS, "utf8");

/**
 * Reads a task's status from the PROGRESS board.
 *
 * The STATUS COLUMN, not the row. The first version searched the whole line
 * for a bolded `**DONE**`, and the board bolds a status only when the row
 * carries a caveat worth drawing the eye to — most completed tasks are a plain
 * `DONE`. So this read every one of them as unfinished.
 *
 * The effect was invisible in the direction the check was originally written:
 * everything looked less shipped than it was, which only ever suppressed a
 * complaint. It surfaced the moment the check gained its other direction and a
 * capability had to PROVE it was shipped. A check that fails safe in one
 * direction and silently wrong in the other is worth distrusting on sight.
 *
 * Parsing the column rather than the line also means a status of "not DONE",
 * or a note mentioning DONE, cannot be mistaken for the status itself.
 */
function statusOf(taskId) {
  const row = progress
    .split("\n")
    .find((line) => line.startsWith(`| ${taskId} |`));
  if (!row) return null;

  // | id | name | size | status | depends on |
  const status = row.split("|")[4];
  if (status === undefined) return null;

  return /^\s*(\*\*)?DONE(\*\*)?\b/.test(status) ? "DONE" : "NOT DONE";
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

  // The direction that matters most, and the one the first version of this
  // check did not have: the page says a visitor can use this, and the board
  // says it is not finished.
  if (claim.phase === SHIPPED && outstanding.length > 0) {
    problems.push(
      `"${claim.label}" is described as shipped, but ${outstanding.join(", ")} ` +
        `${outstanding.length === 1 ? "is" : "are"} not DONE on the PROGRESS board`,
    );
  }

  if (claim.phase !== SHIPPED && outstanding.length === 0) {
    problems.push(
      `"${claim.label}" is labelled "${claim.phase}" but every task it needs ` +
        `(${claim.tasks.join(", ")}) is DONE — it has shipped, and describing ` +
        `it as planned is now the inaccuracy`,
    );
  }
}

// --- the site's own status statement ---------------------------------------

// The landing page carries one status sentence, and it is the sentence a
// visitor reads before any card. It said "in development" through Phase 0 and
// now says Phase 1 is built — changed on the page, here and in CLAIMS.md in
// one commit, which is what the old message asked for. Pinning the current
// wording means the next change has to be deliberate too.
const STATUS = "Status: Phase 1 is built and running";
if (!landingText.includes(STATUS)) {
  problems.push(
    `the landing page no longer carries its "${STATUS}" statement — ` +
      "if that is deliberate, update this check and CLAIMS.md together",
  );
}

// And the qualifier inside it. With two capabilities marked shipped, this is
// the sentence that keeps "shipped" from reading as "available to you, now" —
// there is no hosted offering to sign up for.
if (!landingText.includes("no hosted signup")) {
  problems.push(
    "the landing page no longer says there is no hosted signup, which is what " +
      'stops a "Shipped" label reading as an invitation to sign up',
  );
}

if (problems.length > 0) fail(problems);

const shipped = CAPABILITY_CLAIMS.filter((c) => c.phase === SHIPPED);
const planned = CAPABILITY_CLAIMS.filter((c) => c.phase !== SHIPPED);
const outstandingFor = (c) =>
  c.tasks.filter((t) => statusOf(t) !== "DONE").join(", ");

console.log(
  `${CAPABILITY_CLAIMS.length} capability claims audited; every label matches the board.
  shipped: ${shipped.map((c) => c.label).join("; ") || "none"}
  planned: ${planned.map((c) => `${c.label} (${outstandingFor(c)})`).join("; ") || "none"}`,
);
