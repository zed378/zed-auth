import type { SidebarsConfig } from "@docusaurus/plugin-content-docs";

/**
 * Documentation sidebar.
 *
 * Authored by hand rather than generated from the folder structure, so reading
 * order is a decision rather than an accident of filenames. `docs/UI-UX/20` § Docs
 * Home says this section's success metric is "time to correct page" — the
 * ordering below is what a first-time reader needs, not alphabetical.
 *
 * The API reference is appended from the generated sidebar, so an endpoint
 * cannot appear here without existing in `openapi/openapi.yaml`.
 */
const sidebars: SidebarsConfig = {
  docs: [
    "docs-home",
    "quickstart",
    {
      type: "category",
      label: "Concepts",
      // Concepts come before guides deliberately. A guide followed without the
      // model behind it produces a working integration nobody can modify.
      collapsed: false,
      items: ["concepts/model", "concepts/authorization", "concepts/sessions"],
    },
    {
      type: "category",
      label: "Guides",
      items: ["guides/guides-index"],
    },
    {
      type: "category",
      label: "Console",
      items: ["console/console-index"],
    },
    {
      type: "category",
      label: "API reference",
      link: {
        type: "generated-index",
        title: "API reference",
        // An explicit slug, so /docs/api-reference is a real route. Without it
        // Docusaurus puts the generated index at /docs/category/api-reference,
        // and every link written the obvious way is broken — which the build
        // catches because onBrokenLinks is "throw".
        slug: "/api-reference",
        description:
          "Generated from openapi/openapi.yaml, the same file the service's own " +
          "handlers are generated from. An endpoint documented here exists.",
      },
      items: require("./docs/api-reference/sidebar.ts"),
    },
  ],
};

export default sidebars;
