import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { MfaTab } from "./MfaTab";
import { AuthError } from "../lib/auth/oidc";
import { clearToken } from "../lib/auth/tokens";
import { expectNoAxeViolations } from "../test/axe";
import { renderScreen, signIn, stubApi } from "../test/harness";

/**
 * The Multi-factor tab (P3-10).
 *
 * What these protect is the split `docs/UI-UX/08` specifies and the honesty
 * around it: no management control on somebody else's page, a removal the API
 * would refuse is refused visibly first, codes are shown once behind an
 * acknowledgement, and a stale session is told to sign in again rather than
 * told it is not allowed.
 */

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return {
    ...actual,
    renewSilently: () => Promise.reject(new AuthError("login_required", null)),
  };
});

const SELF = "admin-1";
const MEMBER = "member-2";

const totpFactor = {
  id: "11111111-1111-1111-1111-111111111111",
  type: "totp",
  label: "phone",
  created_at: "2026-09-01T10:00:00Z",
  last_used_at: null,
};

function mine(overrides: Record<string, unknown> = {}) {
  return {
    factors: [],
    recovery_codes_remaining: 0,
    mfa_required: false,
    available_types: ["totp", "webauthn"],
    ...overrides,
  };
}

const grid = Array.from({ length: 21 }, (_, y) =>
  Array.from({ length: 21 }, (_, x) => ((x + y) % 3 === 0 ? "1" : "0")).join(""),
);

beforeEach(() => {
  clearToken();
  sessionStorage.clear();
  vi.stubEnv("VITE_AUTH_CLIENT_ID", "console-test-client");
  vi.stubEnv("VITE_AUTH_ISSUER", "https://auth.example.test");
  signIn();
});

// --- somebody else's -----------------------------------------------------------------------

describe("a member's factors", () => {
  it("are read-only, with the reset for an administrator", async () => {
    stubApi((url) => {
      if (url.includes(`/users/${MEMBER}/mfa`)) {
        return { status: 200, body: { factors: [totpFactor], recovery_codes_remaining: 7 } };
      }
      return { status: 404, body: { error: { code: "NOT_FOUND", message: "x" } } };
    });

    renderScreen(<MfaTab orgId="org-1" userId={MEMBER} />);

    expect(await screen.findByText("Authenticator app")).toBeInTheDocument();
    expect(screen.getByText("Unused recovery codes: 7")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /remove/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /add authenticator/i })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Reset multi-factor" })).toBeInTheDocument();
  });
});

// --- one's own -------------------------------------------------------------------------------

