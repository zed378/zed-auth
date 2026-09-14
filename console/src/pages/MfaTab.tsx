import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { Badge } from "../components/Badge";
import { Button } from "../components/Button";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { Modal } from "../components/Modal";
import { QrCode } from "../components/QrCode";
import { ErrorState, Skeleton } from "../components/states";
import { Table } from "../components/Table";
import type { Column } from "../components/Table";
import { api, queryKeys } from "../lib/api/client";
import { ApiFailure, asFailure, useMyMfa, useUserMfa } from "../lib/api/queries";
import type { components } from "../lib/api/schema.gen";
import { useAuth } from "../lib/auth/AuthProvider";
import { authConfig } from "../lib/auth/config";
import { signedInWithin } from "../lib/auth/oidc";

type Factor = components["schemas"]["MfaFactor"];

/**
 * The Multi-factor tab on User detail (P3-10, `docs/UI-UX/08`).
 *
 * **Two screens behind one tab**, chosen by whose page this is. For somebody
 * else it is read-only — an administrator can see what a member has and reset
 * it through `P3-04`'s audited path, and nothing else. For oneself it is full
 * management. The split is the API's, not the UI's: the self routes take the
 * user from the token, so an administrator's "manage" controls could only ever
 * have acted on the administrator. Rendering them on a member's page would be a
 * button that lies about whose account it changes.
 */
export function MfaTab({ orgId, userId }: { orgId: string | null; userId: string }) {
  const { claims } = useAuth();
  const self = claims !== null && claims.subject === userId;
  return self ? (
    <MyFactors orgId={orgId} returnPath={`/users/${userId}?tab=mfa`} />
  ) : (
    <MemberFactors orgId={orgId} userId={userId} />
  );
}

// --- somebody else's ------------------------------------------------------------------------

function MemberFactors({ orgId, userId }: { orgId: string | null; userId: string }) {
  const { hasRole } = useAuth();
  const queryClient = useQueryClient();
  const factors = useUserMfa(orgId, userId, true);
  const [resetting, setResetting] = useState(false);

  const reset = useMutation({
    mutationFn: async () => {
      const { data, error } = await api.POST("/v1/organizations/{org_id}/users/{user_id}/mfa-reset", {
        params: { path: { org_id: orgId as string, user_id: userId } },
      });
      if (error !== undefined) throw asFailure(error);
      return data;
    },
    onSuccess: () => {
      setResetting(false);
      void queryClient.invalidateQueries({ queryKey: [...queryKeys.mfa, orgId] });
    },
  });

  const rows = factors.data?.factors ?? [];

  return (
    <>
      <p className="max-w-prose text-body text-text-secondary">
        Second factors are managed by the person they belong to. You can see what they have set up,
        and reset it if they have lost access.
      </p>

      <div className="mt-4">
        <FactorTable
          factors={rows}
          status={factors.isPending ? "loading" : factors.isError ? "error" : "ready"}
          errorKind={factors.error instanceof ApiFailure ? factors.error.kind : "server"}
          onRetry={() => void factors.refetch()}
          emptyText="This user has no second factor."
        />
      </div>

      {factors.data !== undefined ? (
        <p className="mt-3 text-small text-text-secondary">
          Unused recovery codes: {factors.data.recovery_codes_remaining}
        </p>
      ) : null}

      {hasRole("ORG_ADMIN", "ORG_OWNER", "INSTANCE_OWNER") && rows.length > 0 ? (
        <div className="mt-6 border-t border-border pt-4">
          <Button variant="danger-text" onClick={() => setResetting(true)}>
            Reset multi-factor
          </Button>
        </div>
      ) : null}

      <ConfirmDialog
        open={resetting}
        title="Reset this user's multi-factor?"
        verb="Reset"
        busy={reset.isPending}
        onCancel={() => setResetting(false)}
        onConfirm={() => reset.mutate()}
        consequence={
          <>
            <p>
              Every second factor and every recovery code this user has will be removed. Use this
              when they have lost the device they sign in with.
            </p>
            <p className="mt-2">
              If your organization requires multi-factor, they will be asked to set one up the next
              time they sign in. The reset is recorded in the audit log with your name.
            </p>
          </>
        }
      />
      {reset.isError ? (
        <p role="alert" className="mt-2 text-small text-text-primary">
          The reset did not go through. {reset.error instanceof Error ? reset.error.message : ""}
        </p>
      ) : null}
    </>
  );
}

