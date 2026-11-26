import { useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { Button } from "../components/Button";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { FormSummary, FormSystem } from "../components/FormSystem";
import { ErrorState, LoadingRegion, Skeleton } from "../components/states";
import { api, queryKeys } from "../lib/api/client";
import { invalidateQueries, mutationInvalidations } from "../lib/api/queryPolicy";
import { LOGIN_METHODS, SETTINGS_BOUNDS, SETTINGS_DEFAULTS } from "../lib/api/settings.gen";
import { asFailure, useOrganization, useOrgId } from "../lib/api/queries";
import type { components } from "../lib/api/schema.gen";

type MfaImpact = components["schemas"]["MfaImpact"];

type PasswordPolicy = {
  min_length?: number;
  require_uppercase?: boolean;
  max_age_days?: number;
};

/**
 * `allowed_login_methods` is typed as the generated union, not `string[]`.
 *
 * The spec's enum is the set of methods the service implements. Widening it here
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

const policyFields = [
  { name: "min_length", path: "password_policy.min_length", label: "Minimum length" },
  { name: "require_uppercase", path: "password_policy.require_uppercase", label: "Require an upper-case letter" },
  { name: "max_age_days", path: "password_policy.max_age_days", label: "Expire passwords after" },
  { name: "session_lifetime_hours", path: "session_lifetime_hours", label: "Sessions last" },
  { name: "allowed_login_methods", path: "allowed_login_methods", label: "Sign-in methods" },
  { name: "mfa_required", path: "mfa_required", label: "Require a second factor" },
];

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
 * **Nothing claims enforcement that does not exist — or denies one that
 * does.** Through Phase 2 `mfa_required` was stored and enforced by nothing,
 * and this screen said so. `P3-07` made it enforced and the screen went on
 * saying "setting this changes nothing today" for a whole task — the reverse
 * failure, and the more dangerous one, because an administrator told a switch
 * is inert has no reason to warn anybody before flipping it. `P3-13` replaced
 * it with what the switch now does and how many people it reaches.
 *
 * **Changes that reduce access are confirmed with their blast radius named.**
 * Shortening a session lifetime signs people out. Removing a login method
 * locks out everyone who has only that one. Both are one keystroke away on
 * this screen and neither is reversible for the people it affects.
 */
export function PoliciesPage() {
  const orgId = useOrgId();
  return <PoliciesScope key={orgId} orgId={orgId} />;
}

function PoliciesScope({ orgId }: { orgId: string | null }) {
  const organization = useOrganization(orgId);
  const queryClient = useQueryClient();

  // Memoised because `?? {}` is a fresh object every render, which would make
  // the comparisons below recompute whether or not anything changed.
  const stored = useMemo(
    () => (organization.data?.settings ?? {}) as Settings,
    [organization.data?.settings],
  );

  const accessDenied = organization.isError && kindOf(organization.error) === "permission";

  const [draft, setDraft] = useState<Settings | null>(null);
  const [confirmation, setConfirmation] = useState<Settings | null>(null);
  const confirming = confirmation !== null;
  const [problem, setProblem] = useState<string | null>(null);
  const [problems, setProblems] = useState<Record<string, string>>({});
  const [saved, setSaved] = useState(false);
  const formRef = useRef<HTMLFormElement>(null);
  const focusField = useRef<string | null>(null);
  const saving = useRef(false);

  // Seeded from the server once it arrives, keyed on the organization so a
  // switch does not leave the previous tenant's policy in the form.
  const [seededFor, setSeededFor] = useState<string | null>(null);
  // A refusal invalidates any local copy of the protected policy, including a
  // draft. If access returns later, the form is seeded from the fresh response.
  if (accessDenied && seededFor !== null) {
    setSeededFor(null);
    setDraft(null);
    setConfirmation(null);
    setProblem(null);
    setProblems({});
    setSaved(false);
  }
  if (organization.isSuccess && orgId !== null && seededFor !== orgId) {
    setSeededFor(orgId);
    setDraft(stored);
    setProblem(null);
    setProblems({});
    setSaved(false);
  }

  const current = draft ?? stored;

  // Who the MFA mandate reaches (P3-07's endpoint, first used here in P3-13).
  // Under the organization's key, so saving the policies refreshes it.
  const impact = useQuery({
    queryKey: [...queryKeys.organizations, orgId, "mfa-impact"],
    enabled: orgId !== null,
    queryFn: async (): Promise<MfaImpact> => {
      const { data, error } = await api.GET("/v1/organizations/{org_id}/mfa-impact", {
        params: { path: { org_id: orgId as string } },
      });
      if (error !== undefined) throw asFailure(error);
      return data;
    },
  });

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
      void invalidateQueries(queryClient, mutationInvalidations.organization(orgId)).catch(() => undefined);
      setConfirmation(null);
      setProblem(null);
      setProblems({});
      setSaved(true);
    },
    onError: (error: unknown) => {
      setConfirmation(null);
      const mapped = policyFailure(error);
      report(mapped.fields, mapped.message);
    },
    onSettled: () => { saving.current = false; },
  });

  useEffect(() => {
    if (save.isPending || confirming || focusField.current === null) return;
    const element = formRef.current?.elements.namedItem(focusField.current);
    if (element instanceof HTMLElement) element.focus();
    focusField.current = null;
  }, [problems, save.isPending, confirming]);

  const reductions = useMemo(
    () => reducesAccess(stored, confirmation ?? current, impact.data ?? null),
    [stored, confirmation, current, impact.data],
  );
  const changed = useMemo(
    () => JSON.stringify(normalise(stored)) !== JSON.stringify(normalise(current)),
    [stored, current],
  );

  function update(next: Settings) {
    setDraft(next);
    setSaved(false);
  }

  function report(next: Record<string, string>, fallback: string | null = null) {
    setProblems(next);
    const invalid = policyFields.filter((field) => next[field.name]);
    setProblem(invalid.length
      ? `Check ${invalid.length} ${invalid.length === 1 ? "field" : "fields"}: ${invalid.map((field) => field.label).join(", ")}.${fallback ? ` ${fallback}` : ""}`
      : fallback);
    focusField.current = invalid[0]?.name ?? null;
    return invalid.length === 0;
  }

  function readSettings(): Settings {
    const data = formRef.current ? new FormData(formRef.current) : null;
    const number = (name: string, fallback: number | undefined) => {
      const raw = data?.get(name);
      return typeof raw === "string" ? raw.trim() === "" ? undefined : Number(raw) : fallback;
    };
    const minLength = number("min_length", current.password_policy?.min_length);
    const maxAge = number("max_age_days", current.password_policy?.max_age_days);
    return {
      ...current,
      password_policy: current.password_policy === undefined && minLength === undefined && maxAge === undefined ? undefined : {
        ...current.password_policy,
        min_length: minLength,
        max_age_days: maxAge,
      },
      session_lifetime_hours: number("session_lifetime_hours", current.session_lifetime_hours),
    };
  }

  function blur(name: string) {
    const next = readSettings();
    setDraft(next);
    setProblems((previous) => {
      const updated = { ...previous };
      delete updated[name];
      const issue = validate(next)[name];
      if (issue) updated[name] = issue;
      return updated;
    });
  }

  function commit(settings: Settings) {
    if (saving.current) return;
    saving.current = true;
    save.mutate(settings);
  }

  function submit() {
    if (saving.current || confirming) return;
    const next = readSettings();
    setDraft(next);
    setSaved(false);
    if (!report(validate(next))) return;
    // Step 5: a change that could reduce access for existing users is
    // confirmed; one that cannot is applied.
    if (reducesAccess(stored, next, impact.data ?? null).length > 0) {
      setConfirmation(next);
      return;
    }
    commit(next);
  }

  if (organization.isPending) {
    return (
      <>
        <h1 className="text-heading-1 font-medium text-text-primary">Access policies</h1>
        <LoadingRegion label="Loading this organization's policies">
          <div className="mt-6 max-w-2xl">
            <Skeleton className="h-64 w-full" />
          </div>
        </LoadingRegion>
      </>
    );
  }

  // Without a prior successful read there is nothing safe to show after a
  // failure. Permission denial always takes this path, even with cached data.
  if (organization.isError && (!organization.data || accessDenied)) {
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

      {organization.isError ? (
        <div className="mt-4 max-w-2xl">
          <ErrorState
            kind={kindOf(organization.error) === "network" ? "network" : "server"}
            detail="Showing last-known policies. Try again to refresh these settings."
            onRetry={() => void organization.refetch()}
          />
        </div>
      ) : null}

      <FormSystem
        ref={formRef}
        pattern="explicit"
        className="mt-6 max-w-2xl"
        onSubmit={(event) => {
          event.preventDefault();
          submit();
        }}
      >
        <fieldset disabled={save.isPending || confirming} className="rounded border border-border bg-bg-surface p-5">
          <legend className="px-2 text-heading-3 font-medium text-text-primary">Passwords</legend>

          <NumberField
            id="min-length"
            name="min_length"
            label="Minimum length"
            value={current.password_policy?.min_length}
            fallback={SETTINGS_DEFAULTS.min_length}
            bounds={SETTINGS_BOUNDS.min_length}
            problem={problems.min_length}
            onBlur={() => blur("min_length")}
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
            name="require_uppercase"
            label="Require an upper-case letter"
            value={current.password_policy?.require_uppercase}
            fallback={SETTINGS_DEFAULTS.require_uppercase}
            problem={problems.require_uppercase}
            onBlur={() => blur("require_uppercase")}
            onChange={(value) =>
              update({
                ...current,
                password_policy: { ...current.password_policy, require_uppercase: value },
              })
            }
          />

          <NumberField
            id="max-age"
            name="max_age_days"
            label="Expire passwords after"
            value={current.password_policy?.max_age_days}
            fallback={SETTINGS_DEFAULTS.max_age_days}
            bounds={SETTINGS_BOUNDS.max_age_days}
            problem={problems.max_age_days}
            onBlur={() => blur("max_age_days")}
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

        <fieldset disabled={save.isPending || confirming} className="mt-5 rounded border border-border bg-bg-surface p-5">
          <legend className="px-2 text-heading-3 font-medium text-text-primary">Sessions</legend>

          <NumberField
            id="session-lifetime"
            name="session_lifetime_hours"
            label="Sessions last"
            value={current.session_lifetime_hours}
            fallback={SETTINGS_DEFAULTS.session_lifetime_hours}
            bounds={SETTINGS_BOUNDS.session_lifetime_hours}
            problem={problems.session_lifetime_hours}
            onBlur={() => blur("session_lifetime_hours")}
            help="Hours, from sign-in. A value outside the range is clamped by the service rather than refused, and the clamp is logged — but a value it has to clamp is one nobody intended."
            onChange={(value) => update({ ...current, session_lifetime_hours: value })}
          />
        </fieldset>

        <fieldset disabled={save.isPending || confirming} className="mt-5 rounded border border-border bg-bg-surface p-5">
          <legend className="px-2 text-heading-3 font-medium text-text-primary">
            Sign-in methods
          </legend>

          <p className="text-small text-text-secondary">
            How people in this organization may sign in. At least one is required — an organization
            that permits none permits nobody.
            Social sign-in also requires a configured provider and a linked local account.
          </p>

          <ul className="mt-3 flex flex-col gap-2">
            {LOGIN_METHODS.map((method) => {
              const enabled = (current.allowed_login_methods ?? SETTINGS_DEFAULTS.allowed_login_methods).some(
                (allowed) => allowed === method,
              );
              return (
                <li key={method}>
                  <label className="flex items-center gap-3">
                    <input
                      type="checkbox"
                      name={method === LOGIN_METHODS[0] ? "allowed_login_methods" : undefined}
                      aria-invalid={problems.allowed_login_methods !== undefined || undefined}
                      aria-describedby={problems.allowed_login_methods !== undefined ? "login-methods-error" : undefined}
                      onBlur={() => blur("allowed_login_methods")}
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
            <p id="login-methods-error" role="alert" className="mt-2 text-small text-danger">
              {problems.allowed_login_methods}
            </p>
          ) : null}
        </fieldset>

        <fieldset disabled={save.isPending || confirming} className="mt-5 rounded border border-border bg-bg-surface p-5">
          <legend className="px-2 text-heading-3 font-medium text-text-primary">Multi-factor</legend>

          <BooleanField
            id="mfa-required"
            name="mfa_required"
            label="Require a second factor"
            value={current.mfa_required}
            fallback={SETTINGS_DEFAULTS.mfa_required}
            problem={problems.mfa_required}
            onBlur={() => blur("mfa_required")}
            onChange={(value) => update({ ...current, mfa_required: value })}
          />

          <MandateEffect impact={impact.data ?? null} failed={impact.isError} on={current.mfa_required ?? SETTINGS_DEFAULTS.mfa_required} />
        </fieldset>

        {problem !== null ? (
          <div className="mt-4"><FormSummary message={problem} /></div>
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
            disabled={!changed || confirming}
          >
            Save policies
          </Button>
          {changed ? (
            <Button
              type="button"
              disabled={save.isPending || confirming}
              onClick={() => {
                update(stored);
                setProblem(null);
                setProblems({});
              }}
            >
              Discard changes
            </Button>
          ) : null}
        </div>
      </FormSystem>

      <ConfirmDialog
        open={confirming}
        title="Apply a change that reduces access?"
        verb="Apply policies"
        busy={save.isPending}
        onCancel={() => { if (!saving.current) setConfirmation(null); }}
        onConfirm={() => { if (confirmation !== null) commit(confirmation); }}
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
  name,
  label,
  value,
  fallback,
  bounds,
  problem,
  help,
  onChange,
  onBlur,
}: {
  id: string;
  name: string;
  label: string;
  value: number | undefined;
  fallback: number;
  bounds: { min: number; max: number };
  problem?: string;
  help: string;
  onChange: (value: number | undefined) => void;
  onBlur: () => void;
}) {
  const configured = value !== undefined;

  return (
    <div className="mt-4 first:mt-0">
      <label htmlFor={id} className="block text-small font-medium text-text-secondary">
        {label}
      </label>
      <input
        id={id}
        name={name}
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
        onBlur={onBlur}
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
  name,
  label,
  value,
  fallback,
  onChange,
  onBlur,
  problem,
}: {
  id: string;
  name: string;
  label: string;
  value: boolean | undefined;
  fallback: boolean;
  onChange: (value: boolean) => void;
  onBlur: () => void;
  problem?: string;
}) {
  return (
    <div className="mt-4">
      <label className="flex items-center gap-3">
        <input
          id={id}
          name={name}
          type="checkbox"
          aria-invalid={problem !== undefined || undefined}
          aria-describedby={problem !== undefined ? `${id}-error` : undefined}
          checked={value ?? fallback}
          onChange={(event) => onChange(event.currentTarget.checked)}
          onBlur={onBlur}
        />
        <span className="text-body text-text-primary">{label}</span>
      </label>
      {problem !== undefined ? <p id={`${id}-error`} className="mt-1 text-small text-danger">{problem}</p> : value === undefined ? (
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
function reducesAccess(stored: Settings, next: Settings, impact: MfaImpact | null): string[] {
  const out: string[] = [];

  const wasMandate = stored.mfa_required ?? SETTINGS_DEFAULTS.mfa_required;
  const nowMandate = next.mfa_required ?? SETTINGS_DEFAULTS.mfa_required;
  if (nowMandate && !wasMandate) {
    // Nobody is signed out, so this is not a reduction today. It is one on a
    // date, for a number of people this screen can name — and the time to
    // decide whether to tell them first is before saving, not after.
    out.push(
      impact === null
        ? "Everyone without a second factor must set one up. After the grace period they are sent into enrolment when they sign in, before they can continue."
        : `${impact.without_factor} of ${impact.members} active ${impact.members === 1 ? "member has" : "members have"} no second factor. They have ${impact.grace_period_days} days from now; after that they are sent into enrolment when they sign in, before they can continue.`,
    );
  }

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

/**
 * What the mandate does, stated as what it does (P3-13).
 *
 * The count comes from the service and the grace length with it, so neither is
 * a copy that can drift. Counts only — the endpoint deliberately never names
 * who has no factor.
 */
function MandateEffect({ impact, failed, on }: { impact: MfaImpact | null; failed: boolean; on: boolean }) {
  const deadline =
    impact?.grace_ends_at !== undefined && impact.grace_ends_at !== null ? new Date(impact.grace_ends_at) : null;
  const [now] = useState(() => Date.now());
  const graceOver = deadline !== null && deadline.getTime() <= now;

  return (
    <div className="mt-2 max-w-prose text-small text-text-secondary">
      <p>
        When on, everyone who signs in through this organization needs a second factor. People
        without one get {impact !== null ? `${impact.grace_period_days} days` : "a grace period"} from
        the moment it is switched on; after that, signing in sends them into setting up an
        authenticator app before they can continue. Nobody is signed out when you save.
      </p>
      {failed ? (
        <p className="mt-2">Could not count who this affects right now.</p>
      ) : impact !== null ? (
        <p className="mt-2">
          <strong className="font-medium text-text-primary">
            {impact.without_factor} of {impact.members} active{" "}
            {impact.members === 1 ? "member has" : "members have"} no second factor.
          </strong>
          {impact.mfa_required && deadline !== null
            ? graceOver
              ? " The grace period has ended: they will be asked to set one up at their next sign-in."
              : ` The grace period ends ${deadline.toLocaleString()}.`
            : on
              ? ""
              : " Consider telling them before you switch this on."}
        </p>
      ) : null}
    </div>
  );
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

function policyFailure(failure: unknown): { fields: Record<string, string>; message: string | null } {
  const fields: Record<string, string> = {};
  const fallback = "Those policies could not be saved. Your entries are retained. Try again.";
  if (typeof failure !== "object" || failure === null || !("error" in failure)) {
    return { fields, message: fallback };
  }
  const error = failure.error;
  if (typeof error !== "object" || error === null) return { fields, message: fallback };
  const unmapped: string[] = [];
  if ("details" in error && Array.isArray(error.details)) {
    for (const detail of error.details) {
      if (typeof detail !== "object" || detail === null || typeof detail.issue !== "string") continue;
      const field = policyFields.find(({ name, path }) => detail.field === name || detail.field === path || detail.field === `settings.${path}`);
      if (field) fields[field.name] = [fields[field.name], detail.issue].filter(Boolean).join(" ");
      else unmapped.push(detail.issue);
    }
  }
  const message = unmapped.join(" ") || (Object.keys(fields).length ? null
    : "message" in error && typeof error.message === "string" ? error.message : fallback);
  return { fields, message };
}