describe("one's own factors", () => {
  it("offers both factor types when there are none", async () => {
    stubApi(() => ({ status: 200, body: mine() }));
    const { container } = renderScreen(<MfaTab orgId="org-1" userId={SELF} />);

    expect(await screen.findByRole("button", { name: "Add authenticator app" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add passkey" })).toBeInTheDocument();
    expect(screen.getByText(/No second factor yet/)).toBeInTheDocument();
    await expectNoAxeViolations(container);
  });

  it("says so when the service has no MFA", async () => {
    stubApi(() => ({ status: 200, body: mine({ available_types: [] }) }));
    renderScreen(<MfaTab orgId="org-1" userId={SELF} />);

    expect(await screen.findByText("Second factors are not available on this service")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add authenticator app" })).not.toBeInTheDocument();
  });

  it("refuses to remove the last factor under a mandate, and says why", async () => {
    stubApi(() => ({
      status: 200,
      body: mine({ factors: [totpFactor], recovery_codes_remaining: 10, mfa_required: true }),
    }));
    renderScreen(<MfaTab orgId="org-1" userId={SELF} />);

    const remove = await screen.findByRole("button", { name: /Remove authenticator app/ });
    expect(remove).toBeDisabled();
    expect(remove).toHaveAttribute("aria-describedby", "last-factor-reason");
    expect(screen.getByText(/cannot be removed/)).toBeInTheDocument();
  });

  it("tells somebody inside the grace when the mandate starts applying to them", async () => {
    const deadline = new Date(Date.now() + 5 * 24 * 60 * 60 * 1000).toISOString();
    stubApi(() => ({ status: 200, body: mine({ mfa_required: true, grace_ends_at: deadline }) }));
    const { container } = renderScreen(<MfaTab orgId="org-1" userId={SELF} />);

    const notice = await screen.findByRole("status");
    expect(notice).toHaveTextContent("Your organization requires a second factor");
    expect(notice).toHaveTextContent(`Add one before ${new Date(deadline).toLocaleString()}`);
    await expectNoAxeViolations(container);
  });

  it("says the grace is over once it is, without inventing a date", async () => {
    stubApi(() => ({
      status: 200,
      body: mine({ mfa_required: true, grace_ends_at: "2020-01-15T00:00:00Z" }),
    }));
    renderScreen(<MfaTab orgId="org-1" userId={SELF} />);

    expect(await screen.findByRole("status")).toHaveTextContent(/has ended/);
  });

  it("does not warn somebody who already meets the mandate", async () => {
    stubApi(() => ({
      status: 200,
      body: mine({
        factors: [totpFactor],
        recovery_codes_remaining: 10,
        mfa_required: true,
        grace_ends_at: new Date(Date.now() + 86_400_000).toISOString(),
      }),
    }));
    renderScreen(<MfaTab orgId="org-1" userId={SELF} />);

    expect(await screen.findByText("Authenticator app")).toBeInTheDocument();
    expect(screen.queryByText(/Add one before/)).not.toBeInTheDocument();
  });

  it("warns plainly when a factor has no recovery codes", async () => {
    stubApi(() => ({ status: 200, body: mine({ factors: [totpFactor], recovery_codes_remaining: 0 }) }));
    renderScreen(<MfaTab orgId="org-1" userId={SELF} />);

    expect(await screen.findByText("You have no recovery codes")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Generate recovery codes" })).toBeInTheDocument();
  });

  it("asks for a fresh sign-in when the session is too old, rather than refusing", async () => {
    stubApi((url, init) => {
      if (url.endsWith("/v1/me/mfa/recovery-codes") && init.method === "POST") {
        return {
          status: 403,
          body: { error: { code: "REAUTHENTICATION_REQUIRED", message: "sign in again" } },
        };
      }
      return { status: 200, body: mine({ factors: [totpFactor], recovery_codes_remaining: 5 }) };
    });
    renderScreen(<MfaTab orgId="org-1" userId={SELF} />);

    await userEvent.click(await screen.findByRole("button", { name: "Replace recovery codes" }));
    await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Replace" }));

    expect(await screen.findByText("Sign in again to continue")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Sign in again" })).toBeInTheDocument();
    expect(screen.queryByText(/not allowed|permission/i)).not.toBeInTheDocument();
  });

  it("sends a stale session to sign in before the passkey page, not after", async () => {
    stubApi(() => ({ status: 200, body: mine() }));
    const assign = vi.fn();
    vi.stubGlobal("location", { ...window.location, assign, origin: "https://console.example.test" });

    renderScreen(<MfaTab orgId="org-1" userId={SELF} />);
    await userEvent.click(await screen.findByRole("button", { name: "Add passkey" }));

    expect(await screen.findByText("Sign in again to continue")).toBeInTheDocument();
    expect(assign).not.toHaveBeenCalled();
    vi.unstubAllGlobals();
  });
});

// --- enrolling ---------------------------------------------------------------------------------

describe("adding an authenticator app", () => {
  it("shows the code with a text alternative, checks the code, then shows codes once", async () => {
    let confirmCalls = 0;
    stubApi((url, init) => {
      if (url.endsWith("/v1/me/mfa/totp") && init.method === "POST") {
        return {
          status: 201,
          body: {
            factor_id: totpFactor.id,
            secret: "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP",
            provisioning_uri: "otpauth://totp/x",
            qr: { size: 21, rows: grid },
            digits: 6,
            period: 30,
          },
        };
      }
      if (url.includes("/confirm") && init.method === "POST") {
        confirmCalls++;
        if (confirmCalls === 1) {
          return {
            status: 400,
            body: { error: { code: "VALIDATION_ERROR", message: "no", details: [{ field: "code", issue: "x" }] } },
          };
        }
        return {
          status: 200,
          body: {
            factor: totpFactor,
            recovery_codes: ["AAAA-BBBB-CCCC-DDDD", "EEEE-FFFF-GGGG-HHHH"],
          },
        };
      }
      return { status: 200, body: mine() };
    });

    renderScreen(<MfaTab orgId="org-1" userId={SELF} />);
    await userEvent.click(await screen.findByRole("button", { name: "Add authenticator app" }));

    const dialog = await screen.findByRole("dialog");
    expect(await within(dialog).findByRole("img", { name: /QR code/ })).toBeInTheDocument();
    // The accessible alternative is the key itself, as text.
    expect(within(dialog).getByText("JBSW Y3DP EHPK 3PXP JBSW Y3DP EHPK 3PXP")).toBeInTheDocument();

    const field = within(dialog).getByLabelText(/6-digit code/);
    await userEvent.type(field, "123456");
    await userEvent.click(within(dialog).getByRole("button", { name: "Confirm" }));
    expect(await within(dialog).findByText(/didn't match/)).toBeInTheDocument();
    expect(field).toHaveAttribute("aria-invalid", "true");

    await userEvent.clear(field);
    await userEvent.type(field, "654321");
    await userEvent.click(within(dialog).getByRole("button", { name: "Confirm" }));

    const codes = await screen.findByRole("dialog", { name: "Save your recovery codes" });
    expect(within(codes).getByText("AAAA-BBBB-CCCC-DDDD")).toBeInTheDocument();
    const done = within(codes).getByRole("button", { name: "Done" });
    expect(done).toBeDisabled();
    await userEvent.click(within(codes).getByLabelText("I have saved these codes"));
    expect(done).toBeEnabled();
  });
});
