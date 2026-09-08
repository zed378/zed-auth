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

      // UI-UX/13 targets WCAG 2.1 AA and says it is built in from the first
      // screen rather than retrofitted. These are errors, not warnings: a
      // warning in a lint run of a hundred files is a line people scroll past.
      ...jsxA11y.configs.recommended.rules,

      // The token discipline (UI-UX/05 § Governance, console/README.md).
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
);
