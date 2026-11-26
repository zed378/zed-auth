import { useEffect, useId, useRef, useState } from "react";
import type { InputHTMLAttributes, ReactNode, SelectHTMLAttributes, TextareaHTMLAttributes } from "react";
import { useSavePattern } from "./FormSystem";

interface FieldProps {
  id?: string;
  label: string;
  helperText?: string;
  error?: string;
  validating?: boolean;
  children: (ids: { id: string; describedBy?: string }) => ReactNode;
}

export function FormField({ id: suppliedId, label, helperText, error, validating = false, children }: FieldProps) {
  const generatedId = useId();
  const id = suppliedId ?? generatedId;
  const detailId = `${id}-detail`;
  const detail = error ?? helperText;
  const describedBy = [detail ? detailId : undefined, validating ? `${id}-validating` : undefined]
    .filter(Boolean)
    .join(" ") || undefined;

  return (
    <div className="grid gap-1.5">
      <label htmlFor={id} className="text-small font-medium text-text-primary">{label}</label>
      {children({ id, describedBy })}
      {detail ? (
        <p id={detailId} className={`text-small ${error ? "text-danger" : "text-text-secondary"}`}>
          {detail}
        </p>
      ) : null}
      {validating ? (
        <span id={`${id}-validating`} role="status" className="text-small text-text-secondary">
          Checking…
        </span>
      ) : null}
      {error ? <span className="sr-only" role="alert">{error}</span> : null}
    </div>
  );
}

type TextFieldProps = Omit<InputHTMLAttributes<HTMLInputElement>, "id" | "type"> & {
  id?: string;
  label: string;
  helperText?: string;
  error?: string;
  validating?: boolean;
};

const controlClass = "min-h-11 w-full rounded border border-border bg-bg-surface px-3 py-2 text-body text-text-primary focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent disabled:cursor-not-allowed disabled:opacity-60";

export function TextField({ id, label, helperText, error, validating, ...inputProps }: TextFieldProps) {
  return (
    <FormField id={id} label={label} helperText={helperText} error={error} validating={validating}>
      {({ id: fieldId, describedBy }) => (
        <input {...inputProps} id={fieldId} type="text" className={controlClass} aria-invalid={Boolean(error)} aria-describedby={describedBy} />
      )}
    </FormField>
  );
}

type TextAreaFieldProps = Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, "id"> & {
  id?: string;
  label: string;
  helperText?: string;
  error?: string;
  validating?: boolean;
};

export function TextAreaField({ id, label, helperText, error, validating, ...inputProps }: TextAreaFieldProps) {
  return (
    <FormField id={id} label={label} helperText={helperText} error={error} validating={validating}>
      {({ id: fieldId, describedBy }) => (
        <textarea {...inputProps} id={fieldId} className={`${controlClass} min-h-24`} aria-invalid={Boolean(error)} aria-describedby={describedBy} />
      )}
    </FormField>
  );
}

type SelectFieldProps = Omit<SelectHTMLAttributes<HTMLSelectElement>, "id"> & {
  id?: string;
  label: string;
  helperText?: string;
  error?: string;
  validating?: boolean;
  children: ReactNode;
};

export function SelectField({ id, label, helperText, error, validating, children, ...inputProps }: SelectFieldProps) {
  return (
    <FormField id={id} label={label} helperText={helperText} error={error} validating={validating}>
      {({ id: fieldId, describedBy }) => (
        <select {...inputProps} id={fieldId} className={controlClass} aria-invalid={Boolean(error)} aria-describedby={describedBy}>{children}</select>
      )}
    </FormField>
  );
}

interface ChoiceProps extends Omit<InputHTMLAttributes<HTMLInputElement>, "id" | "type"> {
  id?: string;
  label: string;
  helperText?: string;
  error?: string;
  type?: "checkbox" | "radio";
}

export function ChoiceField({ id: suppliedId, label, helperText, error, type = "checkbox", ...inputProps }: ChoiceProps) {
  const generatedId = useId();
  const id = suppliedId ?? generatedId;
  const detailId = `${id}-detail`;
  return (
    <div className="grid min-h-11 grid-cols-12 items-center gap-x-3">
      <input {...inputProps} id={id} type={type} className="size-6 accent-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent" aria-invalid={Boolean(error)} aria-describedby={error || helperText ? detailId : undefined} />
      <label htmlFor={id} className="text-body text-text-primary">{label}</label>
      {error || helperText ? <p id={detailId} className={`col-start-2 text-small ${error ? "text-danger" : "text-text-secondary"}`}>{error ?? helperText}</p> : null}
      {error ? <span role="alert" className="sr-only">{error}</span> : null}
    </div>
  );
}

export interface RadioOption {
  value: string;
  label: string;
  disabled?: boolean;
}

interface RadioGroupFieldProps {
  id?: string;
  label: string;
  name: string;
  value?: string;
  onChange: (value: string) => void;
  options: RadioOption[];
  helperText?: string;
  error?: string;
}

