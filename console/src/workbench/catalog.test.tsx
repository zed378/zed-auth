import { render, screen, cleanup } from "@testing-library/react";
import { catalog } from "./catalog";

describe("component workbench", () => {
  it("includes each component family specified in docs/UI-UX/07", () => {
    const required = ["Button", "Table", "Form Field", "Modal / Side Panel", "Badge / Tag", "Confirmation Dialog", "Breadcrumb", "Search Input", "Responsive Grid"];
    const names = catalog.map((entry) => entry.name);
    expect(new Set(names).size).toBe(names.length);
    for (const name of required) expect(names).toContain(name);
  });

  for (const entry of catalog) {
    describe(entry.name, () => {
      it("documents the usage rule and each registered required state", () => {
        expect(entry.rule.trim().length).toBeGreaterThan(0);
        const states = entry.examples.map((example) => example.name);
        expect(new Set(states).size).toBe(states.length);
        for (const state of entry.requiredStates) expect(states).toContain(state);
      });
      for (const example of entry.examples) {
        it(`renders ${example.name} without an API provider`, () => {
          const { container } = render(example.render("fixture-description"));
          expect(container).not.toBeEmptyDOMElement();
          cleanup();
        });
      }
    });
  }

  it("explains why the disabled button is unavailable", () => {
    const example = catalog.find((entry) => entry.name === "Button")!.examples.find((entry) => entry.name === "disabled")!;
    render(example.render("disabled-description"));
    expect(screen.getByRole("button", { name: "Save changes" })).toHaveAccessibleDescription("Change a value before saving.");
  });
});
