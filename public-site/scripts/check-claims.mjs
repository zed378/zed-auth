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
const CAPABILITY_CLAIMS = [
  { label: "Single sign-on", phase: "Phase 1", task: "P1-06" },
  { label: "A complete REST API", phase: "Phase 1", task: "P1-15" },
  { label: "Roles that scale to delegation", phase: "Phase 4", task: "P4-01" },
  { label: "Policies when roles are not enough", phase: "Phase 4b", task: "P4B-02" },
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

for (const claim of CAPABILITY_CLAIMS) {
  if (!landingText.includes(claim.label)) {
    problems.push(
      `"${claim.label}" is in CLAIMS.md but not on the landing page — ` +
        `remove it from this manifest, or the manifest is describing a page that changed`,
    );
    continue;
  }

  if (!landingText.includes(claim.phase)) {
    problems.push(
      `"${claim.label}" appears without its phase label "${claim.phase}" — ` +
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
  const status = statusOf(claim.task);

  if (status === null) {
    problems.push(
      `"${claim.label}" maps to ${claim.task}, which is not on the PROGRESS board`,
    );
    continue;
  }

  if (status === "DONE") {
    problems.push(
      `"${claim.label}" is labelled "${claim.phase}" but ${claim.task} is DONE — ` +
        `it has shipped, and describing it as planned is now the inaccuracy`,
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

console.log(
  `${CAPABILITY_CLAIMS.length} capability claims audited; each labelled and unshipped.`,
);
