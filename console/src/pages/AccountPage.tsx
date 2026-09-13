import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";

import { Badge } from "../components/Badge";
import { Button } from "../components/Button";
import { ErrorState, Skeleton } from "../components/states";
import { api } from "../lib/api/client";
import { ApiFailure } from "../lib/api/queries";
import { useOrgId } from "../lib/api/queries";
import { useAuth } from "../lib/auth/AuthProvider";
import { MyFactors } from "./MfaTab";
import { SessionsTab } from "./SessionsTab";

/**
 * Personal account settings (P3-12, `docs/UI-UX/08`).
 *
 * The self-service surface — and the **one** console screen built for a phone,
 * because an end user, unlike an administrator, reasonably opens it on one
 * (`docs/UI-UX/16`). It renders outside the console's narrow-screen guard for
 * that reason, and every section is a single column that works at 360px.
 *
 * Everything here goes through `/v1/me*`, where the user comes from the token.
 * A member with no administrative role manages their own account here; the
 * organization's user routes would refuse them.
 */
export function AccountPage() {
  const orgId = useOrgId();
  const { claims } = useAuth();

  const me = useQuery({
    queryKey: ["me", orgId],
    enabled: orgId !== null,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/me");
      if (error !== undefined) {
        const envelope = error as { error?: { code?: string; message?: string } };
        throw new ApiFailure(envelope.error?.code ?? "SERVER_ERROR", envelope.error?.message ?? "The request failed.");
      }
      return data;
    },
  });

  return (
    <div className="mx-auto flex max-w-2xl flex-col gap-8">
      <h1 className="text-heading-1 font-medium text-text-primary">Your account</h1>

      <section aria-labelledby="profile-heading">
        <h2 id="profile-heading" className="text-heading-2 font-medium text-text-primary">
          Profile
        </h2>
        {me.isPending ? (
          <Skeleton className="mt-3 h-20 w-full" />
        ) : me.isError ? (
          <ErrorState kind={me.error instanceof ApiFailure ? me.error.kind : "server"} onRetry={() => void me.refetch()} />
        ) : (
          <dl className="mt-3 grid grid-cols-1 gap-3">
            <Row label="Email" value={me.data.email} />
            <Row label="Name" value={me.data.display_name ?? "—"} />
            <Row label="Organization" value={me.data.organization.name} />
          </dl>
        )}
      </section>

      <section aria-labelledby="password-heading">
        <h2 id="password-heading" className="text-heading-2 font-medium text-text-primary">
          Password
        </h2>
        {me.data !== undefined ? <PasswordForm policy={me.data.password_policy} /> : null}
      </section>

      <section aria-labelledby="mfa-heading">
        <h2 id="mfa-heading" className="text-heading-2 font-medium text-text-primary">
          Multi-factor authentication
        </h2>
        <div className="mt-3">
          <MyFactors orgId={orgId} returnPath="/account" />
        </div>
      </section>

      <section aria-labelledby="sessions-heading">
        <h2 id="sessions-heading" className="text-heading-2 font-medium text-text-primary">
          Where you are signed in
        </h2>
        <div className="mt-3">
          {claims !== null ? <SessionsTab orgId={orgId} userId={claims.subject} /> : null}
        </div>
      </section>

      <section aria-labelledby="linked-heading">
        <h2 id="linked-heading" className="text-heading-2 font-medium text-text-primary">
          Linked sign-ins
        </h2>
        {/*
          docs/UI-UX/21's governance rule: never imply a capability that is not
          shipped. Social sign-in arrives in Phase 4; until then this says so
          plainly, with no button that looks like it would do something.
        */}
        <div className="mt-3 flex flex-wrap items-center gap-2 rounded border border-border bg-bg-surface p-4">
          <Badge tone="muted">Not available</Badge>
          <p className="text-body text-text-secondary">
            Signing in with another account, such as Google or Microsoft, is not available on this
            service yet.
          </p>
        </div>
      </section>
    </div>
  );
}

type Policy = {
  min_length: number;
  require_uppercase: boolean;
  max_age_days: number;
  breach_checked: boolean;
};

/**
 * Change password, with the rules shown **before** anything is typed
 * (`P1-02`, card step 2), not only after a refusal.
 */
