import js from "@eslint/js";
import jsxA11y from "eslint-plugin-jsx-a11y";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";
import tseslint from "typescript-eslint";

import local from "./eslint-local-rules.js";

export default tseslint.config(
  {
    ignores: [
      "dist/**",
      "node_modules/**",
      // Generated from openapi/openapi.yaml. Linting it would report on code
      // nobody can edit — the fix for anything wrong here is in the spec.
      "src/lib/api/schema.gen.ts",
      "playwright-report/**",
      "test-results/**",
    ],
  },

  js.configs.recommended,
  ...tseslint.configs.recommended,

  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      ecmaVersion: 2023,
      globals: { ...globals.browser },
      parserOptions: {
        ecmaFeatures: { jsx: true },
      },
    },
    plugins: {
      "react-hooks": reactHooks,
      "jsx-a11y": jsxA11y,
      local,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,

      // docs/UI-UX/13 targets WCAG 2.1 AA and says it is built in from the first
      // screen rather than retrofitted. These are errors, not warnings: a
      // warning in a lint run of a hundred files is a line people scroll past.
      ...jsxA11y.configs.recommended.rules,

      // The token discipline (docs/UI-UX/05 § Governance, console/README.md).
      "local/no-raw-color": "error",
      "local/no-arbitrary-value": "error",
      "local/no-inline-style": "error",

      // An unused variable in a component is usually a half-finished edit.
      "@typescript-eslint/no-unused-vars": [
        "error",
        { argsIgnorePattern: "^_", varsIgnorePattern: "^_" },
      ],
    },
  },

  {
    // Playwright's end-to-end tests.
    //
    // The React rules do not apply here and misfire badly if left on:
    // Playwright's fixture API takes a `use` callback that has nothing to do
    // with React's `use` hook, and `async ({}, use) =>` is its idiomatic way
    // of declaring a fixture that depends on nothing. Both are reported as
    // errors by rules that are correct about React and wrong about this file.
    files: ["e2e/**/*.ts"],
    languageOptions: {
      globals: { ...globals.node },
    },
    rules: {
      "react-hooks/rules-of-hooks": "off",
      "no-empty-pattern": "off",
      "local/no-raw-color": "off",
      "local/no-arbitrary-value": "off",
    },
  },

  {
    // Tests may name raw values: that is what they assert on.
    files: ["**/*.test.{ts,tsx}", "**/test/**"],
    languageOptions: {
      globals: { ...globals.node },
    },
    rules: {
      "local/no-raw-color": "off",
      "local/no-arbitrary-value": "off",
    },
  },

  {
    files: ["*.config.{js,ts}", "eslint-local-rules.js"],
    languageOptions: {
      globals: { ...globals.node },
    },
  },

  // Build-time scripts. They run under Node, not in a browser — `scripts/` is
  // where `gen-patterns.mjs` lives because that is where `js-yaml` is
  // installed, not because it is console code.
  {
    files: ["scripts/**/*.mjs"],
    languageOptions: {
      globals: { ...globals.node },
    },
  },
);
