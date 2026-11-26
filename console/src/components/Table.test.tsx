import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { Table } from "./Table";

type Row = { id: string; name: string };

const columns = [{ key: "name", header: "Name", cell: (row: Row) => row.name }];

function renderTable(props: Partial<Parameters<typeof Table<Row>>[0]> = {}) {
  return render(
    <Table<Row>
      caption="Projects"
      columns={columns}
      rows={[]}
      rowKey={(row) => row.id}
      status="ready"
      what="projects"
      {...props}
    />,
  );
}

describe("Table", () => {
  it("keeps last-known rows visible when refresh fails and offers retry", async () => {
    const onRetry = vi.fn();
    renderTable({
      rows: [{ id: "p-1", name: "Billing" }],
      status: "error",
      onRetry,
    });

    expect(screen.getByRole("alert")).toHaveTextContent(/showing last-known data/i);
    expect(screen.getByRole("table", { name: "Projects" })).toHaveTextContent("Billing");
    await userEvent.click(screen.getByRole("button", { name: /try again/i }));
    expect(onRetry).toHaveBeenCalledOnce();
  });

  it("uses a named group for each row's actions", () => {
    renderTable({
      rows: [{ id: "p-1", name: "Billing" }],
      rowLabel: (row) => row.name,
      actions: () => <button type="button">Edit</button>,
    });

    const group = screen.getByRole("group", { name: "Actions for Billing" });
    expect(within(group).getByRole("button", { name: "Edit" })).toBeInTheDocument();
  });

  it("delegates cursor page navigation without deriving offsets", async () => {
    const onCursorChange = vi.fn();
    renderTable({
      rows: [{ id: "p-1", name: "Billing" }],
      pagination: {
        previousCursor: null,
        nextCursor: "opaque-next-page-token",
        onCursorChange,
      },
    });

    const nav = screen.getByRole("navigation", { name: "Table pagination" });
    expect(within(nav).getByRole("button", { name: "Previous page" })).toBeEnabled();
    await userEvent.click(within(nav).getByRole("button", { name: "Next page" }));
    expect(onCursorChange).toHaveBeenCalledExactlyOnceWith("opaque-next-page-token");
    await userEvent.click(within(nav).getByRole("button", { name: "Previous page" }));
    expect(onCursorChange).toHaveBeenLastCalledWith(null);
  });

  it("exposes sort controls and announces the active sort direction", async () => {
    const onSort = vi.fn();
    renderTable({
      columns: [{ ...columns[0], sortDirection: "ascending", onSort }],
      rows: [{ id: "p-1", name: "Billing" }],
    });

    const heading = screen.getByRole("columnheader", { name: "Name" });
    expect(heading).toHaveAttribute("aria-sort", "ascending");
    await userEvent.click(within(heading).getByRole("button", { name: "Name" }));
    expect(onSort).toHaveBeenCalledOnce();
  });

  it("keeps large row sets in the accessible table and optimizes offscreen rendering", () => {
    const rows = Array.from({ length: 150 }, (_, index) => ({ id: `p-${index}`, name: `Project ${index}` }));
    renderTable({ rows });

    const table = screen.getByRole("table", { name: "Projects" });
    expect(table).toHaveAttribute("aria-rowcount", "151");
    expect(screen.getAllByRole("row")).toHaveLength(151);
    expect(screen.getByRole("row", { name: /Project 149/ })).toHaveAttribute("data-render-optimized", "true");
  });

  it("marks secondary columns for tablet-width degradation and mounts details only on demand", async () => {
    renderTable({
      columns: [
        columns[0],
        { key: "owner", header: "Owner", secondary: true, cell: () => "Ari" },
      ],
      rows: [{ id: "p-1", name: "Billing" }],
    });

    expect(screen.getByRole("columnheader", { name: "Owner" }).className).toContain("desktop:table-cell");
    expect(screen.getByText("More details")).toBeInTheDocument();
    expect(screen.getAllByText("Ari")).toHaveLength(1);
    await userEvent.click(screen.getByText("More details"));
    expect(await screen.findByRole("term")).toHaveTextContent("Owner");
  });

  it("offers named keyboard-operable kebab actions", async () => {
    const onSelect = vi.fn();
    renderTable({
      rows: [{ id: "p-1", name: "Billing" }],
      rowLabel: (row) => row.name,
      rowActions: [{ label: "Edit project", onSelect }],
    });

    const group = screen.getByRole("group", { name: "Actions for Billing" });
    const summary = within(group).getByLabelText("Actions for Billing");
    await userEvent.click(summary);
    await userEvent.click(within(group).getByRole("button", { name: "Edit project" }));
    expect(onSelect).toHaveBeenCalledWith({ id: "p-1", name: "Billing" });
  });
});
