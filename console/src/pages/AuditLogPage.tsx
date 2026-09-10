import { useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { Button } from "../components/Button";
import { Table } from "../components/Table";
import type { Column } from "../components/Table";
import { api, queryKeys } from "../lib/api/client";
import { useOrgId } from "../lib/api/queries";

type Event = {
  id: string;
  event_type: string;
  actor_user_id?: string | null;
  occurred_at: string;
  ip?: string | null;
  request_id?: string | null;
  payload?: Record<string, unknown>;
};

/**
 * The Audit Log (P1-24, `docs/UI-UX/08` § Audit Log).
 *
 * Deliberately basic. `docs/PLAN/17`'s Phase 1 criterion is that logins appear
 * with the correct actor and timestamp; filtering polish and export are Phase
 * 5, and building them now would be work nobody asked for on a screen whose
 * shape will change once somebody has used it in an incident.
 */
export function AuditLogPage() {
  const orgId = useOrgId();
  const [type, setType] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [cursors, setCursors] = useState<string[]>([]);
  const [expanded, setExpanded] = useState<string | null>(null);

  const cursor = cursors[cursors.length - 1];

  const events = useQuery({
    queryKey: [...queryKeys.events, orgId, type, from, to, cursor ?? ""],
    enabled: orgId !== null,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/organizations/{org_id}/events", {
        params: {
          path: { org_id: orgId as string },
          query: {
            page_size: 25,
            ...(type === "" ? {} : { event_type: [type] }),
            ...(from === "" ? {} : { from: `${from}T00:00:00Z` }),
            // Exclusive upper bound on the server, so "to 2026-09-10" means
            // the whole of that day rather than nothing on it.
            ...(to === "" ? {} : { to: `${to}T23:59:59Z` }),
            ...(cursor === undefined ? {} : { page_token: cursor }),
          },
        },
      });
      if (error !== undefined) throw error;
      return data;
    },
  });

  const rows = (events.data?.events ?? []) as Event[];
  const nextToken = events.data?.page_info?.next_page_token;
  const filtered = type !== "" || from !== "" || to !== "";

  const columns: Column<Event>[] = [
    {
      key: "when",
      header: "When",
      cell: (event) => (
        // The viewer's timezone for reading, the UTC value for correlating.
        // P1-24 step 5: incident timelines are reconstructed across
        // timezones, so the underlying value has to stay inspectable — the
        // `title` and the `dateTime` both carry it.
        <time dateTime={event.occurred_at} title={event.occurred_at} className="whitespace-nowrap">
          {new Date(event.occurred_at).toLocaleString()}
        </time>
      ),
    },
    { key: "type", header: "Event", cell: (event) => <code>{event.event_type}</code> },
    {
      key: "actor",
      header: "Actor",
      cell: (event) =>
        event.actor_user_id === null || event.actor_user_id === undefined ? (
          // Not "unknown" and not blank. A failed login against an address
          // that does not exist genuinely has no actor, and saying so is more
          // useful than implying the record is incomplete (P1-20).
          <span className="text-text-secondary">None — no authenticated actor</span>
        ) : (
          <span className="font-mono text-small">{event.actor_user_id}</span>
        ),
    },
    {
      key: "ip",
      header: "From",
      secondary: true,
      cell: (event) => <span className="font-mono text-small">{event.ip ?? "—"}</span>,
    },
  ];

  return (
    <>
      <h1 className="text-heading-1 font-medium text-text-primary">Audit log</h1>
      <p className="mt-1 max-w-prose text-body text-text-secondary">
        Every identity- and permission-changing action in this organization, newest first. The log
        is append-only: nothing here can be edited or removed.
      </p>

      <div className="mt-4 flex flex-wrap items-end gap-3">
        <div>
          <label htmlFor="event-type" className="block text-small text-text-secondary">
            Event type
          </label>
          <select
            id="event-type"
            value={type}
            onChange={(event) => {
              setType(event.currentTarget.value);
              setCursors([]);
            }}
            className="mt-1 rounded border border-border bg-bg-surface px-3 py-2 text-body text-text-primary"
          >
            <option value="">All events</option>
            <option value="user.login.success">Sign-in succeeded</option>
            <option value="user.login.failed">Sign-in failed</option>
            <option value="user.created">User created</option>
            <option value="user.deactivated">User deactivated</option>
            <option value="application.created">Application registered</option>
            <option value="application.secret_rotated">Client secret rotated</option>
            <option value="project.created">Project created</option>
          </select>
        </div>

        <div>
          <label htmlFor="event-from" className="block text-small text-text-secondary">
            From
          </label>
          <input
            id="event-from"
            type="date"
            value={from}
            onChange={(event) => {
              setFrom(event.currentTarget.value);
              setCursors([]);
            }}
            className="mt-1 rounded border border-border bg-bg-surface px-3 py-2 text-body text-text-primary"
          />
        </div>

        <div>
          <label htmlFor="event-to" className="block text-small text-text-secondary">
            To
          </label>
          <input
            id="event-to"
            type="date"
            value={to}
            onChange={(event) => {
              setTo(event.currentTarget.value);
              setCursors([]);
            }}
            className="mt-1 rounded border border-border bg-bg-surface px-3 py-2 text-body text-text-primary"
          />
        </div>
      </div>

      <div className="mt-4">
        <Table<Event>
          caption="Audit events, newest first"
          columns={columns}
          rows={rows}
          rowKey={(event) => event.id}
          status={events.isPending ? "loading" : events.isError ? "error" : "ready"}
          errorKind={kindOf(events.error)}
          onRetry={() => void events.refetch()}
          what="events"
          filtered={filtered}
          onClearFilter={() => {
            setType("");
            setFrom("");
            setTo("");
            setCursors([]);
          }}
          actions={(event) => (
            <button
              type="button"
              aria-expanded={expanded === event.id}
              aria-controls={`detail-${event.id}`}
              onClick={() => setExpanded(expanded === event.id ? null : event.id)}
              className="text-accent underline"
            >
              {expanded === event.id ? "Hide detail" : "Detail"}
            </button>
          )}
        />
      </div>

      {expanded !== null ? (
        <EventDetail event={rows.find((row) => row.id === expanded)} />
      ) : null}

      {/*
        Cursor pagination against P1-20. Forward only, with a stack so "back"
        returns to the exact page rather than re-querying from the start —
        a keyset cursor has no page numbers, and pretending otherwise would
        mean an offset query on a table with no ceiling.
      */}
      <div className="mt-4 flex gap-2">
        <Button disabled={cursors.length === 0} onClick={() => setCursors(cursors.slice(0, -1))}>
          Previous
        </Button>
        <Button
          disabled={nextToken === undefined || nextToken === null}
          onClick={() => setCursors([...cursors, nextToken as string])}
        >
          Next
        </Button>
      </div>
    </>
  );
}