// --- one's own --------------------------------------------------------------------------------

/**
 * The mandate, for somebody who does not yet meet it (P3-13).
 *
 * `P3-07` gives an organization's members fourteen days from the moment the
 * mandate is switched on, and wrote the warning for that period — which nothing
 * displayed. A person inside the grace learnt the rule existed at the sign-in
 * that would no longer let them through without enrolling. Said here, with the
 * date, while doing it is still a choice.
 *
 * Not `color-danger`: nothing is being destroyed, and nothing has gone wrong.
 */
function MandateNotice({ graceEndsAt }: { graceEndsAt: string | null }) {
  const deadline = graceEndsAt === null ? null : new Date(graceEndsAt);
  const known = deadline !== null && !Number.isNaN(deadline.getTime());
  // Read once, when the notice first renders: a deadline passing while the
  // page is open is not worth a timer, and the next load says so.
  const [now] = useState(() => Date.now());
  const passed = known && deadline.getTime() <= now;

  return (
    <div role="status" className="mb-4 rounded border border-border bg-bg-surface p-5">
      <h2 className="text-heading-3 font-medium text-text-primary">
        Your organization requires a second factor
      </h2>
      <p className="mt-1 max-w-prose text-body text-text-secondary">
        {!known
          ? "Add one now. You may be asked to set up an authenticator app the next time you sign in."
          : passed
            ? "The time to set one up has ended. You will be asked to set up an authenticator app the next time you sign in — add one now to do it here instead."
            : `Add one before ${deadline.toLocaleString()}. After that you will be asked to set up an authenticator app when you sign in, before you can continue.`}
      </p>
    </div>
  );
}

const PASSKEY_RECENCY_MS = 9 * 60 * 1000;

/** `mfa.RecoveryLowWaterMark` — when a user is told to replace their codes. */
const LOW_RECOVERY_CODES = 3;

/**
 * One's own factors, managed. Exported for personal settings (P3-12), which
 * shows the same component; `returnPath` is where re-authentication and the
 * hosted passkey page send the user back to.
 */
