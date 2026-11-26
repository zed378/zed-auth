import { useState } from "react";
import { Button } from "../components/Button";
import { TextField } from "../components/FormField";
import { FormReview, FormSummary, FormSystem, formValidators, useExplicitForm } from "../components/FormSystem";
import { StepIndicator } from "../components/Supporting";
import type { ComponentEntry } from "./catalog";

function FormExample({ serverFailure = false, wizard = false }: { serverFailure?: boolean; wizard?: boolean }) {
  const [step, setStep] = useState(0);
  const form = useExplicitForm({
    initialValues: { name: "", role_key: "" },
    fields: {
      name: { label: "Display name", validate: formValidators.required },
      role_key: { label: "Role key", validate: formValidators.roleKey },
    },
    onSave: async () => {
      if (serverFailure) throw { error: { code: "VALIDATION_ERROR", message: "This role key is unavailable.", details: [{ field: "role_key", issue: "This role key is reserved." }] } };
    },
  });
  return <FormSystem {...form.formProps} pattern="explicit" className="space-y-4" onSubmit={(event) => {
    if (wizard && step === 0) { event.preventDefault(); if (form.validate()) setStep(1); }
    else void form.formProps.onSubmit(event);
  }}>
    {wizard && <StepIndicator steps={["Details", "Review"]} current={step} onBack={setStep} />}
    {step === 0 ? <>
      <TextField label="Display name" {...form.field("name")} helperText="A name your team recognizes." />
      <TextField label="Role key" {...form.field("role_key")} helperText="Lowercase letters, numbers, hyphens and underscores." />
    </> : <FormReview entries={Object.entries(form.values).map(([key, value]) => ({ label: key === "name" ? "Display name" : "Role key", value }))} />}
    <FormSummary message={form.summary} />
    <div className="flex gap-3">
      {step === 1 && <Button onClick={() => setStep(0)} disabled={form.saving} disabledReason="Wait for saving to finish.">Back</Button>}
      <Button type="submit" variant="primary" loading={form.saving}>{wizard && step === 0 ? "Review details" : wizard ? "Confirm details" : "Save example"}</Button>
    </div>
    <p className="text-small text-text-secondary">Development fixture. No API request is made.</p>
  </FormSystem>;
}

export const formEntry: ComponentEntry = {
  name: "Form System",
  rule: "Validate on blur and revalidate every field on submit. Map API detail paths to the matching field, focus the first invalid input, preserve drafts on failure, and review all entered values before confirming a multi-step form. Use one save pattern per form.",
  requiredStates: ["blur-and-submit", "server-field-error", "multi-step-review"],
  examples: [
    { name: "blur-and-submit", render: () => <FormExample /> },
    { name: "server-field-error", render: () => <FormExample serverFailure /> },
    { name: "multi-step-review", render: () => <FormExample wizard /> },
  ],
};
