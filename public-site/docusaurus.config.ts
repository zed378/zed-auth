import type * as Preset from "@docusaurus/preset-classic";
import type { Config } from "@docusaurus/types";
import { themes as prismThemes } from "prism-react-renderer";

/**
 * The public site: landing, about, docs, changelog, contact.
 *
 * **Governing documents**: `PLAN/20-PUBLIC-SITE-ARCHITECTURE.md` (technical),
 * `UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md` (design),
 * `UI-UX/21-CONTENT-AND-COPY-STRATEGY.md` (copy).
 *
 * One project rather than two (ADR-014). `PLAN/20` suggests a marketing SSG
 * alongside a separate docs framework, and `UI-UX/20` § Cross-Page
 * Requirements requires every page to share the same header and footer so that
 * moving Landing → Docs "never feels like a different product". Two projects
 * make that navigation a duplicated component, which is precisely how it
 * drifts. The marketing pages here are ordinary React/MDX and could move to a
 * separate Astro build later without touching the docs.
 */

// The site's own address. Wrong values here are not cosmetic: they produce
// canonical URLs and sitemap entries pointing somewhere that does not exist,
// which is an SEO problem on the surface whose entire job is discovery
// (`PLAN/20`).
const url = process.env.SITE_URL ?? "https://zedth.my.id";
const baseUrl = process.env.SITE_BASE_URL ?? "/";

