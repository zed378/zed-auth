/**
 * Local ESLint rules enforcing the token discipline.
 *
 * `UI-UX/05-DESIGN-SYSTEM.md` § Governance requires token names rather than
 * raw values everywhere, so that a rebrand or theme adjustment never means
 * hunting through every page spec. A rule people are asked to remember is a
 * rule that holds until the week someone is in a hurry; these make it a build
 * failure instead.
 *
 * `console/README.md` lists "tokens only" as non-negotiable. This is where
 * that stops being a sentence.
 */

/** Hex colours: #rgb, #rgba, #rrggbb, #rrggbbaa. */
const HEX = /#[0-9a-fA-F]{3,8}\b/;

/** CSS colour functions written out longhand. */
const COLOUR_FN = /\b(rgba?|hsla?|hwb|lab|lch|oklab|oklch|color)\s*\(/;

/**
 * Tailwind's arbitrary-value syntax: `p-[13px]`, `text-[#abc]`, `w-[42rem]`.
 *
 * The escape hatch is the point of Tailwind and the problem here: it lets a
 * component step outside the 4px/8px scale without anything noticing, which is
 * how two screens end up 1px apart for no reason.
 */
const ARBITRARY = /\b[a-z-]+-\[[^\]]+\]/;

/**
 * Where raw values are legitimate.
 *
 * tokens.css is where the values live, by definition. The tests read those
 * values and assert on them, so they must be able to name them. Everything
 * else is application code.
 */
const VALUE_FILES = /(tokens\.css|tokens\.test\.ts|branding\.ts|branding\.test\.ts)$/;

function isExempt(filename) {
  return VALUE_FILES.test(filename.replace(/\\/g, "/"));
}

/** Reports on string literals and template chunks, wherever they appear. */
function makeStringRule({ pattern, messageId, message }) {
  return {
    meta: {
      type: "problem",
      docs: { description: message },
      schema: [],
      messages: { [messageId]: message },
    },
    create(context) {
      const filename = context.filename ?? context.getFilename();
      if (isExempt(filename)) return {};

      function check(node, value) {
        if (typeof value !== "string") return;
        if (!pattern.test(value)) return;
        context.report({ node, messageId });
      }

      return {
        Literal(node) {
          check(node, node.value);
        },
        TemplateElement(node) {
          check(node, node.value.raw);
        },
        JSXText(node) {
          check(node, node.value);
        },
      };
    },
  };
}

export default {
  rules: {
    /**
     * A raw hex colour or CSS colour function in application code.
     *
     * This is P0-17's Definition of Done in one rule: "a raw hex color in a
     * component fails lint".
     */
    "no-raw-color": makeStringRule({
      pattern: new RegExp(`${HEX.source}|${COLOUR_FN.source}`),
      messageId: "rawColour",
      message:
        "Raw colour value. Use a design token (UI-UX/05 § Governance): a Tailwind " +
        "class such as `text-text-primary`, or `var(--color-...)`. If no token fits, " +
        "the token goes into UI-UX/05 and src/styles/tokens.css first — the design " +
        "system is the source of truth, not the screen that needed something new.",
    }),

    /**
     * Tailwind arbitrary values, which step outside the scales.
     */
    "no-arbitrary-value": makeStringRule({
      pattern: ARBITRARY,
      messageId: "arbitrary",
      message:
        "Arbitrary Tailwind value. Use the 4px/8px spacing scale or the type scale " +
        "from src/styles/tokens.css (UI-UX/05 § Spacing & Layout). A one-off value " +
        "here is how two screens end up a pixel apart for no reason.",
    }),

    /**
     * Inline `style={{ ... }}` on a JSX element.
     *
     * Not a token rule exactly, and it belongs with them: an inline style is
     * the easiest way to smuggle a raw value past the two rules above, since
     * a numeric `padding: 13` is not a string at all.
     *
     * Allowed where a value is genuinely computed at runtime — a progress bar's
     * width — via an eslint-disable line, which is a review conversation
     * rather than a silent exception.
     */
    "no-inline-style": {
      meta: {
        type: "problem",
        docs: { description: "Disallow inline style props" },
        schema: [],
        messages: {
          inlineStyle:
            "Inline style. Use token-based classes so the value stays in the design " +
            "system. If the value is genuinely computed at runtime, disable this rule " +
            "on the line with a reason.",
        },
      },
      create(context) {
        const filename = context.filename ?? context.getFilename();
        if (isExempt(filename)) return {};

        return {
          JSXAttribute(node) {
            if (node.name?.name !== "style") return;
            context.report({ node, messageId: "inlineStyle" });
          },
        };
      },
    },
  },
};
