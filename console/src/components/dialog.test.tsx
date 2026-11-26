import { useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { Modal } from "./Modal";
import { SidePanel } from "./SidePanel";
import { ConfirmDialog } from "./ConfirmDialog";
import { expectNoAxeViolations } from "../test/axe";

for (const Component of [Modal, SidePanel]) {
  describe(Component.name, () => {
    function Example() {
      const [open, setOpen] = useState(false);
      const [value, setValue] = useState("");
      return <><button onClick={() => setOpen(true)}>Open</button>
        <Component open={open} title="Edit project" onClose={() => setOpen(false)}>
          <label>Name<input value={value} onChange={e => setValue(e.target.value)} /></label>
          <button disabled>Unavailable</button><button>Last action</button>
        </Component></>;
    }
    it("keeps focus while typing through rerenders, contains Tab, and restores the trigger on Escape", async () => {
      const user = userEvent.setup();
      render(<Example />);
      const trigger = screen.getByRole("button", { name: "Open" });
      await user.click(trigger);
      expect(screen.getByRole("dialog")).toHaveFocus();
      const input = screen.getByRole("textbox", { name: "Name" });
      await user.type(input, "project");
      expect(input).toHaveFocus();
      expect(input).toHaveValue("project");
      await user.tab();
      expect(screen.getByRole("button", { name: "Last action" })).toHaveFocus();
      await user.tab();
      expect(screen.getByRole("button", { name: /^Close/ })).toHaveFocus();
      await user.tab({ shift: true });
      expect(screen.getByRole("button", { name: "Last action" })).toHaveFocus();
      await user.keyboard("{Escape}");
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
      expect(trigger).toHaveFocus();
    });
    it("locks scrolling until close and provides a visible close control", async () => {
      const user = userEvent.setup();
      const previous = document.body.style.overflow;
      render(<Example />);
      await user.click(screen.getByText("Open"));
      expect(document.body.style.overflow).toBe("hidden");
      await user.click(screen.getByRole("button", { name: /^Close/ }));
      expect(document.body.style.overflow).toBe(previous);
    });
    it("labels simultaneous dialogs uniquely and only dismisses the top dialog", () => {
      const outer = vi.fn(), inner = vi.fn();
      render(<><Component open title="First" onClose={outer}>First content</Component>
        <Component open title="Second" onClose={inner}>Second content</Component></>);
      expect(screen.getByRole("dialog", { name: "First" })).toHaveAccessibleName("First");
      expect(screen.getByRole("dialog", { name: "Second" })).toHaveAccessibleName("Second");
      fireEvent.keyDown(document, { key: "Escape" });
      expect(inner).toHaveBeenCalledOnce();
      expect(outer).not.toHaveBeenCalled();
    });
    it("has no axe violations", async () => {
      const { container } = render(<Component open title="Edit project" onClose={() => undefined}><label>Name<input /></label></Component>);
      await expectNoAxeViolations(container);
    });
  });
}

it("keeps a non-dismissible secret modal inside the focus boundary with no tab stops", () => {
  const close = vi.fn();
  render(<Modal open title="Secret" dismissible={false} onClose={close}>Read the secret</Modal>);
  const dialog = screen.getByRole("dialog");
  fireEvent.keyDown(document, { key: "Tab" });
  expect(dialog).toHaveFocus();
  fireEvent.keyDown(document, { key: "Escape" });
  expect(close).not.toHaveBeenCalled();
  expect(screen.queryByRole("button", { name: "Close dialog" })).not.toBeInTheDocument();
});

it("rejects destructive confirmations in a side panel", () => {
  expect(() => render(<SidePanel open title="Revoke" purpose="destructive-confirmation" onClose={() => undefined}>Access removal</SidePanel>)).toThrow(/must use Modal/);
});

it("renders destructive confirmations without entrance motion", () => {
  render(<ConfirmDialog open title="Revoke?" consequence="Access is removed." verb="Revoke" onCancel={() => undefined} onConfirm={() => undefined} />);
  expect(screen.getByRole("dialog")).toHaveAttribute("data-confirm-dialog", "true");
  expect(screen.getByRole("dialog")).not.toHaveClass("motion-modal");
});
