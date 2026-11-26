import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { CollectionStateBoundary, EmptyState } from "./states";
import { Table } from "./Table";

const base = { count: 0, loading: false, failed: false, what: "projects", loadingLabel: "Loading projects",
  skeleton: <span>Project skeleton</span>, onRetry: () => undefined, children: <span>Project content</span> };

describe("shared collection states", () => {
  it("never shows empty results during loading, and definitive failures override loading", () => {
    const view = render(<CollectionStateBoundary {...base} loading />);
    expect(screen.getByText("Project skeleton")).toBeInTheDocument();
    expect(screen.queryByText(/no projects/i)).not.toBeInTheDocument();
    view.rerender(<CollectionStateBoundary {...base} loading failed />);
    expect(screen.getByRole("alert")).toHaveTextContent(/try again/i);
    expect(screen.queryByText("Project skeleton")).not.toBeInTheDocument();
    expect(screen.queryByText(/no projects/i)).not.toBeInTheDocument();
    view.rerender(<CollectionStateBoundary {...base} />);
    expect(screen.getByRole("heading", { name: "No projects yet" })).toBeInTheDocument();
  });

  it("preserves mounted stale content and delegates only the failed request on retry", async () => {
    const onRetry = vi.fn();
    const content = <input aria-label="Local draft" defaultValue="Original" />;
    const view = render(<CollectionStateBoundary {...base} count={1} onRetry={onRetry}>{content}</CollectionStateBoundary>);
    const input = screen.getByRole("textbox");
    await userEvent.type(input, " draft");
    view.rerender(<CollectionStateBoundary {...base} count={1} failed onRetry={onRetry}>{content}</CollectionStateBoundary>);
    expect(screen.getByRole("textbox")).toBe(input);
    expect(input).toHaveValue("Original draft");
    expect(screen.getByRole("alert")).toHaveTextContent(/last-known data/i);
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(onRetry).toHaveBeenCalledOnce();
    expect(input).toHaveValue("Original draft");
  });

  it("offers only filter recovery for a filtered empty result", async () => {
    const onClearFilter = vi.fn();
    render(<EmptyState filtered what="projects" action={<button>Create project</button>} onClearFilter={onClearFilter} />);
    expect(screen.queryByRole("button", { name: "Create project" })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /clear/i }));
    expect(onClearFilter).toHaveBeenCalledOnce();
    expect(screen.queryByText(/there are projects here/i)).not.toBeInTheDocument();
  });

  it("does not disclose cached collection contents after permission denial", () => {
    render(<CollectionStateBoundary {...base} count={1} failed permitted={false}>Confidential project</CollectionStateBoundary>);
    expect(screen.getByRole("alert")).toHaveTextContent(/administrator/i);
    expect(screen.queryByText("Confidential project")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });

  it("hides stale table rows after a permission error", () => {
    render(<Table caption="Projects" columns={[{ key: "name", header: "Name", cell: (row: { name: string }) => row.name }]}
      rows={[{ name: "Confidential project" }]} rowKey={(row) => row.name} status="error" errorKind="permission" what="projects" />);
    expect(screen.queryByText("Confidential project")).not.toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent(/review your access/i);
  });
});