/**
 * The redacted payload.
 *
 * `P1-24` step 3: the console must never render a field the API should not
 * have returned in the first place. It does not try to filter — filtering here
 * would be a second redaction policy that drifts from the real one — it
 * renders what arrived, which `P0-12` redacted before storage.
 */
function EventDetail({ event }: { event: Event | undefined }) {
  if (event === undefined) return null;

  return (
    <section
      id={`detail-${event.id}`}
      aria-label={`Detail for ${event.event_type}`}
      className="mt-4 rounded border border-border bg-bg-surface p-4"
    >
      <h2 className="text-heading-3 font-medium text-text-primary">{event.event_type}</h2>

      <dl className="mt-2 grid grid-cols-1 gap-2 tablet:grid-cols-2">
        <div>
          <dt className="text-small text-text-secondary">When (UTC)</dt>
          <dd className="font-mono text-small text-text-primary">{event.occurred_at}</dd>
        </div>
        <div>
          <dt className="text-small text-text-secondary">Request</dt>
          <dd className="font-mono text-small text-text-primary">{event.request_id ?? "—"}</dd>
        </div>
      </dl>

      <h3 className="mt-3 text-small font-medium text-text-secondary">Payload</h3>
      <pre className="mt-1 overflow-x-auto rounded bg-bg-base p-3 font-mono text-small text-text-primary">
        {JSON.stringify(event.payload ?? {}, null, 2)}
      </pre>
      <p className="mt-1 text-small text-text-secondary">
        Redacted before storage. Tokens, passwords and raw authorization attributes never reach the
        log.
      </p>
    </section>
  );
}

function kindOf(error: unknown): "network" | "server" | "permission" | "validation" {
  const failure = error as { kind?: "network" | "server" | "permission" | "validation" } | null;
  return failure?.kind ?? "server";
}
