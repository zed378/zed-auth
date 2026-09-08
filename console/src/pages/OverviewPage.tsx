/**
 * The organization overview.
 *
 * A placeholder that says what it is. The real screen is specified in
 * `UI-UX/08-PAGE-SPECIFICATIONS.md` and built in Phase 1 — `PLAN/16` forbids
 * building a Phase N+1 feature while Phase N is incomplete, and this shell's
 * job is to prove the foundation, not to start the pages.
 */
export function OverviewPage() {
  return (
    <div className="flex flex-col gap-4">
      <header className="flex flex-col gap-1">
        <h1 className="text-heading-1 font-bold text-text-primary">Overview</h1>
        <p className="text-body text-text-secondary">
          The console shell. Design tokens, routing, navigation and the API client are in
          place; the screens themselves arrive in Phase 1.
        </p>
      </header>

      <section
        aria-labelledby="foundation-heading"
        className="max-w-form rounded border border-border bg-bg-surface p-5 shadow-flat"
      >
        <h2 id="foundation-heading" className="text-heading-3 font-medium text-text-primary">
          What is wired up
        </h2>

        <ul className="mt-3 flex flex-col gap-2 text-body text-text-secondary">
          <li>
            Every design token from <code>UI-UX/05</code>, by name, with contrast verified
            by test rather than asserted in a comment.
          </li>
          <li>
            A typed API client generated from <code>openapi/openapi.yaml</code> — the same
            file the backend&rsquo;s handlers are generated from.
          </li>
          <li>
            The navigation tree from <code>PLAN/06</code>, which mirrors the data model so
            navigation never needs a concept the model does not have.
          </li>
        </ul>
      </section>
    </div>
  );
}