export function MyFactors({ orgId, returnPath }: { orgId: string | null; returnPath: string }) {
  const { login } = useAuth();
  const queryClient = useQueryClient();
  const mfa = useMyMfa(orgId, true);

  const [enrolling, setEnrolling] = useState(false);
  const [removing, setRemoving] = useState<Factor | null>(null);
  const [regenerating, setRegenerating] = useState(false);
  const [codes, setCodes] = useState<string[] | null>(null);
  const [needsSignIn, setNeedsSignIn] = useState(false);

  const refresh = () => void queryClient.invalidateQueries({ queryKey: [...queryKeys.mfa, orgId] });

  /** Routes a refusal: a stale session asks for sign-in; anything else is thrown on. */
  const handle = (error: unknown) => {
    if (error instanceof ApiFailure && error.code === "REAUTHENTICATION_REQUIRED") {
      setNeedsSignIn(true);
      return;
    }
    throw error;
  };

  const remove = useMutation({
    mutationFn: async (factor: Factor) => {
      const { error } = await api.DELETE("/v1/me/mfa/factors/{factor_id}", {
        params: { path: { factor_id: factor.id } },
      });
      if (error !== undefined) throw asFailure(error);
    },
    onSuccess: () => {
      setRemoving(null);
      refresh();
    },
    onError: (error) => {
      setRemoving(null);
      try {
        handle(error);
      } catch {
        // Shown below from the mutation's own error state.
      }
    },
  });

  const regenerate = useMutation({
    mutationFn: async () => {
      const { data, error } = await api.POST("/v1/me/mfa/recovery-codes", {});
      if (error !== undefined) throw asFailure(error);
      return data.codes;
    },
    onSuccess: (fresh) => {
      setRegenerating(false);
      setCodes(fresh);
      refresh();
    },
    onError: (error) => {
      setRegenerating(false);
      try {
        handle(error);
      } catch {
        // Shown below.
      }
    },
  });

  if (mfa.isPending) return <Skeleton className="h-40 w-full" />;
  if (mfa.isError) {
    return (
      <ErrorState
        kind={mfa.error instanceof ApiFailure ? mfa.error.kind : "server"}
        onRetry={() => void mfa.refetch()}
      />
    );
  }

  const { factors, recovery_codes_remaining: remaining, mfa_required: required, available_types: types } =
    mfa.data;
  const lastUnderMandate = required && factors.length === 1;
  const canTotp = types.includes("totp") && !factors.some((f) => f.type === "totp");
  const canPasskey = types.includes("webauthn");

  const addPasskey = () => {
    if (!signedInWithin(PASSKEY_RECENCY_MS)) {
      setNeedsSignIn(true);
      return;
    }
    const config = authConfig();
    const params = new URLSearchParams({
      client_id: config.clientId,
      return_to: `${window.location.origin}${returnPath}`,
    });
    window.location.assign(`${config.issuer}/account/passkeys?${params.toString()}`);
  };

  const failure = remove.error ?? regenerate.error;
  const shownFailure =
    failure instanceof ApiFailure && failure.code !== "REAUTHENTICATION_REQUIRED" ? failure : null;

  return (
    <>
      {types.length === 0 ? (
        <div className="rounded border border-border bg-bg-surface p-5">
          <h2 className="text-heading-3 font-medium text-text-primary">
            Second factors are not available on this service
          </h2>
          <p className="mt-1 max-w-prose text-body text-text-secondary">
            Ask the people who run it to turn multi-factor authentication on.
          </p>
        </div>
      ) : null}

      {needsSignIn ? (
        <div role="alert" className="mb-4 rounded border border-border bg-bg-surface p-5">
          <h2 className="text-heading-3 font-medium text-text-primary">Sign in again to continue</h2>
          <p className="mt-1 max-w-prose text-body text-text-secondary">
            For your security, changing how you sign in needs a recent sign-in. You will come back
            here afterwards.
          </p>
          <p className="mt-3">
            <Button variant="primary" onClick={() => void login(returnPath, { reauthenticate: true })}>
              Sign in again
            </Button>
          </p>
        </div>
      ) : null}

      {required && factors.length === 0 && types.length > 0 ? (
        <MandateNotice graceEndsAt={mfa.data.grace_ends_at ?? null} />
      ) : required ? (
        <p className="mb-3 max-w-prose text-body text-text-secondary">
          Your organization requires a second factor to sign in.
        </p>
      ) : null}

      <FactorTable
        factors={factors}
        status="ready"
        errorKind="server"
        onRetry={refresh}
        emptyText="No second factor yet. Add one so a stolen password is not enough to get into your account."
        action={(factor) => (
          <>
            <Button
              variant="danger-text"
              disabled={lastUnderMandate}
              aria-describedby={lastUnderMandate ? "last-factor-reason" : undefined}
              aria-label={`Remove ${describe(factor)}`}
              onClick={() => setRemoving(factor)}
            >
              Remove
            </Button>
          </>
        )}
      />
      {lastUnderMandate ? (
        <p id="last-factor-reason" className="mt-2 max-w-prose text-small text-text-secondary">
          This is your only second factor and your organization requires one, so it cannot be
          removed. Add another first.
        </p>
      ) : null}

      {factors.length > 0 && remaining === 0 ? (
        <div role="status" className="mt-4 rounded border border-border bg-bg-surface p-5">
          <h2 className="text-heading-3 font-medium text-text-primary">You have no recovery codes</h2>
          <p className="mt-1 max-w-prose text-body text-text-secondary">
            If you lose your second factor, recovery codes are the way back into your account.
            Generate a set and keep it somewhere safe.
          </p>
        </div>
      ) : null}

      {factors.length > 0 && remaining > 0 && remaining <= LOW_RECOVERY_CODES ? (
        // P3-04 F-4: warn while there are still codes to spend, not after.
        <div role="status" className="mt-4 rounded border border-border bg-bg-surface p-5">
          <h2 className="text-heading-3 font-medium text-text-primary">
            Only {remaining} recovery {remaining === 1 ? "code" : "codes"} left
          </h2>
          <p className="mt-1 max-w-prose text-body text-text-secondary">
            Replace them before you run out, while you can still sign in.
          </p>
        </div>
      ) : null}

      {factors.length > 0 && remaining > LOW_RECOVERY_CODES ? (
        <p className="mt-3 text-small text-text-secondary">Unused recovery codes: {remaining}</p>
      ) : null}

      <div className="mt-6 flex flex-wrap gap-2 border-t border-border pt-4">
        {canTotp ? (
          <Button variant="primary" onClick={() => setEnrolling(true)}>
            Add authenticator app
          </Button>
        ) : null}
        {canPasskey ? <Button onClick={addPasskey}>Add passkey</Button> : null}
        {factors.length > 0 ? (
          <Button onClick={() => setRegenerating(true)}>
            {remaining === 0 ? "Generate recovery codes" : "Replace recovery codes"}
          </Button>
        ) : null}
      </div>

      {shownFailure !== null ? (
        <p role="alert" className="mt-3 max-w-prose text-small text-text-primary">
          {shownFailure.message}
        </p>
      ) : null}

      {enrolling ? (
        <EnrolAuthenticator
          onClose={(issued) => {
            setEnrolling(false);
            refresh();
            if (issued !== null) setCodes(issued);
          }}
          onNeedsSignIn={() => {
            setEnrolling(false);
            setNeedsSignIn(true);
          }}
        />
      ) : null}

      <ConfirmDialog
        open={removing !== null}
        title={`Remove ${removing !== null ? describe(removing) : "this factor"}?`}
        verb="Remove"
        busy={remove.isPending}
        onCancel={() => setRemoving(null)}
        onConfirm={() => removing !== null && remove.mutate(removing)}
        consequence={
          <p>
            You will no longer be able to use it to sign in. {factors.length === 1
              ? "It is your only second factor, so your sign-in will be protected by your password alone."
              : "Your other second factors keep working."}
          </p>
        }
      />

      <ConfirmDialog
        open={regenerating}
        title={remaining === 0 ? "Generate recovery codes?" : "Replace your recovery codes?"}
        verb={remaining === 0 ? "Generate" : "Replace"}
        busy={regenerate.isPending}
        onCancel={() => setRegenerating(false)}
        onConfirm={() => regenerate.mutate()}
        consequence={
          <p>
            {remaining === 0
              ? "You will get ten new single-use codes, shown once."
              : "Your current codes stop working immediately, used or not. You will get ten new ones, shown once."}
          </p>
        }
      />

      {codes !== null ? <RecoveryCodesDialog codes={codes} onDone={() => setCodes(null)} /> : null}
    </>
  );
}

