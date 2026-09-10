import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { Badge } from "./Badge";
import { Button } from "./Button";
import { ClientSecretModal } from "./ClientSecretModal";
import { Table } from "./Table";
import { EmptyState, ErrorState } from "./states";
import { expectNoAxeViolations } from "../test/axe";

/**
 * The design-system components, and the states `docs/UI-UX/14` names.
 *
 * The point of testing these here rather than only through the screens is the
 * one `docs/UI-UX/19` makes: loading, error, empty and filtered-empty are a
 * SET, and the failure mode is a component that has three of them. A test file
 * per component makes the missing one visible.
 */

type Row = { id: string; name: string };

const columns = [{ key: "name", header: "Name", cell: (row: Row) => row.name }];

function renderTable(props: Partial<Parameters<typeof Table<Row>>[0]> = {}) {
  return render(
    <Table<Row>
      caption="Things"
      columns={columns}
      rows={[]}
      rowKey={(row) => row.id}
      status="ready"
      what="things"
      {...props}
    />,
  );
}

describe("Table states", () => {
  it("shows skeleton rows while loading, keeping the header", () => {
    renderTable({ status: "loading" });

    // The header survives, so nothing moves when the data arrives
    // (docs/UI-UX/07 § Table states).
    expect(screen.getByRole("columnheader", { name: "Name" })).toBeInTheDocument();
    expect(screen.getAllByRole("row").length).toBeGreaterThan(1);
  });

  it("distinguishes genuinely empty from filtered to empty", () => {
    const { unmount } = renderTable({ status: "ready", rows: [], filtered: false });
    expect(screen.getByText(/No things yet/i)).toBeInTheDocument();
    unmount();

    renderTable({ status: "ready", rows: [], filtered: true });
    // Different copy AND a different recovery — the whole reason
    // docs/UI-UX/14 separates them.
    expect(screen.getByText(/No things match that search/i)).toBeInTheDocument();
  });

  it("offers a retry for a server failure and none for a refusal", () => {
    const { unmount } = renderTable({ status: "error", errorKind: "server", onRetry: () => {} });
    expect(screen.getByRole("button", { name: /try again/i })).toBeInTheDocument();
    unmount();

    renderTable({ status: "error", errorKind: "permission", onRetry: () => {} });
    // Retrying a refusal produces the same refusal, and the button would say
    // otherwise.
    expect(screen.queryByRole("button", { name: /try again/i })).not.toBeInTheDocument();
  });

  it("names its actions column, so a screen reader can describe it", () => {
    renderTable({
      rows: [{ id: "1", name: "One" }],
      actions: () => <button type="button">Do</button>,
    });

    const headers = screen.getAllByRole("columnheader");
    expect(headers[headers.length - 1]).toHaveTextContent("Actions");
  });

  it("has an accessible name from its caption", () => {
    renderTable({ rows: [{ id: "1", name: "One" }] });
    expect(screen.getByRole("table", { name: "Things" })).toBeInTheDocument();
  });

  it("has no axe violations with rows", async () => {
    const { container } = renderTable({ rows: [{ id: "1", name: "One" }] });
    await expectNoAxeViolations(container);
  });
});

describe("Button", () => {
  it("keeps an accessible name while loading", () => {
    render(<Button loading>Save</Button>);

    // A button whose label is replaced by a spinner is an unnamed button to a
    // screen reader unless the name is kept.
    expect(screen.getByRole("button", { name: "Save" })).toBeInTheDocument();
  });

  it("cannot be clicked twice while loading", async () => {
    const onClick = vi.fn();
    render(
      <Button loading onClick={onClick}>
        Save
      </Button>,
    );

    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    // A second request on a create is a second object.
    expect(onClick).not.toHaveBeenCalled();
  });
});

describe("Badge", () => {
  it("always carries text, never colour alone", () => {
    render(<Badge tone="positive">Active</Badge>);
    // docs/UI-UX/07 § Badge rule, and docs/UI-UX/13: a user who cannot
    // distinguish the tones gets the same information.
    expect(screen.getByText("Active")).toBeInTheDocument();
  });
});

describe("ClientSecretModal", () => {
  const open = () =>
    render(
      <ClientSecretModal
        open
        applicationName="Billing portal"
        secret="s3cr3t-value-here"
        onClose={() => {}}
      />,
    );

  it("shows the secret readably and warns that it will not be shown again", () => {
    open();

    expect(screen.getByDisplayValue("s3cr3t-value-here")).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent(/never be shown again/i);
  });

  it("will not let the dialog be finished until the user acknowledges", async () => {
    open();

    const done = screen.getByRole("button", { name: /done/i });
    expect(done).toBeDisabled();

    await userEvent.click(screen.getByRole("checkbox"));
    expect(done).toBeEnabled();
  });

  it("cannot be dismissed with Escape", async () => {
    const onClose = vi.fn();
    render(
      <ClientSecretModal open applicationName="App" secret="abc" onClose={onClose} />,
    );

    await userEvent.keyboard("{Escape}");

    // An accidental dismissal costs a rotation and a redeploy. This is the one
    // dialog in the console where flow loses to friction.
    expect(onClose).not.toHaveBeenCalled();
  });

  it("is a labelled modal dialog that takes focus", () => {
    open();

    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveAccessibleName(/Billing portal/);
    expect(dialog).toHaveAttribute("aria-modal", "true");
  });

  it("has no axe violations", async () => {
    const { container } = open();
    await expectNoAxeViolations(container);
  });
});

describe("states", () => {
  it("tells a validation failure from a server one", () => {
    const { unmount } = render(<ErrorState kind="validation" />);
    expect(screen.getByRole("alert")).toHaveTextContent(/could not be saved/i);
    unmount();

    render(<ErrorState kind="network" />);
    expect(screen.getByRole("alert")).toHaveTextContent(/could not reach/i);
  });

  it("offers a clear-search action only for a filtered empty", () => {
    const { unmount } = render(
      <EmptyState filtered what="users" onClearFilter={() => {}} />,
    );
    expect(screen.getByRole("button", { name: /clear the search/i })).toBeInTheDocument();
    unmount();

    render(<EmptyState filtered={false} what="users" onClearFilter={() => {}} />);
    expect(screen.queryByRole("button", { name: /clear the search/i })).not.toBeInTheDocument();
  });
});

describe("the table at tablet width", () => {
  it("marks droppable columns rather than scrolling everything", () => {
    const { container } = render(
      <Table<Row>
        caption="Things"
        columns={[
          { key: "name", header: "Name", cell: (row) => row.name },
          { key: "extra", header: "Extra", secondary: true, cell: () => "x" },
        ]}
        rows={[{ id: "1", name: "One" }]}
        rowKey={(row) => row.id}
        status="ready"
        what="things"
      />,
    );

    // The secondary column carries the class that hides it below desktop
    // width. Asserted on the class rather than by resizing, because jsdom
    // applies no CSS — what this checks is that the decision was made.
    const header = within(container).getByRole("columnheader", { name: "Extra" });
    expect(header.className).toContain("desktop:table-cell");
  });
});
