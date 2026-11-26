import { createContext, useContext, useEffect, useRef, useState } from "react";
import type { ChangeEvent, ComponentPropsWithRef, FormEvent, ReactNode } from "react";
import { isValidPermissionKey, isValidRoleKey } from "../lib/api/patterns.gen";

type SavePattern = "explicit" | "immediate";
const SavePatternContext = createContext<SavePattern | null>(null);

export function useSavePattern(expected: SavePattern) {
  const actual = useContext(SavePatternContext);
  if (import.meta.env.DEV && actual !== null && actual !== expected) {
    throw new Error(`Cannot use ${expected} saving inside an ${actual} form.`);
  }
}

type FormSystemProps = Omit<ComponentPropsWithRef<"form">, "onSubmit"> & (
  | { pattern: "explicit"; onSubmit: (event: FormEvent<HTMLFormElement>) => void }
  | { pattern: "immediate"; onSubmit?: never }
);

/** Immediate controls call useSavePattern("immediate") before applying changes. */
export function FormSystem({ pattern, children, onSubmit, ...props }: FormSystemProps) {
  const parent = useContext(SavePatternContext);
  if (import.meta.env.DEV && parent !== null) throw new Error("Forms cannot be nested.");
  return <SavePatternContext.Provider value={pattern}>
    <form {...props} noValidate onSubmit={(event) => {
      if (pattern === "immediate") event.preventDefault();
      else onSubmit?.(event);
    }}>{children}</form>
  </SavePatternContext.Provider>;
}

type Errors<T> = Partial<Record<keyof T, string>>;
function serverError(failure: unknown): { message?: string; details: Array<{ field: string; issue: string }> } {
  if (typeof failure !== "object" || failure === null || !("error" in failure)) return { details: [] };
  const error = failure.error;
  if (typeof error !== "object" || error === null) return { details: [] };
  const message = "message" in error && typeof error.message === "string" ? error.message : undefined;
  const details = "details" in error && Array.isArray(error.details) ? error.details.filter(
    (detail): detail is { field: string; issue: string } => typeof detail === "object" && detail !== null
      && "field" in detail && typeof detail.field === "string" && "issue" in detail && typeof detail.issue === "string",
  ) : [];
  return { message, details };
}

export interface FormFieldRule<T> {
  label: string;
  validate?: (value: string, values: T) => string | undefined;
}

/** Adapters reuse the OpenAPI-generated formats already shared with Go. */
export const formValidators = {
  required: (value: string) => value.trim() ? undefined : "Enter a value.",
  roleKey: (value: string) => isValidRoleKey(value) ? undefined : "Enter a valid role key.",
  permissionKey: (value: string) => isValidPermissionKey(value) ? undefined : "Enter a valid permission key.",
};

export function useExplicitForm<T extends Record<string, string>>({ initialValues, fields, onSave }: {
  initialValues: T;
  fields: { [K in keyof T]: FormFieldRule<T> };
  onSave: (values: T) => Promise<void>;
}) {
  const [values, setValues] = useState<T>(initialValues);
  const [errors, setErrors] = useState<Errors<T>>({});
  const [summary, setSummary] = useState("");
  const [saving, setSaving] = useState(false);
  const busy = useRef(false);
  const formRef = useRef<HTMLFormElement>(null);
  const focusField = useRef<string | null>(null);
  const names = Object.keys(fields) as Array<keyof T & string>;

  useEffect(() => {
    if (saving || focusField.current === null) return;
    const element = formRef.current?.elements.namedItem(focusField.current);
    if (element instanceof HTMLElement) element.focus();
    else if (element instanceof RadioNodeList) {
      const first = Array.from(element).find((item) => item instanceof HTMLElement && !(item as HTMLInputElement).disabled);
      if (first instanceof HTMLElement) first.focus();
    }
    focusField.current = null;
  }, [errors, saving]);

  function report(next: Errors<T>, fallback = "") {
    setErrors(next);
    const invalid = names.filter((name) => next[name]);
    setSummary(invalid.length ? `Check ${invalid.length} ${invalid.length === 1 ? "field" : "fields"}: ${invalid.map((name) => fields[name].label).join(", ")}.` : fallback);
    // Focus after rendering: a rejected request must re-enable its controls first.
    focusField.current = invalid[0] ?? null;
    return invalid.length === 0;
  }

  function validate(nextValues: T, selected = names) {
    const next: Errors<T> = {};
    for (const name of selected) {
      const issue = fields[name].validate?.(nextValues[name], nextValues);
      if (issue) next[name] = issue;
    }
    return next;
  }

  function readValues(form: HTMLFormElement): T {
    const data = new FormData(form);
    const next = { ...values };
    for (const name of names) {
      const value = data.get(name);
      if (typeof value === "string") next[name] = value as T[typeof name];
    }
    return next;
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy.current) return;
    // Read the DOM as well as React state: browser autofill may not emit change.
    const next = readValues(event.currentTarget);
    setValues(next);
    if (!report(validate(next))) return;
    busy.current = true;
    setSaving(true);
    setSummary("");
    try {
      await onSave(next);
      setSummary("Changes saved.");
    } catch (failure) {
      const mapped: Errors<T> = {};
      const error = serverError(failure);
      const unmapped: string[] = [];
      for (const detail of error.details) {
        if (Object.prototype.hasOwnProperty.call(fields, detail.field)) {
          const name = detail.field as keyof T;
          mapped[name] = [mapped[name], detail.issue].filter(Boolean).join(" ");
        } else unmapped.push(detail.issue);
      }
      const message = unmapped.join(" ") || error.message || "Changes could not be saved. Your entries are retained. Try again.";
      report(mapped, message);
      if (unmapped.length && Object.keys(mapped).length) setSummary((current) => `${current} ${message}`);
    } finally {
      busy.current = false;
      setSaving(false);
    }
  }

  function field(name: keyof T & string) {
    return {
      name,
      value: values[name],
      error: errors[name],
      disabled: saving,
      onChange: (event: ChangeEvent<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>) => {
        const value = event.currentTarget.value;
        setValues((current) => ({ ...current, [name]: value }));
      },
      onBlur: () => {
        const next = formRef.current ? readValues(formRef.current) : values;
        setValues(next);
        setErrors((current) => ({ ...current, [name]: fields[name].validate?.(next[name], next) }));
      },
    };
  }

  return { values, errors, summary, saving, field, formProps: { ref: formRef, onSubmit: submit },
    reset: () => { setValues(initialValues); setErrors({}); setSummary(""); },
    validate: (selected?: Array<keyof T & string>) => {
      const next = formRef.current ? readValues(formRef.current) : values;
      setValues(next);
      return report(validate(next, selected));
    } };
}

export function FormSummary({ message }: { message: string }) {
  return <p role="status" aria-live="polite" aria-atomic="true" className="text-small text-text-secondary">{message}</p>;
}

/** Keep useExplicitForm mounted above the step views; drafts remain in memory. */
export function FormReview({ entries }: { entries: Array<{ label: string; value: ReactNode }> }) {
  return <section aria-label="Review entered information" className="space-y-3">
    <h3 className="text-heading-3 font-medium">Review before confirming</h3>
    <dl className="space-y-3">{entries.map(({ label, value }) => <div key={label}>
      <dt className="text-small font-medium text-text-secondary">{label}</dt>
      <dd className="break-words text-body text-text-primary">{value || "Not provided"}</dd>
    </div>)}</dl>
  </section>;
}
