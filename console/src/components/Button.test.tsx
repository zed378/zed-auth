import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { Button, ButtonSection } from "./Button";

describe("Button", () => {
  it("provides the three action severities and a visible focus treatment", () => {
    render(
      <>
        <Button variant="primary">Create</Button>
        <Button variant="secondary">Cancel</Button>
        <Button variant="destructive">Revoke</Button>
      </>,
    );

    expect(screen.getByRole("button", { name: "Create" })).toHaveClass("bg-accent", "focus-visible:outline-accent");
    expect(screen.getByRole("button", { name: "Cancel" })).toHaveClass("bg-bg-surface");
    expect(screen.getByRole("button", { name: "Revoke" })).toHaveClass("bg-danger");
  });

  it("surfaces a disabled action reason in its tooltip", () => {
    render(<Button disabled disabledReason="Enter a project name first">Create project</Button>);

    const button = screen.getByRole("button", { name: "Create project" });
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute("title", "Enter a project name first");
  });

  it("keeps its label and width while loading and prevents repeat activation", async () => {
    const onClick = vi.fn();
    render(<Button loading onClick={onClick}>Save changes</Button>);

    const button = screen.getByRole("button", { name: "Save changes" });
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute("aria-busy", "true");
    expect(button.querySelector(".invisible")).toHaveTextContent("Save changes");
    await userEvent.click(button);
    expect(onClick).not.toHaveBeenCalled();
  });

  it("warns when a section contains more than one primary action", () => {
    const warning = vi.spyOn(console, "warn").mockImplementation(() => undefined);
    render(
      <ButtonSection>
        <Button variant="primary">Create</Button>
        <Button variant="primary">Save</Button>
      </ButtonSection>,
    );

    expect(warning).toHaveBeenCalledWith("ButtonSection should contain at most one primary button.");
    warning.mockRestore();
  });

  it("does not warn for a single primary action", () => {
    const warning = vi.spyOn(console, "warn").mockImplementation(() => undefined);
    render(
      <ButtonSection>
        <Button variant="primary">Create</Button>
        <Button>Cancel</Button>
      </ButtonSection>,
    );

    expect(warning).not.toHaveBeenCalled();
    warning.mockRestore();
  });
});