const config: Config = {
  title: "Zed Auth",
  tagline: "One login. Every app. Full control over who can do what.",
  favicon: "img/favicon.svg",

  url,
  baseUrl,

  organizationName: "zed378",
  projectName: "zed-auth",

  // Fail the build on a broken internal link rather than shipping one.
  //
  // A 404 inside documentation is worse than a missing page: the reader
  // assumes the content exists and that they took a wrong turn. `throw` costs
  // a build failure now and saves a support question later.
  onBrokenLinks: "throw",
  onBrokenAnchors: "throw",
  onDuplicateRoutes: "throw",

  i18n: {
    defaultLocale: "en",
    locales: ["en"],
  },

  markdown: {
    format: "detect",
    hooks: {
      onBrokenMarkdownLinks: "throw",
    },
  },

  presets: [
    [
      "classic",
      {
        docs: {
          sidebarPath: "./sidebars.ts",
          routeBasePath: "docs",

          // Versioning is enabled from the first commit, deliberately.
          //
          // `PLAN/20` § Versioning Strategy requires old-version docs to stay
          // reachable through a version's deprecation window, and retrofitting
          // versioning once v1 docs exist means reorganising every file at the
          // moment there is the most content to break. Cheap now, painful
          // later.
          // `current` is the live, in-development documentation and is what a
          // visitor gets by default. `1.0` is a snapshot proving the
          // versioning pipeline works before there is a release to version —
          // the same reason the API reference is generated now from a spec
          // with two endpoints in it.
          //
          // It is labelled a placeholder rather than presented as a shipped
          // release. Everything else on this site is careful not to claim
          // something exists that does not; a version dropdown implying a 1.0
          // release would be the same lie in a different control.
          //
          // Delete the `1.0` snapshot when a real release replaces it:
          //   rm -rf versioned_docs/version-1.0 versioned_sidebars/version-1.0-sidebars.json
          //   and remove it from versions.json
          lastVersion: "current",
          versions: {
            current: {
              label: "v1 · in development",
              path: "",
              badge: false,
            },
            "1.0": {
              label: "1.0 · placeholder",
              path: "1.0",
              banner: "unmaintained",
            },
          },

          editUrl: "https://github.com/zed378/zed-auth/tree/main/public-site/",
          showLastUpdateTime: true,
        },

        blog: {
          // The blog plugin, used as the changelog (`PLAN/20` § Site
          // Structure). Reverse-chronological release notes are what a blog
          // engine already is; a second content type would be the same
          // machinery under a different name.
          path: "changelog",
          routeBasePath: "changelog",
          blogTitle: "Changelog",
          blogDescription: "Release notes for Zed Auth.",
          blogSidebarTitle: "Releases",
          blogSidebarCount: "ALL",
          showReadingTime: false,
          onUntruncatedBlogPosts: "ignore",
          feedOptions: {
            type: ["rss", "atom"],
            title: "Zed Auth changelog",
            description: "Release notes for Zed Auth.",
            xslt: true,
          },
        },

        theme: {
          customCss: "./src/css/custom.css",
        },

        sitemap: {
          lastmod: "date",
          changefreq: "weekly",
          priority: 0.5,
        },
      } satisfies Preset.Options,
    ],
  ],

  plugins: [
    // The API reference, generated from openapi/openapi.yaml.
    //
    // `CLAUDE.md` makes this a hard rule: API reference documentation is never
    // hand-written. The same file generates the backend's server interface
    // (ADR-013) and the console's client, so all three cannot disagree.
    [
      "docusaurus-plugin-openapi-docs",
      {
        id: "api",
        docsPluginId: "classic",
        config: {
          management: {
            specPath: "../openapi/openapi.yaml",
            outputDir: "docs/api-reference",
            sidebarOptions: {
              groupPathsBy: "tag",
              categoryLinkSource: "tag",
            },
          },
        },
      },
    ],

    // Local search, indexed at build time.
    //
    // `PLAN/20` names Algolia DocSearch or a built-in local search. Local,
    // because Algolia means an external crawler and an API key for a site that
    // currently has a dozen pages — and `UI-UX/20` § Docs Home requires fuzzy
    // matching because "a developer often doesn't know the exact terminology
    // this project uses yet", which local search does.
    [
      "@easyops-cn/docusaurus-search-local",
      {
        hashed: true,
        indexDocs: true,
        indexBlog: true,
        indexPages: true,
        docsRouteBasePath: "/docs",
        blogRouteBasePath: "/changelog",
        highlightSearchTermsOnTargetPage: true,
        explicitSearchResultPath: true,
      },
    ],
  ],

  themes: ["docusaurus-theme-openapi-docs"],

  themeConfig: {
    image: "img/social-card.png",

    colorMode: {
      // The site follows the reader's system setting and offers a switch.
      // Neither is a design statement; both are what a reader expects.
      defaultMode: "light",
      respectPrefersColorScheme: true,
    },

    metadata: [
      {
        name: "description",
        content:
          "Zed Auth is a centralized identity and access service: single sign-on over OIDC and OAuth 2.1, a complete REST management API, and role-based access control that scales to cross-organization delegation.",
      },
    ],

    navbar: {
      title: "Zed Auth",
      logo: {
        alt: "Zed Auth",
        src: "img/logo.svg",
      },
      items: [
        { to: "/docs", label: "Docs", position: "left" },
        { to: "/changelog", label: "Changelog", position: "left" },
        { to: "/about", label: "About", position: "left" },
        {
          type: "docsVersionDropdown",
          position: "right",
          dropdownActiveClassDisabled: true,
        },
        {
          href: "https://github.com/zed378/zed-auth",
          label: "GitHub",
          position: "right",
        },
      ],
    },

    footer: {
      style: "light",
      links: [
        {
          title: "Documentation",
          items: [
            { label: "Quickstart", to: "/docs/quickstart" },
            { label: "Concepts", to: "/docs/concepts/model" },
            { label: "API reference", to: "/docs/api-reference" },
          ],
        },
        {
          title: "Project",
          items: [
            { label: "About", to: "/about" },
            { label: "Changelog", to: "/changelog" },
            { label: "GitHub", href: "https://github.com/zed378/zed-auth" },
          ],
        },
        {
          title: "Contact",
          items: [
            { label: "Contact", to: "/contact" },
            // The responsible-disclosure path, which `PLAN/20` § What Never
            // Gets Published requires to exist and be documented — a
            // researcher with nowhere to report goes public instead.
            { label: "Report a security issue", to: "/contact#security" },
          ],
        },
      ],
      copyright: `© ${new Date().getFullYear()} Zed Auth.`,
    },

    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ["bash", "json", "go"],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
