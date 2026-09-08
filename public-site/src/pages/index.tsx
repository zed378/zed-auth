import Link from "@docusaurus/Link";
import Layout from "@theme/Layout";
import type { ReactNode } from "react";

/**
 * The landing page.
 *
 * Structure from `UI-UX/20` § Detailed Spec: Landing Page. Copy adapted from
 * `UI-UX/21`'s blueprint, with `[Product name]` resolved to Zed Auth.
 *
 * Two rules shape what is here and what is not.
 *
 * **Nothing claims a capability that has not shipped** (`UI-UX/21` § Content
 * Governance, `CLAUDE.md`). `UI-UX/21`'s capabilities blueprint has four
 * cards — SSO, REST API, RBAC, policy-based access — written in the present
 * tense. The project is in Phase 0: the service runs, it is deployed, and it
 * serves two operational probes. None of those four exist yet. So they are
 * described as the design, in a section that says the roadmap phase each
 * arrives in, rather than as things a visitor can use today.
 *
 * **No social proof section.** `UI-UX/20` is explicit: include it "only once
 * genuinely available", because "an empty or fabricated social-proof section
 * is worse than omitting it entirely". There are no users to quote.
 *
 * One primary CTA style, used consistently (`UI-UX/20` § Interaction), with
 * secondary actions visually subordinate — avoiding the homepage with five
 * equally-weighted competing calls to action.
 */

interface Capability {
  title: string;
  body: string;
  /** The `PLAN/16` phase that delivers it. */
  phase: string;
}

const CAPABILITIES: Capability[] = [
  {
    title: "Single sign-on",
    body:
      "Log in once, reach every registered application — over standard OIDC and " +
      "OAuth 2.1, not a proprietary protocol.",
    phase: "Phase 1",
  },
  {
    title: "A complete REST API",
    body:
      "Everything the console can do, your scripts and CI can do too. Nothing is " +
      "reachable only through the interface.",
    phase: "Phase 1",
  },
  {
    title: "Roles that scale to delegation",
    body:
      "Start with per-project roles. Add cross-organization delegation when a " +
      "partner needs to manage their own team's access — without giving up which " +
      "roles they are allowed to grant.",
    phase: "Phase 4",
  },
  {
    title: "Policies when roles are not enough",
    body:
      "For rules that depend on context — department, an amount limit, time of " +
      "day — layer attribute-based policies over the roles you already have.",
    phase: "Phase 4b",
  },
];

export default function Home(): ReactNode {
  return (
    <Layout
      title="Identity and access for every app you build"
      description="Zed Auth is a centralized identity and access service: single sign-on over OIDC and OAuth 2.1, a complete REST management API, and role-based access control that scales to cross-organization delegation."
    >
      <header className="site-hero">
        <div className="site-container">
          {/*
            `UI-UX/21`'s hero headline, verbatim. It is concrete rather than
            generic — "Secure your business" is the failure mode that document
            names — and it survives the shipped-capability rule because it
            describes what the product is for, not what you can do with it
            today.
          */}
          <h1 className="site-hero__title">
            One login. Every app. Full control over who can do what.
          </h1>

          <p className="site-hero__subtitle">
            A centralized identity and access service with single sign-on, a complete
            REST API, and role-based access control that scales from one team to
            multi-organization delegation.
          </p>

          <div className="site-cta-row">
            <Link className="button button--primary button--lg" to="/docs">
              Read the docs
            </Link>
            <Link className="button button--secondary button--lg" to="/docs/concepts/model">
              Understand the model
            </Link>
          </div>

          {/*
            The honest status, above the fold rather than buried.

            `UI-UX/21`'s blueprint puts "Get Started" here, pointing at a
            quickstart. There is no working integration to start, so the CTA
            points at the docs instead — a "Get Started" button leading to a
            page that says "not available yet" costs more trust than it wins.
          */}
          <p className="site-lede" style={{ marginTop: "2rem" }}>
            <strong>Status: in development.</strong> The service runs and is deployed;
            the authentication and authorization endpoints described below are being
            built. This site describes the design and says which phase each part
            arrives in.
          </p>
        </div>
      </header>

      <section className="site-section site-section--surface">
        <div className="site-container">
          <h2>Stop rebuilding login for every service.</h2>
          <p className="site-lede">
            Every new application means another login form, another user table, another
            place permissions drift out of sync. Zed Auth centralizes authentication and
            authorization once, so every app you build or buy plugs into the same
            identity layer.
          </p>
        </div>
      </section>

      <section className="site-section">
        <div className="site-container">
          <h2>What it is designed to do</h2>
          <p className="site-lede" style={{ marginBottom: "2.5rem" }}>
            Each of these is specified and none is finished. The phase label is the
            roadmap milestone that delivers it.
          </p>

          <div className="site-grid">
            {CAPABILITIES.map((capability) => (
              <article className="site-card" key={capability.title}>
                <h3 className="site-card__title">{capability.title}</h3>
                <p className="site-card__body">{capability.body}</p>
                <p
                  className="site-card__body"
                  style={{ marginTop: "0.75rem", fontSize: "0.9rem" }}
                >
                  <em>{capability.phase}</em>
                </p>
              </article>
            ))}
          </div>
        </div>
      </section>

      <section className="site-section site-section--surface">
        <div className="site-container">
          <h2>Built in the open, decided in writing</h2>
          <p className="site-lede">
            The engineering plan, the threat model, and every architectural decision are
            in the repository — including the ones that turned out to be wrong. If you
            are evaluating this, the reasoning is available to read rather than
            summarized in a brochure.
          </p>
          <div className="site-cta-row" style={{ marginTop: "2rem" }}>
            <Link className="button button--primary button--lg" to="/docs/concepts/model">
              Read the concepts
            </Link>
            <Link
              className="button button--secondary button--lg"
              href="https://github.com/zed378/zed-auth"
            >
              Browse the source
            </Link>
          </div>
        </div>
      </section>
    </Layout>
  );
}
