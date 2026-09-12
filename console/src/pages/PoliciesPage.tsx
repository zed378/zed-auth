import { useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { Badge } from "../components/Badge";
import { Button } from "../components/Button";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { ErrorState, Skeleton } from "../components/states";
import { api, queryKeys } from "../lib/api/client";
import { LOGIN_METHODS, SETTINGS_BOUNDS, SETTINGS_DEFAULTS } from "../lib/api/settings.gen";
import { useOrganization, useOrgId } from "../lib/api/queries";

type PasswordPolicy = {
  min_length?: number;
  require_uppercase?: boolean;
  max_age_days?: number;
};

/**
 * `allowed_login_methods` is typed as the generated union, not `string[]`.
 *
 * The spec's enum is the set of methods the service implements — `password`
 * today, with `passkey` and `social` refused until they work. Widening it here
 * would let this screen offer a method the API rejects, which is the console
 * claiming a capability that does not exist.
 */
type LoginMethod = (typeof LOGIN_METHODS)[number];

type Settings = {
  password_policy?: PasswordPolicy;
  mfa_required?: boolean;
  session_lifetime_hours?: number;
  allowed_login_methods?: LoginMethod[];
};

/**
 * The Policies (Access) screen (P2-14, `docs/UI-UX/08` § Policies — Access tab).
 *
 * The implementation chain (`docs/UI-UX/19`) is committed beside the code at
 * `console/docs/implementation-chain-P2-14.md`.
 *
 * Three things here are load-bearing.
 *
 * **The bounds and the defaults are the server's**, generated from the same
 * OpenAPI schema the service validates against (`settings.gen.ts`). A form
 * that accepts what the server refuses produces a rejection nobody can
 * explain; a form that restates the defaults keeps showing yesterday's numbers
 * after the service changes them.
 *
 * **Nothing claims enforcement that does not exist.** `mfa_required` is stored
 * and validated by the API and enforced by nothing until Phase 3, so it is
 * shown as what it is — a recorded intention — rather than as a control
 * (`P2-14` step 3, `docs/UI-UX/21`'s governance rule).
 *
 * **Changes that reduce access are confirmed with their blast radius named.**
 * Shortening a session lifetime signs people out. Removing a login method
 * locks out everyone who has only that one. Both are one keystroke away on
 * this screen and neither is reversible for the people it affects.
 */
export function PoliciesPage() {
  const orgId = useOrgId();
  const organization = useOrganization(orgId);
  const queryClient = useQueryClient();

  // Memoised because `?? {}` is a fresh object every render, which would make
  // the comparisons below recompute whether or not anything changed.
  const stored = useMemo(
    () => (organization.data?.settings ?? {}) as Settings,
    [organization.data?.settings],
  );

  const [draft, setDraft] = useState<Settings | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  // Seeded from the server once it arrives, keyed on the organization so a
  // switch does not leave the previous tenant's policy in the form.
  const [seededFor, setSeededFor] = useState<string | null>(null);
  if (organization.isSuccess && orgId !== null && seededFor !== orgId) {
    setSeededFor(orgId);
    setDraft(stored);
    setProblem(null);
    setSaved(false);
  }

  const current = draft ?? stored;

  const save = useMutation({
    mutationFn: async (settings: Settings) => {
      const { error } = await api.PATCH("/v1/organizations/{org_id}", {
        params: { path: { org_id: orgId as string } },
        // The whole document, not the changed fields. The API merges key by
        // key — deeply, since `P2-14` fixed the shallow merge that discarded a
        // password rule's siblings — so sending everything is what makes the
        // screen's displayed state and the stored state the same thing.
        body: { settings },
      });
      if (error !== undefined) throw error;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: [...queryKeys.organizations, orgId] });
      setConfirming(false);
      setProblem(null);
      setSaved(true);
    },
    onError: (error: unknown) => {
      setConfirming(false);
      setProblem(messageFor(error));
    },
  });

  const problems = useMemo(() => validate(current), [current]);
  const reductions = useMemo(() => reducesAccess(stored, current), [stored, current]);
  const changed = useMemo(
    () => JSON.stringify(normalise(stored)) !== JSON.stringify(normalise(current)),
    [stored, current],
  );

  function update(next: Settings) {
    setDraft(next);
    setSaved(false);
  }

  function submit() {
    if (Object.keys(problems).length > 0) return;
    // Step 5: a change that could reduce access for existing users is
    // confirmed; one that cannot is applied.
    if (reductions.length > 0) {
      setConfirming(true);
      return;
    }
    save.mutate(current);
  }

  if (organization.isPending) {
    return (
      <>
        <h1 className="text-heading-1 font-medium text-text-primary">Access policies</h1>
        <div className="mt-6 max-w-2xl" aria-busy="true" aria-live="polite">
          <span className="sr-only">Loading this organization's policies</span>
          <Skeleton className="h-64 w-full" />
        </div>
      </>
    );
  }

  if (organization.isError) {
    return (
      <>
        <h1 className="text-heading-1 font-medium text-text-primary">Access policies</h1>
        <div className="mt-6 max-w-2xl">
          <ErrorState
            kind={kindOf(organization.error)}
            onRetry={() => void organization.refetch()}
          />
        </div>
      </>
    );
  }

  return (
    <>
      <h1 className="text-heading-1 font-medium text-text-primary">Access policies</h1>
      <p className="mt-2 max-w-prose text-body text-text-secondary">
        What {organization.data?.name ?? "this organization"} requires of its users. These apply to
        everyone who signs in through it.
      </p>

      <form
        className="mt-6 max-w-2xl"
        onSubmit={(event) => {
          event.preventDefault();
          submit();
        }}
      >
        <fieldset className="rounded border border-border bg-bg-surface p-5">
          <legend className="px-2 text-heading-3 font-medium text-text-primary">Passwords</legend>

          <NumberField
            id="min-length"
            label="Minimum length"
            value={current.password_policy?.min_length}
            fallback={SETTINGS_DEFAULTS.min_length}
            bounds={SETTINGS_BOUNDS.min_length}
            problem={problems.min_length}
            help={`Characters. The service will not accept a value below ${SETTINGS_BOUNDS.min_length.min}: a per-organization setting that could go under the platform floor would be a way to switch the floor off.`}
            onChange={(value) =>
              update({
                ...current,
                password_policy: { ...current.password_policy, min_length: value },
              })
            }
          />

          <BooleanField
            id="require-uppercase"
            label="Require an upper-case letter"
            value={current.password_policy?.require_uppercase}
            fallback={SETTINGS_DEFAULTS.require_uppercase}
            onChange={(value) =>
              update({
                ...current,
                password_policy: { ...current.password_policy, require_uppercase: value },
              })
            }
          />

          <NumberField
            id="max-age"
            label="Expire passwords after"
            value={current.password_policy?.max_age_days}
            fallback={SETTINGS_DEFAULTS.max_age_days}
            bounds={SETTINGS_BOUNDS.max_age_days}
            problem={problems.max_age_days}
            help="Days. 0 means passwords never expire — a real choice, and increasingly the recommended one: NIST SP 800-63B argues forced rotation makes passwords worse."
            onChange={(value) =>
              update({
                ...current,
                password_policy: { ...current.password_policy, max_age_days: value },
              })
            }
          />

          <p className="mt-4 text-small text-text-secondary">
            Tightening these does not lock anybody out. They govern what a <em>new</em> password
            must satisfy; the only rule that acts on an existing one is expiry, which takes effect
            at that user's next sign-in.
          </p>
        </fieldset>

        <fieldset className="mt-5 rounded border border-border bg-bg-surface p-5">
          <legend className="px-2 text-heading-3 font-medium text-text-primary">Sessions</legend>

          <NumberField
            id="session-lifetime"
            label="Sessions last"
            value={current.session_lifetime_hours}
            fallback={SETTINGS_DEFAULTS.session_lifetime_hours}
            bounds={SETTINGS_BOUNDS.session_lifetime_hours}
            problem={problems.session_lifetime_hours}
            help="Hours, from sign-in. A value outside the range is clamped by the service rather than refused, and the clamp is logged — but a value it has to clamp is one nobody intended."
            onChange={(value) => update({ ...current, session_lifetime_hours: value })}
          />
        </fieldset>

        <fieldset className="mt-5 rounded border border-border bg-bg-surface p-5">
          <legend className="px-2 text-heading-3 font-medium text-text-primary">
            Sign-in methods
          </legend>

          <p className="text-small text-text-secondary">
            How people in this organization may sign in. At least one is required — an organization
            that permits none permits nobody.
          </p>

          <ul className="mt-3 flex flex-col gap-2">
            {LOGIN_METHODS.map((method) => {
              const enabled = (current.allowed_login_methods ?? SETTINGS_DEFAULTS.allowed_login_methods).includes(
                method,
              );
              return (
                <li key={method}>
                  <label className="flex items-center gap-3">
                    <input
                      type="checkbox"
                      checked={enabled}
                      onChange={(event) => {
                        const on = event.currentTarget.checked;
                        const now = new Set<LoginMethod>(
                          current.allowed_login_methods ?? SETTINGS_DEFAULTS.allowed_login_methods,
                        );
                        if (on) now.add(method);
                        else now.delete(method);
                        update({ ...current, allowed_login_methods: [...now] });
                      }}
                    />
                    <span className="text-body text-text-primary capitalize">{method}</span>
                  </label>
                </li>
              );
            })}
          </ul>

          {problems.allowed_login_methods !== undefined ? (
            <p role="alert" className="mt-2 text-small text-danger">
              {problems.allowed_login_methods}
            </p>
          ) : null}
        </fieldset>

        {/*
          Step 3: shown, and shown as what it is. `mfa_required` is accepted and
          stored by the API today and enforced by nothing until Phase 3.
          Presenting it as a working control would be the console claiming a
          capability that does not exist — the exact thing `docs/UI-UX/21`'s
          governance rule forbids — and an administrator who switched it on
          would believe their organization was protected.
        */}
        <fieldset className="mt-5 rounded border border-border bg-bg-surface p-5">
          <legend className="px-2 text-heading-3 font-medium text-text-primary">
            Multi-factor <Badge tone="attention">Not enforced yet</Badge>
          </legend>

          <BooleanField
            id="mfa-required"
            label="Require a second factor"
            value={current.mfa_required}
            fallback={SETTINGS_DEFAULTS.mfa_required}
            onChange={(value) => update({ ...current, mfa_required: value })}
          />

          <p className="mt-2 max-w-prose text-small text-text-secondary">
            <strong className="font-medium text-text-primary">
              Setting this changes nothing today.
            </strong>{" "}
            It is recorded, and it will take effect when multi-factor enrolment ships in Phase 3.
            Until then nobody is asked for a second factor, whatever this says.
          </p>
        </fieldset>

        {problem !== null ? (
          <p role="alert" className="mt-4 text-small text-danger">
            {problem}
          </p>
        ) : null}

        {saved ? (
          <p role="status" className="mt-4 text-small text-success">
            Saved. These apply to sign-ins from now on.
          </p>
        ) : null}

        <div className="mt-5 flex items-center gap-3">
          <Button
            type="submit"
            variant="primary"
            loading={save.isPending}
            disabled={!changed || Object.keys(problems).length > 0}
          >
            Save policies
          </Button>
          {changed ? (
            <Button
              type="button"
              onClick={() => {
                update(stored);
                setProblem(null);
              }}
            >
              Discard changes
            </Button>
          ) : null}
        </div>
      </form>

      <ConfirmDialog
        open={confirming}
        title="Apply a change that reduces access?"
        verb="Apply policies"
        busy={save.isPending}
        onCancel={() => setConfirming(false)}
        onConfirm={() => save.mutate(current)}
        consequence={
          <>
            <p>These changes affect people who are signed in right now:</p>
            <ul className="mt-2 list-disc pl-5">
              {reductions.map((reduction) => (
                <li key={reduction} className="mt-1">
                  {reduction}
                </li>
              ))}
            </ul>
            <p className="mt-3 text-text-secondary">
              Everything else on the form is saved at the same time.
            </p>
          </>
        }
      />
    </>
  );
}

