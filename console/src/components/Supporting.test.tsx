import { act, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, useLocation, useNavigate } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CopyButton, InlineErrorBanner, InlineSuccess, KpiCard, PaginationFooter, StepIndicator, Tabs, Toast } from "./Supporting";
import { expectNoAxeViolations } from "../test/axe";

function Location() {
  const location = useLocation();
  const navigate = useNavigate();
  return <><output aria-label="URL">{location.search}</output><button onClick={() => navigate(-1)}>Browser back</button></>;
}

afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks(); });

describe("supporting components", () => {
  it("navigates enabled tabs with keys and browser history, preserves other query parameters and draft panels", async () => {
    const user = userEvent.setup();
    render(<MemoryRouter initialEntries={["/?organization=acme&tab=one"]}><Tabs label="Details" items={[
      { key: "one", label: "One", content: <input aria-label="Draft" /> },
      { key: "disabled", label: "Unavailable", content: "Unavailable", disabled: true },
      { key: "two", label: "Two", content: "Second panel" },
    ]} /><Location /></MemoryRouter>);
    await user.type(screen.getByLabelText("Draft"), "retained");
    screen.getByRole("tab", { name: "One" }).focus();
    await user.keyboard("{ArrowRight}");
    expect(screen.getByRole("tab", { name: "Two" })).toHaveFocus();
    expect(screen.getByRole("tab", { name: "Two" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByLabelText("URL")).toHaveTextContent("organization=acme&tab=two");
    await user.click(screen.getByRole("button", { name: "Browser back" }));
    expect(screen.getByLabelText("Draft")).toHaveValue("retained");
    expect(screen.getByRole("tab", { name: "One" })).toHaveAttribute("aria-selected", "true");
    screen.getByRole("tab", { name: "One" }).focus();
    await user.keyboard("{End}");
    expect(screen.getByRole("tab", { name: "Two" })).toHaveFocus();
    await user.keyboard("{Home}{ArrowLeft}");
    expect(screen.getByRole("tab", { name: "Two" })).toHaveFocus();
  });

  it("only offers previous steps and identifies the current step", async () => {
    const onBack = vi.fn();
    render(<StepIndicator steps={["Details", "Review", "Complete"]} current={1} onBack={onBack} />);
    expect(screen.getByText("Step 2 of 3")).toBeInTheDocument();
    expect(screen.getByText("2. Review").closest("li")).toHaveAttribute("aria-current", "step");
    await userEvent.click(screen.getByRole("button", { name: "1. Details" }));
    expect(onBack).toHaveBeenCalledWith(0);
    expect(screen.queryByRole("button", { name: /Complete/ })).not.toBeInTheDocument();
  });

  it("reports clipboard failure, then actual success, without exposing the value", async () => {
    const writeText = vi.fn().mockRejectedValueOnce(new Error("denied")).mockResolvedValueOnce(undefined);
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    render(<CopyButton value="one-time-secret" />);
    fireEvent.click(screen.getByRole("button", { name: "Copy" }));
    expect(await screen.findByText(/Could not copy/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Copy" }));
    expect(await screen.findByRole("button", { name: "✓ Copied" })).toBeInTheDocument();
    expect(writeText).toHaveBeenLastCalledWith("one-time-secret");
    expect(screen.queryByText("one-time-secret")).not.toBeInTheDocument();
  });

  it("does not announce a stale copy after its value changes", async () => {
    let complete!: () => void;
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: vi.fn(() => new Promise<void>((resolve) => { complete = resolve; })) } });
    const { rerender } = render(<CopyButton value="old" />);
    fireEvent.click(screen.getByRole("button", { name: "Copy" }));
    rerender(<CopyButton value="new" />);
    await act(async () => complete());
    expect(screen.getByRole("button", { name: "Copy" })).toBeInTheDocument();
    expect(screen.getByRole("status")).toBeEmptyDOMElement();
  });

  it("pauses automatic toast dismissal on hover and keyboard focus", () => {
    vi.useFakeTimers();
    const dismiss = vi.fn();
    render(<Toast onDismiss={dismiss}>Saved preferences.</Toast>);
    const container = screen.getByRole("status").parentElement!;
    fireEvent.mouseEnter(container);
    act(() => vi.advanceTimersByTime(20000));
    expect(dismiss).not.toHaveBeenCalled();
    fireEvent.focus(screen.getByRole("button", { name: "Dismiss notification" }));
    fireEvent.mouseLeave(container);
    act(() => vi.advanceTimersByTime(20000));
    expect(dismiss).not.toHaveBeenCalled();
    fireEvent.blur(screen.getByRole("button", { name: "Dismiss notification" }));
    act(() => vi.advanceTimersByTime(10000));
    expect(dismiss).toHaveBeenCalledOnce();
  });

  it("passes opaque cursors, including an empty first cursor, and blocks concurrent paging", async () => {
    const onPage = vi.fn();
    const { rerender } = render(<PaginationFooter previousCursor="" nextCursor="opaque+/=" onPage={onPage} count={20} />);
    await userEvent.click(screen.getByRole("button", { name: "Previous" }));
    expect(onPage).toHaveBeenLastCalledWith("");
    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(onPage).toHaveBeenLastCalledWith("opaque+/=");
    rerender(<PaginationFooter nextCursor="next" onPage={onPage} loading />);
    expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
  });

  it("keeps a static KPI inert and only links a card with a destination", () => {
    const { rerender } = render(<KpiCard label="Users" value={12} />);
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
    expect(screen.getByText("Users").parentElement?.className).not.toMatch(/hover:|cursor-/);
    rerender(<MemoryRouter><KpiCard label="Users" value={12} to="/users" /></MemoryRouter>);
    expect(screen.getByRole("link")).toHaveAttribute("href", "/users");
  });

  it("retries only through its supplied callback and disables repeat requests", async () => {
    const onRetry = vi.fn();
    const { rerender } = render(<InlineErrorBanner message="Could not load users." onRetry={onRetry} />);
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(onRetry).toHaveBeenCalledOnce();
    rerender(<InlineErrorBanner message="Could not load users." onRetry={onRetry} retrying />);
    expect(screen.getByRole("button", { name: "Try again" })).toBeDisabled();
  });

  it("exposes accessible tab panels, progress, feedback, cards and pagination", async () => {
    const { container } = render(<MemoryRouter><Tabs label="Details" items={[{ key: "one", label: "One", content: "First panel" }]} />
      <StepIndicator steps={["Details", "Review"]} current={0} /><InlineSuccess>Saved.</InlineSuccess>
      <KpiCard label="Users" value={12} /><PaginationFooter count={0} onPage={() => {}} />
      <InlineErrorBanner message="Could not load users." onRetry={() => {}} /></MemoryRouter>);
    await expectNoAxeViolations(container);
  });
});
