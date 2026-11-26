import { act, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { MemoryRouter, useLocation, useNavigate } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SearchInput, ScopedSearchInput } from "./SearchInput";
import { expectNoAxeViolations } from "../test/axe";

afterEach(() => { vi.useRealTimers(); });

function Controlled({ onSearch }: { onSearch: (query: string) => void }) {
  const [query, setQuery] = useState("");
  return <SearchInput label="Search projects" query={query} onQueryChange={setQuery} onSearch={onSearch} />;
}

function Location() {
  const navigate = useNavigate();
  return <><output aria-label="URL">{useLocation().search}</output><button onClick={() => navigate("?q=back&status=active")}>Back view</button></>;
}

describe("SearchInput", () => {
  it("debounces rapid edits and cancels a pending query when unmounted", () => {
    vi.useFakeTimers();
    const onSearch = vi.fn();
    const { unmount } = render(<Controlled onSearch={onSearch} />);
    fireEvent.change(screen.getByRole("searchbox"), { target: { value: "a" } });
    act(() => vi.advanceTimersByTime(100));
    fireEvent.change(screen.getByRole("searchbox"), { target: { value: "acme" } });
    act(() => vi.advanceTimersByTime(249));
    expect(onSearch).not.toHaveBeenCalled();
    act(() => vi.advanceTimersByTime(1));
    expect(onSearch).toHaveBeenCalledExactlyOnceWith("acme");
    fireEvent.change(screen.getByRole("searchbox"), { target: { value: "later" } });
    unmount();
    act(() => vi.runAllTimers());
    expect(onSearch).toHaveBeenCalledTimes(1);
  });

  it("waits for IME composition to finish before querying", () => {
    vi.useFakeTimers();
    const onSearch = vi.fn();
    render(<Controlled onSearch={onSearch} />);
    const input = screen.getByRole("searchbox");
    fireEvent.compositionStart(input);
    fireEvent.change(input, { target: { value: "東京" } });
    act(() => vi.advanceTimersByTime(500));
    expect(onSearch).not.toHaveBeenCalled();
    fireEvent.compositionEnd(input);
    act(() => vi.advanceTimersByTime(250));
    expect(onSearch).toHaveBeenCalledExactlyOnceWith("東京");
  });

  it("delays loading feedback, cancels fast feedback and suppresses stale counts", () => {
    vi.useFakeTimers();
    const props = { label: "Search", query: "a", onQueryChange: vi.fn(), onSearch: vi.fn() };
    const { rerender } = render(<SearchInput {...props} loading />);
    act(() => vi.advanceTimersByTime(100));
    expect(screen.getByRole("status")).toBeEmptyDOMElement();
    rerender(<SearchInput {...props} results={{ query: "a", count: 2 }} />);
    act(() => vi.advanceTimersByTime(200));
    expect(screen.getByRole("status")).toHaveTextContent("2 results");
    rerender(<SearchInput {...props} query="b" loading results={{ query: "a", count: 2 }} />);
    act(() => vi.advanceTimersByTime(199));
    expect(screen.getByRole("status")).toBeEmptyDOMElement();
    act(() => vi.advanceTimersByTime(1));
    expect(screen.getByRole("status")).toHaveTextContent("Searching…");
    rerender(<SearchInput {...props} query="b" results={{ query: "a", count: 2 }} />);
    expect(screen.getByRole("status")).toBeEmptyDOMElement();
    rerender(<SearchInput {...props} query="b" results={{ query: "b", count: 0 }} />);
    expect(screen.getByRole("status")).toHaveTextContent("0 results");
  });

  it("focuses global search with Ctrl+K and Command+K and removes the shortcut on unmount", () => {
    const { unmount } = render(<SearchInput label="Search everywhere" variant="global" query="Acme" onQueryChange={vi.fn()} onSearch={vi.fn()} />);
    const input = screen.getByRole("searchbox", { name: "Search everywhere" });
    fireEvent.keyDown(window, { key: "k", ctrlKey: true });
    expect(input).toHaveFocus();
    (input as HTMLInputElement).blur();
    fireEvent.keyDown(window, { key: "K", metaKey: true });
    expect(input).toHaveFocus();
    unmount();
    const event = new KeyboardEvent("keydown", { key: "k", ctrlKey: true, cancelable: true });
    window.dispatchEvent(event);
    expect(event.defaultPrevented).toBe(false);
  });

  it("restores scoped queries from the URL and preserves unrelated filters", () => {
    vi.useFakeTimers();
    const onSearch = vi.fn();
    const { unmount } = render(<MemoryRouter initialEntries={["/?q=stored&status=active"]}>
      <ScopedSearchInput label="Search users" onSearch={onSearch} /><Location />
    </MemoryRouter>);
    expect(screen.getByRole("searchbox")).toHaveValue("stored");
    act(() => vi.advanceTimersByTime(250));
    expect(onSearch).toHaveBeenLastCalledWith("stored");
    fireEvent.change(screen.getByRole("searchbox"), { target: { value: "jane" } });
    expect(screen.getByLabelText("URL")).toHaveTextContent("?q=jane&status=active");
    fireEvent.click(screen.getByRole("button", { name: "Back view" }));
    expect(screen.getByRole("searchbox")).toHaveValue("back");
    fireEvent.change(screen.getByRole("searchbox"), { target: { value: "" } });
    expect(screen.getByLabelText("URL")).toHaveTextContent("?status=active");
    unmount();
    render(<MemoryRouter initialEntries={["/?q=jane&status=active"]}><ScopedSearchInput label="Search users" onSearch={onSearch} /></MemoryRouter>);
    expect(screen.getByRole("searchbox")).toHaveValue("jane");
  });

  it("labels both variants and exposes accessible feedback", async () => {
    const { container } = render(<SearchInput variant="global" label="Search console" query="" onQueryChange={vi.fn()} onSearch={vi.fn()} results={{ query: "", count: 1 }} />);
    expect(screen.getByRole("searchbox")).toHaveAccessibleDescription("Ctrl / ⌘ K");
    expect(screen.getByRole("status")).toHaveTextContent("1 result");
    await expectNoAxeViolations(container);
  });
});