// --- enrolling an authenticator app -----------------------------------------------------------

function EnrolAuthenticator({
  onClose,
  onNeedsSignIn,
}: {
  onClose: (issuedCodes: string[] | null) => void;
  onNeedsSignIn: () => void;
}) {
  const [code, setCode] = useState("");
  const [problem, setProblem] = useState<string | null>(null);

  const begin = useMutation({
    mutationFn: async () => {
      const { data, error } = await api.POST("/v1/me/mfa/totp", { body: {} });
      if (error !== undefined) throw asFailure(error);
      return data;
    },
    onError: (error) => {
      if (error instanceof ApiFailure && error.code === "REAUTHENTICATION_REQUIRED") onNeedsSignIn();
    },
  });

  const confirm = useMutation({
    mutationFn: async (factorId: string) => {
      const { data, error } = await api.POST("/v1/me/mfa/totp/{factor_id}/confirm", {
        params: { path: { factor_id: factorId } },
        body: { code: code.trim() },
      });
      if (error !== undefined) throw asFailure(error);
      return data;
    },
    onSuccess: (confirmed) => onClose(confirmed.recovery_codes ?? null),
    onError: (error) => {
      if (!(error instanceof ApiFailure)) {
        setProblem("Something went wrong. Please try again.");
      } else if (error.code === "VALIDATION_ERROR") {
        setProblem("That code didn't match. Codes change every 30 seconds — try the one showing now.");
      } else if (error.code === "NOT_FOUND") {
        setProblem("This setup expired. Close this and start again.");
      } else if (error.code === "RATE_LIMITED") {
        setProblem("Too many attempts. Wait a few minutes and try again.");
      } else {
        setProblem(error.message);
      }
    },
  });

  // Begun once, when the dialog opens: the secret is created by this request
  // and shown by it, and there is nothing to show until it answers. In an
  // effect with no dependencies on state that changes, so a re-render cannot
  // start a second enrolment and replace the secret the user is scanning.
  const { mutate: start } = begin;
  useEffect(() => {
    start();
  }, [start]);

  const enrolment = begin.data;

  return (
    <Modal
      open
      title="Add an authenticator app"
      onClose={() => onClose(null)}
      footer={
        <>
          <Button onClick={() => onClose(null)}>Cancel</Button>
          <Button
            variant="primary"
            loading={confirm.isPending}
            disabled={enrolment === undefined || code.trim().length !== 6}
            onClick={() => enrolment !== undefined && confirm.mutate(enrolment.factor_id)}
          >
            Confirm
          </Button>
        </>
      }
    >
      {begin.isPending ? <Skeleton className="h-48 w-48" /> : null}
      {begin.isError && !(begin.error instanceof ApiFailure && begin.error.code === "REAUTHENTICATION_REQUIRED") ? (
        <p role="alert" className="text-body text-text-primary">
          {begin.error instanceof Error ? begin.error.message : "The setup could not start."}
        </p>
      ) : null}

      {enrolment !== undefined ? (
        <>
          <ol className="list-decimal space-y-3 pl-5 text-body text-text-primary">
            <li>
              Open your authenticator app and scan this code.
              <div className="mt-2">
                <QrCode rows={enrolment.qr.rows} label="QR code for setting up your authenticator app" />
              </div>
            </li>
            <li>
              Can't scan it? Enter this key instead:
              <p className="mt-1 break-all font-mono text-body" aria-label="Setup key">
                {groupKey(enrolment.secret)}
              </p>
            </li>
            <li>
              <label htmlFor="totp-code" className="block">
                Enter the 6-digit code the app shows
              </label>
              <input
                id="totp-code"
                inputMode="numeric"
                autoComplete="one-time-code"
                maxLength={6}
                value={code}
                onChange={(event) => {
                  setCode(event.target.value.replace(/\D/g, ""));
                  setProblem(null);
                }}
                aria-invalid={problem !== null}
                aria-describedby={problem !== null ? "totp-code-problem" : undefined}
                className="mt-1 w-40 rounded border border-border bg-bg-surface px-3 py-2 font-mono text-body"
              />
              {problem !== null ? (
                <p id="totp-code-problem" role="alert" className="mt-1 text-small text-text-primary">
                  {problem}
                </p>
              ) : null}
            </li>
          </ol>
        </>
      ) : null}
    </Modal>
  );
}

