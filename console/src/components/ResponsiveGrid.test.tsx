import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { GridRegion, ResponsiveGrid } from "./ResponsiveGrid";

describe("responsive grid contract", () => {
  it("preserves labelled content and explicit per-breakpoint region occupancy", () => {
    render(<ResponsiveGrid aria-label="Page regions">
      <GridRegion columns={{ mobile: 4, tablet: 8, desktop: 8, wide: 9 }} aria-label="Primary region">Details</GridRegion>
      <GridRegion columns={{ mobile: 4, tablet: 8, desktop: 4, wide: 3 }} aria-label="Supporting region">Activity</GridRegion>
    </ResponsiveGrid>);
    const primary = screen.getByLabelText("Primary region");
    expect(primary).toHaveTextContent("Details");
    expect(screen.getByLabelText("Page regions")).toHaveClass("grid-cols-4", "tablet:grid-cols-8", "desktop:grid-cols-12", "gap-4", "wide:gap-5");
    expect(primary).toHaveClass("col-span-4", "tablet:col-span-8", "desktop:col-span-8", "wide:col-span-9", "min-w-0");
    expect(screen.getByLabelText("Supporting region")).toHaveClass("col-span-4", "tablet:col-span-8", "desktop:col-span-4", "wide:col-span-3");
  });

  it("rejects a runtime region wider than its breakpoint grid", () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => undefined);
    try {
      // @ts-expect-error Assert protection for JavaScript consumers bypassing the typed spans.
      expect(() => render(<GridRegion columns={{ mobile: 8, tablet: 8, desktop: 12, wide: 12 }}>Invalid</GridRegion>)).toThrow("Invalid mobile grid span");
    } finally { consoleError.mockRestore(); }
  });
});
