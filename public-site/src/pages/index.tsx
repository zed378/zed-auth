import Link from "@docusaurus/Link";
import Layout from "@theme/Layout";
import type { ReactNode } from "react";

/**
 * The landing page.
 *
 * Structure from `docs/UI-UX/20` § Detailed Spec: Landing Page. Copy adapted from
 * `docs/UI-UX/21`'s blueprint, with `[Product name]` resolved to Zed Auth.
 *
 * Two rules shape what is here and what is not.
 *
 * **Nothing claims a capability that has not shipped** (`docs/UI-UX/21` § Content
 * Governance, `CLAUDE.md`). `docs/UI-UX/21`'s capabilities blueprint has four
 * cards — SSO, REST API, RBAC, policy-based access — written in the present
 * tense. Two of them are now true: Phase 1 built single sign-on and the
 * management API, and `P1-25` executed the quickstart against a running
 * deployment. The other two are still the design, labelled with the roadmap
 * phase that delivers them.
 *
 * The rule runs in both directions, which is the part that took a second
 * attempt. A card labelled "Phase 1" after Phase 1 shipped is as inaccurate as
 * one claiming a capability that does not exist — it just fails in the
 * direction nobody notices.
 *
 * **No social proof section.** `docs/UI-UX/20` is explicit: include it "only once
 * genuinely available", because "an empty or fabricated social-proof section
 * is worse than omitting it entirely". There are no users to quote.
 *
 * One primary CTA style, used consistently (`docs/UI-UX/20` § Interaction), with
 * secondary actions visually subordinate — avoiding the homepage with five
 * equally-weighted competing calls to action.
 */

interface Capability {
  title: string;
  body: string;
  /**
   * The `docs/PLAN/16` phase that delivers it, or `SHIPPED` once every task it
   * needs is done.
   *
   * Both directions are checked on every build by `scripts/check-claims.mjs`:
   * a capability still labelled with a phase whose tasks are all complete is
   * as inaccurate as one claiming to be available while they are not. The
   * first version only checked one direction, and this label is the reason it
   * had to grow the other — Phase 1 finished and the page went on describing
   * single sign-on as planned.
   */
  phase: string;
}

/** The label a shipped capability carries instead of a phase. */
export const SHIPPED = "Shipped — Phase 1";

const CAPABILITIES: Capability[] = [
  {
    title: "Single sign-on",
    body:
      "Log in once, reach every registered application — over standard OIDC and " +
      "OAuth 2.1, not a proprietary protocol. Authorization Code with PKCE, a " +
      "hosted login page, and tokens you verify locally against the published " +
      "key set.",
    phase: SHIPPED,
  },
  {
    title: "A complete REST API",
    body:
      "Everything the console can do, your scripts and CI can do too. " +
      "Organizations, projects, applications, users and the audit log, with the " +
      "reference generated from the same specification the service is built " +
      "from. Nothing is reachable only through the interface.",
    phase: SHIPPED,
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
            `docs/UI-UX/21`'s hero headline, verbatim. It is concrete rather than
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
            <Link className="button button--primary button--lg" to="/docs/quickstart">
              Get started
            </Link>
            <Link className="button button--secondary button--lg" to="/docs/concepts/model">
              Understand the model
            </Link>
          </div>

          {/*
            The honest status, above the fold rather than buried.

            `docs/UI-UX/21`'s blueprint puts "Get Started" here, pointing at a
            quickstart. For Phase 0 it pointed at the docs instead, because a
            "Get Started" button leading to a page that says "not available
            yet" costs more trust than it wins. `P1-25` made the quickstart
            real — executed end to end against a running deployment — so the
            button now points where the blueprint always said it should.
          */}
          <p className="site-lede" style={{ marginTop: "2rem" }}>
            <strong>Status: Phase 1 is built and running.</strong> Single sign-on and the
            management API work end to end — the quickstart below is executed against a
            live deployment rather than written from the specification. Roles,
            delegation and policies are specified and not started. There is no hosted
            signup: you run it yourself, from source.
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
          <h2>What it does, and what it will do</h2>
          <p className="site-lede" style={{ marginBottom: "2.5rem" }}>
            Two of these are built and running; two are specified and not started. The
            label on each card says which, and it is checked against the roadmap board
            on every build rather than kept true by hand.
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
          {/*
            The closing section restates the primary action rather than
            introducing a new one (`docs/UI-UX/20` § Detailed Spec: Landing Page,
            "Final CTA section, restating the primary action"). Same label,
            same destination, same visual weight as the hero.
          */}
          <div className="site-cta-row" style={{ marginTop: "2rem" }}>
            <Link className="button button--primary button--lg" to="/docs">
              Read the docs
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
