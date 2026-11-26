import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ConfirmDialog } from "./ConfirmDialog";
import { expectNoAxeViolations } from "../test/axe";

const base = { open: true, title: "Revoke grant?", verb: "Revoke", consequence: "Partner users lose project access.", onCancel: () => undefined, onConfirm: () => undefined };

describe("ConfirmDialog", () => {
  it.each(["OK", "Confirm", " confirm "])("rejects the generic verb %s", verb => {
    expect(() => render(<ConfirmDialog {...base} verb={verb} />)).toThrow(/action verb/);
  });

  it("requires a meaningful consequence", () => {
    expect(() => render(<ConfirmDialog {...base} consequence="" />)).toThrow(/consequence/);
  });

  it("requires exact typed confirmation and resets after cancel/reopen", async () => {
    const user = userEvent.setup(), confirm = vi.fn();
    const view = render(<ConfirmDialog {...base} typeToConfirm="Acme" onConfirm={confirm} />);
    const input = screen.getByRole("textbox");
    expect(input).toHaveFocus();
    await user.type(input, "acme");
    expect(screen.getByRole("button", { name: "Revoke" })).toBeDisabled();
    await user.clear(input);
    await user.type(input, "Acme");
    await user.click(screen.getByRole("button", { name: "Revoke" }));
    expect(confirm).toHaveBeenCalledOnce();
    expect(input).toHaveValue("");
    await user.type(input, "Acme");
    view.rerender(<ConfirmDialog {...base} open={false} typeToConfirm="Acme" />);
    view.rerender(<ConfirmDialog {...base} typeToConfirm="Acme" />);
    expect(screen.getByRole("textbox")).toHaveValue("");
    expect(screen.getByRole("button", { name: "Revoke" })).toBeDisabled();
  });

  it("hides stale impact counts and blocks confirmation during refresh or failure", async () => {
    const confirm = vi.fn(), retry = vi.fn();
    const view = render(<ConfirmDialog {...base} consequence="14 users lose access." consequenceLoading onConfirm={confirm} />);
    expect(screen.queryByText("14 users lose access.")).not.toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("Loading current consequences.");
    fireEvent.click(screen.getByRole("button", { name: "Revoke" }));
    expect(confirm).not.toHaveBeenCalled();
    view.rerender(<ConfirmDialog {...base} consequenceError="Could not load affected users." onRetryConsequence={retry} />);
    expect(screen.getByRole("button", { name: "Revoke" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Retry consequences" }));
    expect(retry).toHaveBeenCalledOnce();
    view.rerender(<ConfirmDialog {...base} consequence="19 users lose access." onConfirm={confirm} />);
    expect(screen.getByText("19 users lose access.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Revoke" })).toBeEnabled();
    await expectNoAxeViolations(view.container);
  });

  it("blocks duplicate confirmation and dismissal while the mutation is pending", () => {
    const confirm = vi.fn(), cancel = vi.fn();
    render(<ConfirmDialog {...base} busy onConfirm={confirm} onCancel={cancel} />);
    fireEvent.click(screen.getByRole("button", { name: "Revoke" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    fireEvent.keyDown(document, { key: "Escape" });
    expect(confirm).not.toHaveBeenCalled();
    expect(cancel).not.toHaveBeenCalled();
  });

  it("announces completion and offers acknowledgement without another destructive action", async () => {
    const done = vi.fn();
    const { container } = render(<ConfirmDialog {...base} success="Grant revoked. Partner access has ended." onCancel={done} />);
    expect(screen.getByRole("status")).toHaveTextContent("Grant revoked. Partner access has ended.");
    expect(screen.queryByRole("button", { name: "Revoke" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(done).toHaveBeenCalledOnce();
    await expectNoAxeViolations(container);
  });

  it("gives each typed field a unique label association", () => {
    render(<><ConfirmDialog {...base} typeToConfirm="First" /><ConfirmDialog {...base} typeToConfirm="Second" /></>);
    const first = screen.getByRole("textbox", { name: "Type First to confirm" });
    const second = screen.getByRole("textbox", { name: "Type Second to confirm" });
    expect(first.id).not.toBe(second.id);
  });
});