export function RadioGroupField({ id: suppliedId, label, name, value, onChange, options, helperText, error }: RadioGroupFieldProps) {
  const generatedId = useId();
  const id = suppliedId ?? generatedId;
  const detailId = `${id}-detail`;
  return (
    <fieldset className="grid gap-2" aria-describedby={helperText || error ? detailId : undefined} aria-invalid={Boolean(error)}>
      <legend className="text-small font-medium text-text-primary">{label}</legend>
      {options.map((option) => {
        const optionId = `${id}-${option.value}`;
        return <div key={option.value} className="flex min-h-11 items-center gap-3">
          <input id={optionId} type="radio" name={name} value={option.value} checked={value === option.value} disabled={option.disabled} onChange={() => onChange(option.value)} className="size-6 accent-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent" />
          <label htmlFor={optionId} className="text-body text-text-primary">{option.label}</label>
        </div>;
      })}
      {helperText || error ? <p id={detailId} className={`text-small ${error ? "text-danger" : "text-text-secondary"}`}>{error ?? helperText}</p> : null}
      {error ? <span role="alert" className="sr-only">{error}</span> : null}
    </fieldset>
  );
}

interface ToggleFieldProps {
  id?: string;
  label: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
  helperText?: string;
  disabled?: boolean;
}

export function ToggleField({ id: suppliedId, label, checked, onChange, helperText, disabled }: ToggleFieldProps) {
  useSavePattern("immediate");
  const generatedId = useId();
  const id = suppliedId ?? generatedId;
  const [confirmation, setConfirmation] = useState("");

  function change(next: boolean) {
    onChange(next);
    setConfirmation(`${label} ${next ? "enabled" : "disabled"}.`);
  }

  return (
    <div className="grid gap-1.5">
      <div className="flex min-h-11 items-center justify-between gap-4">
        <label htmlFor={id} className="text-body font-medium text-text-primary">{label}</label>
        <button id={id} type="button" role="switch" aria-checked={checked} aria-label={label} aria-describedby={helperText ? `${id}-help` : undefined} disabled={disabled} onClick={() => change(!checked)} className="relative h-7 w-12 shrink-0 rounded-full border border-border bg-bg-base p-1 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:cursor-not-allowed disabled:opacity-60">
          <span aria-hidden="true" className={`block size-4 rounded-full bg-text-secondary transition-transform ${checked ? "translate-x-5 bg-accent" : "translate-x-0"}`} />
        </button>
      </div>
      {helperText ? <p id={`${id}-help`} className="text-small text-text-secondary">{helperText}</p> : null}
      <p role="status" aria-live="polite" className="min-h-5 text-small text-text-secondary">{confirmation}</p>
    </div>
  );
}

interface MultiValueFieldProps {
  id?: string;
  label: string;
  values: string[];
  onChange: (values: string[]) => void;
  helperText?: string;
  error?: string;
  placeholder?: string;
  addLabel?: string;
  undoDurationMs?: number;
}

export function MultiValueField({ id: suppliedId, label, values, onChange, helperText, error, placeholder, addLabel = "Add", undoDurationMs = 5000 }: MultiValueFieldProps) {
  const generatedId = useId();
  const id = suppliedId ?? generatedId;
  const [draft, setDraft] = useState("");
  const [removed, setRemoved] = useState<{ value: string; index: number } | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const detailId = `${id}-detail`;

  useEffect(() => () => clearTimeout(timer.current), []);

  function add() {
    const value = draft.trim();
    if (!value || values.includes(value)) return;
    onChange([...values, value]);
    setDraft("");
  }

  function remove(index: number) {
    const value = values[index];
    onChange(values.filter((_, itemIndex) => itemIndex !== index));
    setRemoved({ value, index });
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setRemoved(null), undoDurationMs);
  }

  function undo() {
    if (!removed) return;
    const next = [...values];
    next.splice(Math.min(removed.index, next.length), 0, removed.value);
    onChange(next);
    setRemoved(null);
    clearTimeout(timer.current);
  }

  return (
    <div className="grid gap-2">
      <label htmlFor={id} className="text-small font-medium text-text-primary">{label}</label>
      {values.length ? <ul className="grid gap-1" aria-label={`${label} values`}>
        {values.map((value, index) => <li key={`${value}-${index}`} className="flex min-h-11 items-center justify-between gap-3 rounded border border-border px-3">
          <span className="break-all text-body">{value}</span>
          <button type="button" className="min-h-11 px-2 text-small text-danger underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent" aria-label={`Remove ${value}`} onClick={() => remove(index)}>Remove</button>
        </li>)}
      </ul> : null}
      <div className="flex flex-wrap gap-2">
        <input id={id} value={draft} onChange={(event) => setDraft(event.currentTarget.value)} onKeyDown={(event) => { if (event.key === "Enter") { event.preventDefault(); add(); } }} placeholder={placeholder} className={controlClass} aria-invalid={Boolean(error)} aria-describedby={error || helperText ? detailId : undefined} />
        <button type="button" onClick={add} disabled={!draft.trim() || values.includes(draft.trim())} className="min-h-11 rounded border border-border px-3 text-body focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent disabled:cursor-not-allowed disabled:opacity-60">{addLabel}</button>
      </div>
      {error || helperText ? <p id={detailId} className={`text-small ${error ? "text-danger" : "text-text-secondary"}`}>{error ?? helperText}</p> : null}
      {error ? <span role="alert" className="sr-only">{error}</span> : null}
      <div className="min-h-6" aria-live="polite">
        {removed ? <p className="text-small text-text-secondary">Removed {removed.value}. <button type="button" onClick={undo} className="font-medium underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent">Undo</button></p> : null}
      </div>
    </div>
  );
}