/** A number the service bounds, with what it is today shown beside it. */
function NumberField({
  id,
  label,
  value,
  fallback,
  bounds,
  problem,
  help,
  onChange,
}: {
  id: string;
  label: string;
  value: number | undefined;
  fallback: number;
  bounds: { min: number; max: number };
  problem?: string;
  help: string;
  onChange: (value: number | undefined) => void;
}) {
  const configured = value !== undefined;

  return (
    <div className="mt-4 first:mt-0">
      <label htmlFor={id} className="block text-small font-medium text-text-secondary">
        {label}
      </label>
      <input
        id={id}
        type="number"
        inputMode="numeric"
        min={bounds.min}
        max={bounds.max}
        value={value ?? ""}
        placeholder={String(fallback)}
        aria-invalid={problem !== undefined || undefined}
        aria-describedby={problem !== undefined ? `${id}-error` : `${id}-help`}
        onChange={(event) => {
          const raw = event.currentTarget.value.trim();
          onChange(raw === "" ? undefined : Number(raw));
        }}
        className="mt-1 w-32 rounded border border-border bg-bg-base px-3 py-2 text-body text-text-primary"
      />
      {/*
        Step 6: what is in force today, beside what is being changed. The
        distinction between "configured to 12" and "not configured, so the
        service applies 12" is invisible in the value and matters when the
        service default moves.
      */}
      <p className="mt-1 text-small text-text-secondary">
        {configured ? (
          <>Currently set for this organization.</>
        ) : (
          <>
            Not set — the service applies <strong className="font-medium">{fallback}</strong>. Clear
            the field to go back to that.
          </>
        )}
      </p>
      {problem !== undefined ? (
        <p id={`${id}-error`} className="mt-1 text-small text-danger">
          {problem}
        </p>
      ) : (
        <p id={`${id}-help`} className="mt-1 max-w-prose text-small text-text-secondary">
          {help}
        </p>
      )}
    </div>
  );
}

