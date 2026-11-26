import { useState, type ReactNode } from "react";

import { ErrorState, EmptyState, Skeleton, resolveCollectionState } from "./states";

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
  sortDirection?: "ascending" | "descending";
  onSort?: () => void;
}

export interface RowAction<Row> {
  label: string;
  onSelect: (row: Row) => void;
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
  rowActions?: RowAction<Row>[];
  /** Plain-text row name used for the accessible action-group name. */
  rowLabel?: (row: Row) => string;
  pagination?: {
    /** Opaque API cursors; null denotes the first page, undefined means unavailable. */
    previousCursor?: string | null;
    nextCursor?: string;
    onCursorChange: (cursor: string | null) => void;
    busy?: boolean;
  };
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
  rowActions,
  rowLabel,
  pagination,
}: Props<Row>) {
  const [expandedRows, setExpandedRows] = useState<Set<string>>(new Set());
  const state = resolveCollectionState({ loading: status === "loading", failed: status === "error",
    count: rows.length, permitted: !(status === "error" && errorKind === "permission") });
  const hasStaleRows = state === "error" && rows.length > 0;
  if (state === "permission" || (state === "error" && !hasStaleRows)) {
    return <ErrorState kind={errorKind} onRetry={onRetry} />;
  }
  if (state === "empty") {
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
  const optimizeRows = rows.length > 100;
  const hasActions = actions !== undefined || (rowActions?.length ?? 0) > 0;
  const secondaryColumns = columns.filter(
    (column) => column.secondary === true,
  );

  return (
    <div>
      {hasStaleRows ? (
        <div className="mb-3">
          <ErrorState
            kind={errorKind}
            onRetry={onRetry}
            detail="Showing last-known data. Refresh to retry."
          />
        </div>
      ) : null}
      <div className="overflow-x-auto rounded border border-border bg-bg-surface">
        <table
          className="w-full border-collapse text-body"
          aria-rowcount={status === "loading" ? undefined : rows.length + 1}
        >
          {/* The caption names the table for screen readers (docs/UI-UX/13). */}
          <caption className="sr-only">{caption}</caption>

          <thead>
            <tr className="border-b border-border text-left">
              {visible.map((column) => (
                <th
                  key={column.key}
                  scope="col"
                  aria-sort={column.sortDirection}
                  className={`px-4 py-2 text-small font-medium text-text-secondary ${column.secondary === true ? "hidden desktop:table-cell" : ""}`}
                >
                  {column.onSort !== undefined ? (
                    <button
                      type="button"
                      onClick={column.onSort}
                      className="min-h-7 min-w-7 underline-offset-2 hover:underline focus-visible:outline-accent"
                    >
                      {column.header}
                    </button>
                  ) : (
                    column.header
                  )}
                </th>
              ))}
              {hasActions ? (
                <th
                  scope="col"
                  className="px-4 py-2 text-small font-medium text-text-secondary"
                >
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
                  <tr
                    key={`skeleton-${index}`}
                    className="border-b border-border last:border-0"
                  >
                    {visible.map((column) => (
                      <td
                        key={column.key}
                        className={`px-4 py-3 ${column.secondary === true ? "hidden desktop:table-cell" : ""}`}
                      >
                        <Skeleton className="h-4 w-24" />
                      </td>
                    ))}
                    {hasActions ? (
                      <td className="px-4 py-3">
                        <Skeleton className="h-4 w-16" />
                      </td>
                    ) : null}
                  </tr>
                ))
              : rows.map((row) => (
                  <tr
                    key={rowKey(row)}
                    className={`group border-b border-border last:border-0 ${optimizeRows ? "table-render-window" : ""}`}
                    data-render-optimized={optimizeRows ? "true" : undefined}
                  >
                    {visible.map((column) => (
                      <td
                        key={column.key}
                        className={`px-4 py-3 text-text-primary ${column.secondary === true ? "hidden desktop:table-cell" : ""}`}
                      >
                        {column.cell(row)}
                        {column === visible[0] &&
                        secondaryColumns.length > 0 ? (
                          <details
                            className="mt-2 desktop:hidden"
                            onToggle={(event) => {
                              const open = event.currentTarget.open;
                              setExpandedRows((previous) => {
                                const next = new Set(previous);
                                if (open) next.add(rowKey(row));
                                else next.delete(rowKey(row));
                                return next;
                              });
                            }}
                          >
                            <summary className="cursor-pointer text-small text-text-secondary">
                              More details
                            </summary>
                            {expandedRows.has(rowKey(row)) ? (
                              <dl className="mt-2 text-small">
                                {secondaryColumns.map((secondary) => (
                                  <div key={secondary.key}>
                                    <dt className="font-medium">
                                      {secondary.header}
                                    </dt>
                                    <dd>{secondary.cell(row)}</dd>
                                  </div>
                                ))}
                              </dl>
                            ) : null}
                          </details>
                        ) : null}
                      </td>
                    ))}
                    {hasActions ? (
                      <td className="px-4 py-3 text-right">
                        <div
                          role="group"
                          aria-label={`Actions for ${rowLabel?.(row) ?? rowKey(row)}`}
                          className="table-row-actions opacity-0 group-hover:opacity-100 group-focus-within:opacity-100"
                        >
                          {rowActions !== undefined ? (
                            <details>
                              <summary
                                className="inline-flex min-h-7 min-w-7 cursor-pointer items-center justify-center rounded border border-border text-body"
                                aria-label={`Actions for ${rowLabel?.(row) ?? rowKey(row)}`}
                              >
                                <span aria-hidden="true">⋮</span>
                              </summary>
                              <div className="flex flex-col gap-2 rounded border border-border bg-bg-surface p-2">
                                {rowActions.map((action) => (
                                  <button
                                    key={action.label}
                                    type="button"
                                    className="min-h-7 min-w-7 rounded px-3 py-2 text-left text-body hover:bg-bg-base focus-visible:outline-accent"
                                    onClick={() => action.onSelect(row)}
                                  >
                                    {action.label}
                                  </button>
                                ))}
                              </div>
                            </details>
                          ) : (
                            actions?.(row)
                          )}
                        </div>
                      </td>
                    ) : null}
                  </tr>
                ))}
          </tbody>
        </table>
      </div>
      {pagination !== undefined ? (
        <nav
          aria-label="Table pagination"
          className="mt-3 flex items-center justify-end gap-2"
        >
          <button
            type="button"
            className="min-h-7 min-w-7 rounded border border-border bg-bg-surface px-3 py-2 text-small text-text-primary disabled:cursor-not-allowed disabled:opacity-60"
            disabled={
              pagination.previousCursor === undefined ||
              pagination.busy === true
            }
            onClick={() =>
              pagination.onCursorChange(pagination.previousCursor ?? null)
            }
          >
            Previous page
          </button>
          <button
            type="button"
            className="min-h-7 min-w-7 rounded border border-border bg-bg-surface px-3 py-2 text-small text-text-primary disabled:cursor-not-allowed disabled:opacity-60"
            disabled={
              pagination.nextCursor === undefined || pagination.busy === true
            }
            onClick={() =>
              pagination.onCursorChange(pagination.nextCursor ?? null)
            }
          >
            Next page
          </button>
        </nav>
      ) : null}
    </div>
  );
}