// --- recovery codes, shown once -----------------------------------------------------------------

/**
 * The one time a person sees their recovery codes.
 *
 * Not dismissible until they say they have saved them. A dialog closed by an
 * accidental Escape is a set of codes nobody wrote down, and the next time they
 * are needed is the day a phone is lost — which is not a day to discover that.
 */
export function RecoveryCodesDialog({ codes, onDone }: { codes: string[]; onDone: () => void }) {
  const [saved, setSaved] = useState(false);
  const [copied, setCopied] = useState(false);
  const text = codes.join("\n");

  const download = () => {
    const url = URL.createObjectURL(new Blob([`${text}\n`], { type: "text/plain" }));
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = "recovery-codes.txt";
    anchor.click();
    URL.revokeObjectURL(url);
  };

  return (
    <Modal
      open
      dismissible={false}
      title="Save your recovery codes"
      onClose={onDone}
      footer={
        <Button variant="primary" disabled={!saved} onClick={onDone}>
          Done
        </Button>
      }
    >
      <p className="text-body text-text-primary">
        <strong>These codes will not be shown again.</strong> Each one gets you into your account
        once if you lose your second factor. Keep them somewhere safe and private.
      </p>
      <ul aria-label="Recovery codes" className="mt-3 grid grid-cols-2 gap-2 font-mono text-body">
        {codes.map((c) => (
          <li key={c}>{c}</li>
        ))}
      </ul>
      <div className="mt-3 flex gap-2">
        <Button
          onClick={() => {
            void navigator.clipboard?.writeText(text).then(() => setCopied(true));
          }}
        >
          {copied ? "Copied" : "Copy"}
        </Button>
        <Button onClick={download}>Download</Button>
      </div>
      <label className="mt-4 flex items-center gap-2 text-body text-text-primary">
        <input type="checkbox" checked={saved} onChange={(event) => setSaved(event.target.checked)} />
        I have saved these codes
      </label>
    </Modal>
  );
}