function PasswordForm({ policy }: { policy: Policy }) {
  const [current, setCurrent] = useState("");
  const [chosen, setChosen] = useState("");
  const [confirm, setConfirm] = useState("");
  const [fieldErrors, setFieldErrors] = useState<Record<string, string[]>>({});
  const [done, setDone] = useState(false);

  const change = useMutation({
    mutationFn: async () => {
      const { error, response } = await api.POST("/v1/me/password", {
        body: { current_password: current, new_password: chosen },
      });
      if (error !== undefined) {
        const envelope = error as {
          error?: { code?: string; message?: string; details?: { field: string; issue: string }[] };
        };
        const failure = new ApiFailure(
          envelope.error?.code ?? (response.status === 429 ? "RATE_LIMITED" : "SERVER_ERROR"),
          envelope.error?.message ?? "The password could not be changed.",
        );
        const byField: Record<string, string[]> = {};
        for (const detail of envelope.error?.details ?? []) {
          (byField[detail.field] ??= []).push(detail.issue);
        }
        setFieldErrors(byField);
        throw failure;
      }
    },
    onMutate: () => {
      setDone(false);
      setFieldErrors({});
    },
    onSuccess: () => {
      setDone(true);
      setCurrent("");
      setChosen("");
      setConfirm("");
    },
  });

  const mismatch = confirm !== "" && confirm !== chosen;
  const requirements = [
    `At least ${policy.min_length} characters`,
    ...(policy.require_uppercase ? ["At least one uppercase letter"] : []),
    ...(policy.breach_checked ? ["Not a password that has appeared in a known data breach"] : []),
    "Different from your current password",
  ];

  const failure = change.error instanceof ApiFailure ? change.error : null;
  const formLevel =
    failure !== null && Object.keys(fieldErrors).length === 0 ? failure.message : null;

  return (
    <form
      className="mt-3 flex flex-col gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        if (!mismatch) change.mutate();
      }}
      noValidate
    >
      <div id="password-requirements">
        <p className="text-body text-text-primary">Your new password needs:</p>
        <ul className="mt-1 list-disc pl-5 text-body text-text-secondary">
          {requirements.map((rule) => (
            <li key={rule}>{rule}</li>
          ))}
        </ul>
        {policy.max_age_days > 0 ? (
          <p className="mt-1 text-small text-text-secondary">
            Passwords in your organization expire after {policy.max_age_days} days.
          </p>
        ) : null}
      </div>

      <Field
        id="current-password"
        label="Current password"
        autoComplete="current-password"
        value={current}
        onChange={setCurrent}
        errors={fieldErrors.current_password}
      />
      <Field
        id="new-password"
        label="New password"
        autoComplete="new-password"
        value={chosen}
        onChange={setChosen}
        errors={fieldErrors.new_password}
        describedBy="password-requirements"
      />
      <Field
        id="confirm-password"
        label="Confirm new password"
        autoComplete="new-password"
        value={confirm}
        onChange={setConfirm}
        errors={mismatch ? ["The two new passwords do not match."] : undefined}
      />

      {formLevel !== null ? (
        <p role="alert" className="text-body text-text-primary">
          {formLevel}
        </p>
      ) : null}
      {done ? (
        <p role="status" className="text-body text-text-primary">
          Your password is changed. You were signed out everywhere else.
        </p>
      ) : null}

      <div>
        <Button
          type="submit"
          variant="primary"
          loading={change.isPending}
          disabled={current === "" || chosen === "" || confirm === "" || mismatch}
        >
          Change password
        </Button>
      </div>
    </form>
  );
}

function Field({
  id,
  label,
  autoComplete,
  value,
  onChange,
  errors,
  describedBy,
}: {
  id: string;
  label: string;
  autoComplete: string;
  value: string;
  onChange: (value: string) => void;
  errors?: string[];
  describedBy?: string;
}) {
  const errorId = `${id}-errors`;
  const invalid = errors !== undefined && errors.length > 0;
  const described = [describedBy, invalid ? errorId : undefined].filter(Boolean).join(" ") || undefined;

  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-body font-medium text-text-primary">
        {label}
      </label>
      <input
        id={id}
        type="password"
        autoComplete={autoComplete}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        aria-invalid={invalid}
        aria-describedby={described}
        className="w-full rounded border border-border bg-bg-surface px-3 py-3 text-body text-text-primary"
      />
      {invalid ? (
        <ul id={errorId} className="text-small text-text-primary">
          {errors.map((error) => (
            <li key={error}>{error}</li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-small text-text-secondary">{label}</dt>
      <dd className="mt-1 break-words text-body text-text-primary">{value}</dd>
    </div>
  );
}