function BooleanField({
  id,
  label,
  value,
  fallback,
  onChange,
}: {
  id: string;
  label: string;
  value: boolean | undefined;
  fallback: boolean;
  onChange: (value: boolean) => void;
}) {
  return (
    <div className="mt-4">
      <label className="flex items-center gap-3">
        <input
          id={id}
          type="checkbox"
          checked={value ?? fallback}
          onChange={(event) => onChange(event.currentTarget.checked)}
        />
        <span className="text-body text-text-primary">{label}</span>
      </label>
      {value === undefined ? (
        <p className="mt-1 text-small text-text-secondary">
          Not set — the service applies {fallback ? "on" : "off"}.
        </p>
      ) : null}
    </div>
  );
}

// --- validation, against the server's own published bounds -------------------

function validate(settings: Settings): Record<string, string> {
  const problems: Record<string, string> = {};

  const number = (
    key: "min_length" | "max_age_days" | "session_lifetime_hours",
    value: number | undefined,
  ) => {
    if (value === undefined) return;
    const { min, max } = SETTINGS_BOUNDS[key];
    if (!Number.isInteger(value)) {
      problems[key] = "A whole number is required.";
      return;
    }
    if (value < min || value > max) {
      problems[key] = `Must be between ${min} and ${max}.`;
    }
  };

  number("min_length", settings.password_policy?.min_length);
  number("max_age_days", settings.password_policy?.max_age_days);
  number("session_lifetime_hours", settings.session_lifetime_hours);

  if (settings.allowed_login_methods !== undefined && settings.allowed_login_methods.length === 0) {
    // The API refuses it too. Saying so here means the administrator finds out
    // before they save rather than from a rejection.
    problems.allowed_login_methods =
      "At least one sign-in method is required. An organization that permits none permits nobody — including you.";
  }

  return problems;
}