// --- shared -------------------------------------------------------------------------------------

function FactorTable({
  factors,
  status,
  errorKind,
  onRetry,
  emptyText,
  action,
}: {
  factors: Factor[];
  status: "loading" | "error" | "ready";
  errorKind: "network" | "server" | "permission" | "validation";
  onRetry: () => void;
  emptyText: string;
  action?: (factor: Factor) => React.ReactNode;
}) {
  const columns: Column<Factor>[] = [
    {
      key: "type",
      header: "Type",
      cell: (factor) => <Badge tone="neutral">{factor.type === "totp" ? "Authenticator app" : "Passkey"}</Badge>,
    },
    { key: "name", header: "Name", cell: (factor) => factor.label ?? "—" },
    { key: "added", header: "Added", cell: (factor) => factor.created_at.slice(0, 10) },
    {
      key: "used",
      header: "Last used",
      cell: (factor) => (factor.last_used_at !== null && factor.last_used_at !== undefined ? factor.last_used_at.slice(0, 10) : "Never"),
    },
  ];
  if (action !== undefined) {
    columns.push({ key: "actions", header: "Actions", cell: action });
  }

  if (status === "ready" && factors.length === 0) {
    return (
      <div className="rounded border border-border bg-bg-surface p-5">
        <p className="max-w-prose text-body text-text-secondary">{emptyText}</p>
      </div>
    );
  }

  return (
    <Table<Factor>
      caption="Second factors"
      columns={columns}
      rows={factors}
      rowKey={(factor) => factor.id}
      status={status}
      errorKind={errorKind}
      onRetry={onRetry}
      what="second factors"
      filtered={false}
    />
  );
}

function describe(factor: Factor): string {
  const kind = factor.type === "totp" ? "authenticator app" : "passkey";
  return factor.label !== null && factor.label !== undefined ? `${kind} “${factor.label}”` : kind;
}

/** Groups a base32 key in fours, the way people copy it by eye. */
function groupKey(secret: string): string {
  return secret.replace(/(.{4})/g, "$1 ").trim();
}
