import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { ChoiceField, MultiValueField, RadioGroupField, SelectField, TextAreaField, TextField, ToggleField } from "./FormField";

describe("form field primitives", () => {
  it("keeps labels visible and replaces helper text with an announced, associated error", () => {
    render(<TextField label="Organization name" helperText="Shown to members" error="Name is required" />);
    const input = screen.getByRole("textbox", { name: "Organization name" });
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(input).toHaveAccessibleDescription("Name is required");
    expect(screen.getByRole("alert")).toHaveTextContent("Name is required");
    expect(screen.queryByText("Shown to members")).not.toBeInTheDocument();
  });

  it("supports textarea, select and checkbox labels", async () => {
    const user = userEvent.setup();
    render(<>
      <TextAreaField label="Description" />
      <SelectField label="Region" defaultValue="us"><option value="us">United States</option></SelectField>
      <ChoiceField label="Require MFA" />
    </>);
    expect(screen.getByRole("textbox", { name: "Description" })).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Region" })).toHaveValue("us");
    const checkbox = screen.getByRole("checkbox", { name: "Require MFA" });
    await user.click(checkbox);
    expect(checkbox).toBeChecked();
  });

  it("gives checkbox and radio controls a 24px minimum target", () => {
    render(<>
      <ChoiceField label="Require MFA" />
      <RadioGroupField label="Region" name="region" options={[{ value: "eu", label: "Europe" }]} onChange={() => undefined} />
    </>);

    expect(screen.getByRole("checkbox", { name: "Require MFA" })).toHaveClass("size-6");
    expect(screen.getByRole("radio", { name: "Europe" })).toHaveClass("size-6");
  });

  it("groups radio options and exposes an inline async validation state", async () => {
    const onChange = vi.fn();
    render(<>
      <RadioGroupField label="Visibility" name="visibility" options={[{ value: "private", label: "Private" }, { value: "public", label: "Public" }]} onChange={onChange} />
      <TextField label="Email" validating />
    </>);
    await userEvent.click(screen.getByRole("radio", { name: "Public" }));
    expect(onChange).toHaveBeenCalledWith("public");
    expect(screen.getByRole("status")).toHaveTextContent("Checking");
    expect(screen.getByRole("textbox", { name: "Email" })).toHaveAccessibleDescription("Checking…");
  });

  it("applies toggles immediately and announces the result", async () => {
    const onChange = vi.fn();
    render(<ToggleField label="Enforce MFA" checked={false} onChange={onChange} />);
    await userEvent.click(screen.getByRole("switch", { name: "Enforce MFA" }));
    expect(onChange).toHaveBeenCalledWith(true);
    expect(screen.getByRole("status")).toHaveTextContent("Enforce MFA enabled");
  });

  it("adds values with Enter and offers a working undo after removal", async () => {
    const user = userEvent.setup();
    function Example() {
      const [values, setValues] = useState(["https://one.example/callback"]);
      return <MultiValueField label="Redirect URI" values={values} onChange={setValues} undoDurationMs={1000} />;
    }
    render(<Example />);
    const input = screen.getByRole("textbox", { name: "Redirect URI" });
    await user.type(input, "https://two.example/callback{Enter}");
    expect(screen.getByText("https://two.example/callback")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Remove https://one.example/callback" }));
    expect(screen.queryByText("https://one.example/callback")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Undo" }));
    expect(screen.getByText("https://one.example/callback")).toBeInTheDocument();
  });
});
