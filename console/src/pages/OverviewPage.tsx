import { Link } from "react-router-dom";
import type { ReactNode } from "react";

import { Skeleton } from "../components/states";
import { useEvents, useOrganization, useOrgId, useProjects, useUsers } from "../lib/api/queries";
import type { ApiFailure } from "../lib/api/queries";

/**
 * Organization Overview — the console's dashboard (P1-22, `docs/UI-UX/18`).
 *
 * Its stated objective is that an org admin can tell the organization's
 * operational status **in five seconds of landing**. Everything below follows
 * from that, and the implementation chain for it is in
 * `console/docs/implementation-chain-P1-22.md`.
 *
 * The visual hierarchy is the specification's, in its order: a critical
 * banner only when there is something to say, then three KPI cards, then the
 * activity trend, then settings, then links into detail.
 */
export function OverviewPage() {
  const orgId = useOrgId();
  const organization = useOrganization(orgId);
  const projects = useProjects(orgId);
  const users = useUsers(orgId, "");
  const events = useEvents(orgId, []);

  const invited = users.data?.filter((user) => user.status === "invited").length ?? 0;
  const active = users.data?.filter((user) => user.status === "active").length ?? 0;

  return (
    <>
      <h1 className="text-heading-1 font-medium text-text-primary">
        {organization.isPending ? <Skeleton className="h-7 w-64" /> : (organization.data?.name ?? "Organization")}
      </h1>

      {/*
        The banner renders only when there IS something needing attention.
        docs/UI-UX/14: the absence of a banner is itself information, not a
        missing feature — a permanently present "all clear" strip would train
        people to stop reading the one that matters.
      */}
      <AttentionBanner invited={invited} />

      <section aria-labelledby="kpis" className="mt-5">
        <h2 id="kpis" className="sr-only">
          Key figures
        </h2>

        {/*
          docs/UI-UX/12's grid: 3 columns each at desktop and wide, 2 per row
          at tablet. Below tablet the shell already replaces the layout with
          the "screen too small" message, so there is no single-column case to
          design here.
        */}
        <div className="grid grid-cols-1 gap-4 tablet:grid-cols-2 desktop:grid-cols-3">
          <Kpi
            label="Active users"
            value={active}
            query={users}
            to="/users"
            description="Users who can sign in right now."
          />
          <Kpi
            label="Projects"
            value={projects.data?.length ?? 0}
            query={projects}
            to="/projects"
            description="Containers for applications and roles."
          />
          <Kpi
            label="Pending invites"
            value={invited}
            query={users}
            to="/users?status=invited"
            description="Invitations sent and not yet accepted."
          />
        </div>
      </section>

      <section aria-labelledby="activity" className="mt-6">
        <h2 id="activity" className="text-heading-2 font-medium text-text-primary">
          Recent activity
        </h2>
        <ActivityTrend query={events} />
      </section>
    </>
  );
}

/**
 * A count, and a link only where a real destination exists.
 *
 * `docs/UI-UX/18` is specific: a card is clickable **only** where there is
 * somewhere to go, and anything not clickable must not carry hover or cursor
 * affordance implying it is. So the link is a real anchor when there is a
 * destination and a plain div otherwise — not a div with an onClick, which is
 * the version a keyboard user cannot reach.
 */
