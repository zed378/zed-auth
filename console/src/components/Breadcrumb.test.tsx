import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, useLocation } from "react-router-dom";
import { describe, expect, it } from "vitest";
import { Breadcrumb, ConsoleBreadcrumb } from "./Breadcrumb";
import { expectNoAxeViolations } from "../test/axe";

function Location() {
  return <output aria-label="Current route">{useLocation().pathname}</output>;
}

describe("Breadcrumb", () => {
  it("keeps long names accessible and every segment navigable", async () => {
    const user = userEvent.setup();
    const name = "The organization with a very long but identifiable name";
    const { container } = render(<MemoryRouter initialEntries={["/projects/pos/roles"]}>
      <Breadcrumb items={[{ label: name, to: "/" }, { label: "POS Project", to: "/projects/pos" }, { label: "Roles", to: "/projects/pos/roles" }]} />
      <Location />
    </MemoryRouter>);
    const links = within(screen.getByRole("navigation", { name: "Breadcrumb" })).getAllByRole("link");
    expect(links[0]).toHaveAccessibleName(name);
    expect(links[0]).toHaveAttribute("title", name);
    expect(links[2]).toHaveAttribute("aria-current", "page");
    expect(links[0]).not.toHaveAttribute("aria-current");
    await expectNoAxeViolations(container);
    await user.click(links[1]);
    expect(screen.getByLabelText("Current route")).toHaveTextContent("/projects/pos");
    await user.click(links[0]);
    expect(screen.getByLabelText("Current route")).toHaveTextContent("/");
  });

  it.each([
    ["/projects/pos", ["Acme Org", "Projects", "POS Project", "Applications"]],
    ["/projects/pos/roles", ["Acme Org", "Projects", "POS Project", "Roles"]],
    ["/projects/pos/grants", ["Acme Org", "Projects", "POS Project", "Project Grants"]],
    ["/granted-projects", ["Acme Org", "Granted Projects"]],
    ["/users/jane", ["Acme Org", "Users", "Jane Doe"]],
  ])("derives the hierarchy from %s", (path, names) => {
    render(<MemoryRouter initialEntries={[path]}><ConsoleBreadcrumb organizationName="Acme Org" projectName="POS Project" userName="Jane Doe" /></MemoryRouter>);
    expect(screen.getAllByRole("link").map(link => link.textContent)).toEqual(names);
  });

  it("follows route changes without retaining a previous tab", async () => {
    const user = userEvent.setup();
    render(<MemoryRouter initialEntries={["/projects/pos/roles"]}><ConsoleBreadcrumb organizationName="Acme Org" projectName="POS Project" /></MemoryRouter>);
    await user.click(screen.getByRole("link", { name: "POS Project" }));
    expect(screen.queryByRole("link", { name: "Roles" })).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Applications" })).toHaveAttribute("aria-current", "page");
  });

  it.each(["/unknown", "/constructor", "/projects/pos/unknown", "/projects/pos/toString", "/projects/pos/roles/unknown", "/users/jane/unknown"])("does not invent hierarchy for %s", path => {
    render(<MemoryRouter initialEntries={[path]}><ConsoleBreadcrumb organizationName="Acme Org" /></MemoryRouter>);
    expect(screen.queryByRole("navigation")).not.toBeInTheDocument();
  });
});
