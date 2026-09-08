import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

import { beforeAll, describe, expect, it } from "vitest";

/**
 * Compiles Tailwind and asserts that the utility classes the console uses
 * actually produce CSS.
 *
 * This test exists because of a bug it would have caught and `tokens.test.ts`
 * could not. The tokens were all defined, correctly named after UI-UX/05, and
 * verified for contrast — and `text-body`, `text-heading-1` and the rest
 * generated nothing at all, because Tailwind v4 reads font sizes from
 * `--text-*` and `--font-size-*` is not a namespace it knows. Every piece of
 * text in the console rendered at the browser default.
 *
 * Nothing failed. The stylesheet was valid, the build succeeded, the tests
 * passed, and the tests passed *because they read the source file* — which
 * contained exactly what it was supposed to contain. The gap between "the
 * token is defined" and "the class works" is invisible from the source, and
 * this is the only place it becomes visible.
 *
 * The general shape is worth remembering: a test that reads the input to a
 * compiler cannot tell you what the compiler did.
 */

const CONSOLE_ROOT = process.cwd();

let css = "";

/** True if the compiled output contains a rule for this class. */
function generates(className: string): boolean {
  // Tailwind escapes `:` in variant prefixes, so `tablet:w-nav` appears as
  // `.tablet\:w-nav`.
  const escaped = className.replace(/[:.]/g, "\\\\$&").replace(/[[\]/]/g, "\\$&");
  return new RegExp(`\\.${escaped}[{,:\\s]`).test(css);
}

beforeAll(() => {
  const dir = mkdtempSync(join(tmpdir(), "console-utilities-"));

  try {
    const out = join(dir, "out.css");

    // Compiled against the real source tree, so this reflects the classes the
    // components actually use rather than a list maintained by hand.
    //
    // The CLI's JS entry point is invoked with the current Node binary rather
    // than through `npx`. Two reasons, both learned the hard way: `npx` on
    // Windows resolves to a `.cmd` shim that `execFileSync` refuses to spawn
    // without a shell (EINVAL), and an un-installed `npx` package downloads
    // from the network — a test that needs the internet to pass is a test that
    // fails in the wrong places.
    execFileSync(
      process.execPath,
      [
        resolve(CONSOLE_ROOT, "node_modules/@tailwindcss/cli/dist/index.mjs"),
        "-i",
        resolve(CONSOLE_ROOT, "src/styles/tokens.css"),
        "-o",
        out,
        "--content",
        resolve(CONSOLE_ROOT, "src/**/*.{ts,tsx}"),
      ],
      { cwd: CONSOLE_ROOT, stdio: "pipe" },
    );

    css = readFileSync(out, "utf8");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}, 120_000);

describe("the token-derived utilities the shell depends on", () => {
  // Each of these appears in a component. A miss means that component is
  // styled by a class that does not exist — which looks like a design mistake
  // and is actually a dead class name.
  it.each([
    // Type scale. The five that silently did nothing.
    "text-small",
    "text-body",
    "text-heading-1",
    "text-heading-2",
    "text-heading-3",

    // Colour, from --color-*.
    "bg-bg-base",
    "bg-bg-surface",
    "text-text-primary",
    "text-text-secondary",
    "text-accent",
    "bg-accent",
    "border-border",

    // Weight, elevation, layout.
    "font-medium",
    "font-bold",
    "shadow-flat",
    "shadow-raised",
    "max-w-form",

    // The responsive nav track, which must land inside a media query.
    "tablet:w-nav",
  ])("%s generates a rule", (className) => {
    expect(generates(className)).toBe(true);
  });

  it("does not claim a rule exists for a class nobody defined", () => {
    // Without this the whole suite could pass by matching too loosely, which
    // is how a check ends up confirming itself.
    expect(generates("text-nonexistent-token")).toBe(false);
    expect(generates("bg-not-a-colour")).toBe(false);
  });
});

describe("the responsive tracks from UI-UX/12", () => {
  it("puts the tablet nav width behind the tablet breakpoint", () => {
    // UI-UX/12 § Breakpoint Strategy: the persistent left nav is a desktop and
    // tablet affordance. Below that the shell stacks, so the width must be
    // inside the media query rather than applied unconditionally.
    //
    // Checked by splitting on @media rather than with one regex over the whole
    // file: the CLI emits formatted CSS and the production build emits
    // minified, and a pattern that assumes either one passes in one place and
    // fails in the other for reasons that have nothing to do with the rule.
    const blocks = css.split("@media").slice(1);
    const tabletBlocks = blocks.filter((block) => {
      const header = block.slice(0, block.indexOf("{"));
      return header.includes("768px");
    });

    expect(tabletBlocks.length).toBeGreaterThan(0);
    expect(tabletBlocks.some((block) => block.includes(".tablet\\:w-nav"))).toBe(true);
  });

  it("does not apply the nav width unconditionally", () => {
    // The failure this catches: a nav that keeps its fixed width when the
    // shell stacks below tablet, leaving a 240px column and a squeezed page.
    const beforeFirstMedia = css.split("@media")[0];

    expect(beforeFirstMedia).not.toContain(".tablet\\:w-nav");
  });

  it("defines the three breakpoints UI-UX/12 names", () => {
    for (const width of ["768px", "1024px", "1440px"]) {
      expect(css).toContain(width);
    }
  });
});

describe("token values reach the compiled output", () => {
  it.each([
    "--color-bg-base",
    "--color-bg-surface",
    "--color-text-primary",
    "--color-text-secondary",
    "--color-accent",
    "--color-danger",
    "--color-warning",
    "--color-success",
    "--color-border",
  ])("%s is emitted", (name) => {
    // `@theme static` rather than a bare `@theme`, so a token exists whether
    // or not a utility happens to reference it. `--color-danger` has no user
    // in the shell yet and must still be there for the first destructive
    // action that needs it.
    expect(css).toContain(`${name}:`);
  });
});

describe("the stylesheet the browser receives", () => {
  it("carries the focus ring and the reduced-motion fallback", () => {
    // Both are in tokens.css and tokens.test.ts checks them there. This checks
    // they survive compilation, which is a different question — the one this
    // whole file exists to ask.
    expect(css).toMatch(/:focus-visible/);
    expect(css).toMatch(/prefers-reduced-motion/);
  });
});

// A guard on the harness itself: if the compile step silently produced nothing,
// every `expect(...).toBe(false)` above would pass and the suite would go green
// on an empty file.
describe("the compile step produced something", () => {
  it("produced a non-trivial stylesheet", () => {
    expect(css.length).toBeGreaterThan(2000);
  });

  it("wrote the file this test suite is reading", () => {
    const source = readFileSync(
      resolve(CONSOLE_ROOT, "src/styles/tokens.css"),
      "utf8",
    );
    // A token defined in the source must appear in the output, or the compile
    // ran against something other than our stylesheet.
    expect(source).toContain("--color-accent:");
    expect(css).toContain("--color-accent:");
  });
});