function Kpi({
  label,
  value,
  query,
  to,
  description,
}: {
  label: string;
  value: number;
  query: { isPending: boolean; isError: boolean; error: unknown; refetch: () => unknown };
  to?: string;
  description: string;
}) {
  const body = (
    <>
      <span className="text-small text-text-secondary">{label}</span>
      {query.isPending ? (
        // A skeleton matching the number's geometry, so nothing moves when it
        // arrives (docs/UI-UX/09).
        <Skeleton className="mt-1 h-8 w-16" />
      ) : query.isError ? (
        <span className="mt-1 block text-body text-text-secondary">
          {/*
            An inline retry within THIS card, not a full-page error — the rest
            of the dashboard stays usable (docs/UI-UX/18).
          */}
          Could not load.{" "}
          <button
            type="button"
            onClick={(event) => {
              event.preventDefault();
              query.refetch();
            }}
            className="underline"
          >
            Try again
          </button>
        </span>
      ) : (
        <span className="mt-1 block text-heading-1 font-medium text-text-primary">{value}</span>
      )}
      <span className="mt-1 block text-small text-text-secondary">{description}</span>
    </>
  );

  const shell = "block rounded border border-border bg-bg-surface p-4";

  if (to === undefined || query.isPending || query.isError) {
    return <div className={shell}>{body}</div>;
  }
  return (
    <Link to={to} className={`${shell} hover:border-accent`}>
      {body}
    </Link>
  );
}

/**
 * The activity trend.
 *
 * Deliberately **not clickable**: `docs/UI-UX/18` says there is no drill-down
 * destination defined yet, and that it must not carry affordance implying one.
 * A count per day rather than a chart — a sparkline that cannot be read by a
 * screen reader would need a table beside it anyway, so this is the table.
 */
function ActivityTrend({
  query,
}: {
  query: { isPending: boolean; isError: boolean; data?: { occurred_at: string }[] };
}) {
  if (query.isPending) {
    return <Skeleton className="mt-2 h-24 w-full" />;
  }
  if (query.isError) {
    return (
      <p className="mt-2 text-body text-text-secondary">
        The activity trend could not be loaded. The rest of this page is unaffected.
      </p>
    );
  }

  const days = lastSevenDays();
  const counts = days.map((day) => ({
    day,
    count: (query.data ?? []).filter((event) => event.occurred_at.startsWith(day)).length,
  }));
  const total = counts.reduce((sum, entry) => sum + entry.count, 0);

  if (total === 0) {
    return (
      <p className="mt-2 text-body text-text-secondary">
        Nothing has happened in this organization in the last seven days.
      </p>
    );
  }

  const peak = Math.max(...counts.map((entry) => entry.count), 1);

  return (
    <table className="mt-2 w-full text-body">
      <caption className="sr-only">Audit events per day over the last seven days</caption>
      <tbody>
        {counts.map(({ day, count }) => (
          <tr key={day}>
            <th scope="row" className="w-32 py-1 text-left font-normal text-text-secondary">
              {day}
            </th>
            <td className="py-1">
              <span className="flex items-center gap-2">
                <span
                  aria-hidden="true"
                  className="inline-block h-2 rounded bg-accent"
                  /*
                    The width IS the datum: a proportion of the busiest day,
                    computed per render. A token cannot express "37% of the
                    peak", and a class per percentage would be a hundred
                    classes carrying no meaning.
                  */
                  // eslint-disable-next-line local/no-inline-style
                  style={{ width: `${Math.round((count / peak) * 100)}%` }}
                />
                {/* The number, always. The bar is reinforcement (docs/UI-UX/13). */}
                <span className="text-text-primary">{count}</span>
              </span>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function AttentionBanner({ invited }: { invited: number }): ReactNode {
  if (invited === 0) return null;

  return (
    <div
      role="status"
      className="mt-4 rounded border border-warning bg-bg-surface p-4 text-body text-text-primary"
    >
      <strong className="font-medium">{invited} invitation{invited === 1 ? "" : "s"}</strong> have
      been sent and not yet accepted.{" "}
      <Link to="/users?status=invited" className="underline">
        Review them
      </Link>
      .
    </div>
  );
}

function lastSevenDays(): string[] {
  const days: string[] = [];
  for (let offset = 6; offset >= 0; offset -= 1) {
    const date = new Date();
    date.setUTCDate(date.getUTCDate() - offset);
    days.push(date.toISOString().slice(0, 10));
  }
  return days;
}

export type { ApiFailure };
