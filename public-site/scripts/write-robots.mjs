#!/usr/bin/env node
/**
 * Writes `build/robots.txt` with the site's real origin.
 *
 * The `Sitemap:` directive has to be an absolute URL, so a static
 * `static/robots.txt` has to hard-code a host — and the first deploy to a
 * hostname other than the guessed one publishes a sitemap pointer to somewhere
 * that does not exist. That happened: the site went live at
 * app-auth.zedth.my.id advertising a sitemap at zedth.my.id.
 *
 * It fails quietly, which is the problem. Nothing 404s for a visitor; a
 * crawler follows the pointer, finds nothing, and the site is simply not
 * indexed — on the surface whose entire job is discovery (`docs/PLAN/20`).
 *
 * Generating it from the same `SITE_URL` that Docusaurus uses for canonical
 * URLs means the two cannot disagree.
 */

import { writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const url = (process.env.SITE_URL ?? "https://zedth.my.id").replace(/\/$/, "");

const robots = `# The public site is meant to be found: docs/PLAN/20 names SEO as critical, since
# this is how the product gets discovered.
#
# Generated at build time from SITE_URL so the sitemap pointer always matches
# the host the site is actually served from. Do not edit build/robots.txt by
# hand — edit scripts/write-robots.mjs.
User-agent: *
Allow: /

Sitemap: ${url}/sitemap.xml
`;

const target = resolve(here, "../build/robots.txt");
writeFileSync(target, robots, "utf8");

console.log(`robots.txt written for ${url}`);