// --- blast radius ------------------------------------------------------------

/**
 * What this change takes away from people who already have it.
 *
 * Only reductions. A longer session lifetime and a looser password rule affect
 * nobody adversely, and confirming every save would make the confirmation
 * furniture — `docs/UI-UX/07` is explicit that friction has to stay rare to
 * stay meaningful.
 */
function reducesAccess(stored: Settings, next: Settings): string[] {
  const out: string[] = [];

  const wasLifetime = stored.session_lifetime_hours ?? SETTINGS_DEFAULTS.session_lifetime_hours;
  const nowLifetime = next.session_lifetime_hours ?? SETTINGS_DEFAULTS.session_lifetime_hours;
  if (nowLifetime < wasLifetime) {
    out.push(
      `Sessions drop from ${wasLifetime} to ${nowLifetime} hours. Anyone whose session is already older than ${nowLifetime} hours is signed out at their next request.`,
    );
  }

  const wasMethods = new Set(stored.allowed_login_methods ?? SETTINGS_DEFAULTS.allowed_login_methods);
  const nowMethods = new Set(next.allowed_login_methods ?? SETTINGS_DEFAULTS.allowed_login_methods);
  for (const method of wasMethods) {
    if (!nowMethods.has(method)) {
      out.push(
        `${method[0].toUpperCase()}${method.slice(1)} sign-in stops working immediately. Anyone who has only that method cannot sign in at all — including you, if it is how you got here.`,
      );
    }
  }

  const wasAge = stored.password_policy?.max_age_days ?? SETTINGS_DEFAULTS.max_age_days;
  const nowAge = next.password_policy?.max_age_days ?? SETTINGS_DEFAULTS.max_age_days;
  // 0 means "never expires", so it is the loosest value rather than the
  // tightest — comparing it as a number would read a change to 0 as a
  // reduction and a change from 0 as an improvement, both backwards.
  if (nowAge !== 0 && (wasAge === 0 || nowAge < wasAge)) {
    out.push(
      `Passwords older than ${nowAge} days must be changed at the next sign-in. Anyone whose password already is will be asked to set a new one.`,
    );
  }

  return out;
}

// --- helpers -----------------------------------------------------------------

/** Drops undefined keys, so "absent" and "absent" compare equal. */
function normalise(settings: Settings): Settings {
  return JSON.parse(JSON.stringify(settings)) as Settings;
}

function kindOf(error: unknown): "network" | "server" | "permission" | "validation" {
  const failure = error as { kind?: "network" | "server" | "permission" | "validation" } | null;
  return failure?.kind ?? "server";
}

function messageFor(error: unknown): string {
  const envelope = error as { error?: { message?: string; details?: { issue?: string }[] } };
  return (
    envelope?.error?.details?.[0]?.issue ??
    envelope?.error?.message ??
    "Those policies could not be saved. Please try again."
  );
}
