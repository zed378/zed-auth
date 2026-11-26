import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { Button } from "./Button";
import { TextField, ToggleField } from "./FormField";
import { FormReview, FormSummary, FormSystem, formValidators, useExplicitForm } from "./FormSystem";

function Example({ onSave, wizard = false }: { onSave: (values: { name: string; key: string }) => Promise<void>; wizard?: boolean }) {
  const [step, setStep] = useState(0);
  const form = useExplicitForm({
    initialValues: { name: "", key: "" },
    fields: { name: { label: "Name", validate: formValidators.required }, key: { label: "Key", validate: formValidators.roleKey } },
    onSave,
  });
  return <FormSystem {...form.formProps} aria-label="Example form" pattern="explicit" onSubmit={(event) => {
    if (wizard && step === 0) { event.preventDefault(); if (form.validate()) setStep(1); }
    else void form.formProps.onSubmit(event);
  }}>
    {step === 0 ? <><TextField label="Name" {...form.field("name")} /><TextField label="Key" {...form.field("key")} /></>
      : <><FormReview entries={[{ label: "Name", value: form.values.name }, { label: "Key", value: form.values.key }]} />
        <Button onClick={() => setStep(0)}>Back</Button></>}
    <FormSummary message={form.summary} />
    <Button type="submit">{wizard && step === 0 ? "Review" : "Save"}</Button>
  </FormSystem>;
}

describe("form system", () => {
  it("rejects an apply-on-change toggle in an explicit-save form during development", () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => undefined);
    try {
      expect(() => render(<FormSystem pattern="explicit" onSubmit={() => undefined}>
        <ToggleField label="Require MFA" checked={false} onChange={() => undefined} />
      </FormSystem>)).toThrow("Cannot use immediate saving inside an explicit form.");
    } finally { consoleError.mockRestore(); }
  });

  it("waits for blur and validates untouched fields on submit", async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    render(<Example onSave={onSave} />);
    const user = userEvent.setup();
    const name = screen.getByLabelText("Name");
    await user.type(name, "Team");
    await user.clear(name);
    expect(name).toHaveAttribute("aria-invalid", "false");
    await user.tab();
    expect(name).toHaveAttribute("aria-invalid", "true");
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(screen.getByLabelText("Key")).toHaveAttribute("aria-invalid", "true");
    expect(name).toHaveFocus();
    expect(screen.getByRole("status")).toHaveTextContent("Check 2 fields: Name, Key.");
    expect(onSave).not.toHaveBeenCalled();
  });

  it("reads autofilled DOM values even without a React change event", async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    render(<Example onSave={onSave} />);
    (screen.getByLabelText("Name") as HTMLInputElement).value = "Autofilled team";
    (screen.getByLabelText("Key") as HTMLInputElement).value = "team_admin";
    fireEvent.submit(screen.getByRole("form", { name: "Example form" }));
    await waitFor(() => expect(onSave).toHaveBeenCalledWith({ name: "Autofilled team", key: "team_admin" }));
    expect(screen.getByRole("status")).toHaveTextContent("Changes saved.");
  });

  it("maps server field paths, combines issues and focuses the field", async () => {
    const onSave = vi.fn().mockRejectedValue({ error: { message: "Invalid fields", details: [
      { field: "key", issue: "This key is reserved." }, { field: "key", issue: "Choose a different key." },
    ] } });
    render(<Example onSave={onSave} />);
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Name"), "Team");
    await user.type(screen.getByLabelText("Key"), "org_admin");
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(screen.getByLabelText("Key")).toHaveAccessibleDescription("This key is reserved. Choose a different key."));
    expect(screen.getByLabelText("Key")).toHaveFocus();
    expect(screen.getByLabelText("Name")).toHaveValue("Team");
    expect(screen.getByRole("status")).toHaveTextContent("Check 1 field: Key.");
  });

  it.each([new Error("offline"), null, { error: { details: [null, { field: "name" }] } }])(
    "preserves inputs and offers retry for network or malformed failures (%s)", async (failure) => {
      const onSave = vi.fn().mockRejectedValueOnce(failure).mockResolvedValue(undefined);
      render(<Example onSave={onSave} />);
      const user = userEvent.setup();
      await user.type(screen.getByLabelText("Name"), "Keep this draft");
      await user.type(screen.getByLabelText("Key"), "team_admin");
      await user.click(screen.getByRole("button", { name: "Save" }));
      await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("Your entries are retained. Try again."));
      expect(screen.getByLabelText("Name")).toHaveValue("Keep this draft");
      expect(screen.getByLabelText("Key")).toHaveValue("team_admin");
      await user.click(screen.getByRole("button", { name: "Save" }));
      await waitFor(() => expect(onSave).toHaveBeenCalledTimes(2));
    },
  );

  it("retains all step values on back-navigation and reviews them before saving", async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    render(<Example onSave={onSave} wizard />);
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Name"), "Team");
    await user.type(screen.getByLabelText("Key"), "team_admin");
    await user.click(screen.getByRole("button", { name: "Review" }));
    expect(screen.getByRole("region", { name: "Review entered information" })).toHaveTextContent("Team");
    expect(screen.getByRole("region", { name: "Review entered information" })).toHaveTextContent("team_admin");
    expect(onSave).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Back" }));
    expect(screen.getByLabelText("Name")).toHaveValue("Team");
    expect(screen.getByLabelText("Key")).toHaveValue("team_admin");
    await user.click(screen.getByRole("button", { name: "Review" }));
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(onSave).toHaveBeenCalledWith({ name: "Team", key: "team_admin" }));
  });
});
