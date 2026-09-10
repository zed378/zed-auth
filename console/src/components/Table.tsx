import type { ReactNode } from "react";

import { ErrorState, EmptyState, Skeleton } from "./states";

/**
 * The one table, per `docs/UI-UX/07` § Table and `docs/UI-UX/08`
 * § Cross-Screen Requirements: **no screen invents its own.**
 *
 * That rule is not tidiness. A console is mostly tables, and a user who learns
 * how one behaves — where the actions are, what a loading row looks like, what
 * happens when a filter matches nothing — has learned all of them. Two tables
 * with two answers means neither can be learned.
 *
 * Every state the specification names is handled HERE rather than by each
 * caller, which is what stops a screen from having a designed loading state
 * and an improvised empty one.
 */

export interface Column<Row> {
  key: string;
  header: string;
  /** Rendered for each row. */
  cell: (row: Row) => ReactNode;
  /**
   * A column that may be dropped at tablet width.
   *
   * `docs/UI-UX/12` supports the console down to 768px, and a table with eight
   * columns at that width is a horizontal scrollbar nobody uses. Dropping the
   * least load-bearing columns keeps the row readable; which ones are
   * droppable is a decision per screen rather than per breakpoint.
   */
  secondary?: boolean;
}

interface Props<Row> {
  caption: string;
  columns: Column<Row>[];
  rows: Row[];
  rowKey: (row: Row) => string;

  status: "loading" | "error" | "ready";
  errorKind?: "network" | "server" | "permission" | "validation";
  onRetry?: () => void;

  /** Whether a filter is active, so "empty" can say which kind it is. */
  filtered?: boolean;
  /** Plural noun for the empty state: "projects", "users". */
  what: string;
  onClearFilter?: () => void;
  emptyAction?: ReactNode;

  /** Rendered at the end of each row, as the row's actions. */
  actions?: (row: Row) => ReactNode;
}

export function Table<Row>({
  caption,
  columns,
  rows,
  rowKey,
  status,
  errorKind = "server",
  onRetry,
  filtered = false,
  what,
  onClearFilter,
  emptyAction,
  actions,
}: Props<Row>) {
  if (status === "error") {
    // An inline banner INSTEAD of the table, rather than over it. The
    // specification asks for last-known-good data where there is some; there
    // is none on a first load, and a blank table under an error banner reads
    // as "there are none" (docs/UI-UX/14).
    return <ErrorState kind={errorKind} onRetry={onRetry} />;
  }

  if (status === "ready" && rows.length === 0) {
    return (
      <EmptyState
        filtered={filtered}
        what={what}
        onClearFilter={onClearFilter}
        action={emptyAction}
      />
    );
  }

  const visible = columns;

  return (
    <div className="overflow-x-auto rounded border border-border bg-bg-surface">
      <table className="w-full border-collapse text-body">
        {/*
          A caption, not a visually hidden heading. It is the table's
          accessible name, it is what a screen reader announces on entry, and
          it is the difference between "table with 5 rows" and "Projects,
          table with 5 rows" (docs/UI-UX/13).
        */}
        <caption className="sr-only">{caption}</caption>

        <thead>
          <tr className="border-b border-border text-left">
            {visible.map((column) => (
              <th
                key={column.key}
                scope="col"
                className={`px-4 py-2 text-small font-medium text-text-secondary ${
                  column.secondary === true ? "hidden desktop:table-cell" : ""
                }`}
              >
                {column.header}
              </th>
            ))}
            {actions !== undefined ? (
              <th scope="col" className="px-4 py-2 text-small font-medium text-text-secondary">
                <span className="sr-only">Actions</span>
              </th>
            ) : null}
          </tr>
        </thead>

        <tbody>
          {status === "loading"
            ? // Skeleton ROWS, not a spinner over the table. The header stays,
              // the shape stays, and nothing moves when the data arrives
              // (docs/UI-UX/07 § Table states).
              Array.from({ length: 3 }, (_, index) => (
                <tr key={`skeleton-${index}`} className="border-b border-border last:border-0">
                  {visible.map((column) => (
                    <td
                      key={column.key}
                      className={`px-4 py-3 ${column.secondary === true ? "hidden desktop:table-cell" : ""}`}
                    >
                      <Skeleton className="h-4 w-24" />
                    </td>
                  ))}
                  {actions !== undefined ? (
                    <td className="px-4 py-3">
                      <Skeleton className="h-4 w-16" />
                    </td>
                  ) : null}
                </tr>
              ))
            : rows.map((row) => (
                <tr key={rowKey(row)} className="border-b border-border last:border-0">
                  {visible.map((column) => (
                    <td
                      key={column.key}
                      className={`px-4 py-3 text-text-primary ${
                        column.secondary === true ? "hidden desktop:table-cell" : ""
                      }`}
                    >
                      {column.cell(row)}
                    </td>
                  ))}
                  {actions !== undefined ? (
                    <td className="px-4 py-3 text-right">{actions(row)}</td>
                  ) : null}
                </tr>
              ))}
        </tbody>
      </table>
    </div>
  );
}
