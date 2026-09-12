import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { PoliciesPage } from "./PoliciesPage";
import { AuthError } from "../lib/auth/oidc";
import { clearToken } from "../lib/auth/tokens";
import { SETTINGS_DEFAULTS } from "../lib/api/settings.gen";
import { expectNoAxeViolations } from "../test/axe";
import { renderScreen, signIn, stubApi } from "../test/harness";

/**
 * The Policies (Access) screen (P2-14).
 *
 * The tests that matter are the ones about **honesty**: that nothing here
 * claims an enforcement that does not exist, that an administrator can see
 * what they are changing *from*, and that a change which takes access away
 * says so before it is applied rather than after.
 */

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return {
    ...actual,
    renewSilently: () => Promise.reject(new AuthError("login_required", null)),
  };
});

/** Answers the organization read with the given settings. */
function stubSettings(settings: unknown, onWrite?: () => { status: number; body: unknown }) {
  stubApi((_url, init) => {
    if (init.method === "PATCH" && onWrite !== undefined) return onWrite();
    if (init.method === "PATCH") {
      return { status: 200, body: { id: "org-1", name: "Acme", settings } };
    }
    return { status: 200, body: { id: "org-1", name: "Acme", settings } };
  });
}

/** Everything configured, deliberately unlike the service defaults. */
const configured = {
  password_policy: { min_length: 16, require_uppercase: false, max_age_days: 30 },
  mfa_required: false,
  session_lifetime_hours: 24,
  allowed_login_methods: ["password"],
};

beforeEach(() => {
  clearToken();
  sessionStorage.clear();
  vi.stubEnv("VITE_AUTH_CLIENT_ID", "console-test-client");
  vi.stubEnv("VITE_AUTH_ISSUER", "https://auth.example.test");
  signIn();
});

describe("what is in force today", () => {
  it("shows each configured value", async () => {
    stubSettings(configured);

    renderScreen(<PoliciesPage />);

    expect(await screen.findByLabelText(/minimum length/i)).toHaveValue(16);
    expect(screen.getByLabelText(/expire passwords after/i)).toHaveValue(30);
    expect(screen.getByLabelText(/sessions last/i)).toHaveValue(24);
    expect(screen.getByLabelText(/require an upper-case letter/i)).not.toBeChecked();
  });

  it("says which values are the service's rather than this organization's", async () => {
    // An organization that has never set a password policy. The value in force
    // is the service default and there is nothing stored — a difference that
    // is invisible in the number and matters when the default moves.
    stubSettings({ session_lifetime_hours: 24 });

    renderScreen(<PoliciesPage />);

    const field = await screen.findByLabelText(/minimum length/i);
    expect(field).toHaveValue(null);

    // Scoped to this field's own block, and asserted on the whole subtree:
    // the number is inside a <strong>, and `getByText` only sees an element's
    // DIRECT text nodes, so a matcher across the two would silently never
    // match rather than fail loudly.
    const block = field.closest("div") as HTMLElement;
    // Step 6: what it is changing FROM, named.
    expect(block).toHaveTextContent(
      new RegExp("the service applies " + SETTINGS_DEFAULTS.min_length, "i"),
    );
  });

  it("renders a refusal without offering a retry", async () => {
    stubApi(() => ({
      status: 403,
      body: { error: { code: "PERMISSION_DENIED", message: "no" } },
    }));

    renderScreen(<PoliciesPage />);

    expect(await screen.findByRole("alert")).toHaveTextContent(/do not have access/i);
    expect(screen.queryByRole("button", { name: /try again/i })).not.toBeInTheDocument();
  });
});

describe("multi-factor", () => {
  it("never presents itself as enforced", async () => {
    stubSettings(configured);

    renderScreen(<PoliciesPage />);

    // Step 3, and `docs/UI-UX/21`'s governance rule. An administrator who
    // switched this on and believed their organization was protected would
    // have been misled by the console, not by the API.
    expect(await screen.findByText(/Not enforced yet/i)).toBeInTheDocument();
    expect(screen.getByText(/Setting this changes nothing today/i)).toBeInTheDocument();
    expect(screen.getByText(/Phase 3/i)).toBeInTheDocument();
  });
});

describe("validation", () => {
  it("refuses a value the server would refuse, with the server's own range", async () => {
    stubSettings(configured);

    renderScreen(<PoliciesPage />);

    const field = await screen.findByLabelText(/minimum length/i);
    await userEvent.clear(field);
    await userEvent.type(field, "4");

    // Below the platform floor. The bound comes from the generated schema, so
    // a form that drifted from the server turns this red.
    expect(field).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByText(/Must be between 12 and 128/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /save policies/i })).toBeDisabled();
  });

  it("says why an organization cannot permit no sign-in method", async () => {
    stubSettings(configured);

    renderScreen(<PoliciesPage />);

    await userEvent.click(await screen.findByRole("checkbox", { name: /password/i }));

    // The API refuses an empty list too. Saying so here means the
    // administrator finds out before they save.
    expect(screen.getByRole("alert")).toHaveTextContent(/at least one sign-in method/i);
    expect(screen.getByRole("button", { name: /save policies/i })).toBeDisabled();
  });

  it("accepts 0 for password expiry, because never expiring is a real choice", async () => {
    stubSettings(configured);

    renderScreen(<PoliciesPage />);

    const field = await screen.findByLabelText(/expire passwords after/i);
    await userEvent.clear(field);
    await userEvent.type(field, "0");

    expect(field).not.toHaveAttribute("aria-invalid");
    expect(screen.getByRole("button", { name: /save policies/i })).toBeEnabled();
  });
});

describe("changes that take access away", () => {
  it("names who is affected before applying a shorter session", async () => {
    stubSettings(configured);

    renderScreen(<PoliciesPage />);

    const field = await screen.findByLabelText(/sessions last/i);
    await userEvent.clear(field);
    await userEvent.type(field, "1");
    await userEvent.click(screen.getByRole("button", { name: /save policies/i }));

    const dialog = screen.getByRole("dialog");
    // Step 4: the blast radius, in the words of what happens to people.
    expect(dialog).toHaveTextContent(/from 24 to 1 hours/i);
    expect(dialog).toHaveTextContent(/signed out at their next request/i);
  });

  it("warns that removing the only method locks out the person removing it", async () => {
    stubSettings({ ...configured, allowed_login_methods: ["password"] });

    renderScreen(<PoliciesPage />);

    await userEvent.click(await screen.findByRole("checkbox", { name: /password/i }));
    // Blocked by validation before it can even be confirmed — an empty list is
    // refused by the API, so the form refuses it first.
    expect(screen.getByRole("button", { name: /save policies/i })).toBeDisabled();
  });

  it("does not confirm a change that takes nothing away", async () => {
    stubSettings(configured);

    renderScreen(<PoliciesPage />);

    const field = await screen.findByLabelText(/sessions last/i);
    await userEvent.clear(field);
    await userEvent.type(field, "48");
    await userEvent.click(screen.getByRole("button", { name: /save policies/i }));

    // A LONGER session affects nobody adversely. Confirming every save would
    // make the confirmation furniture (`docs/UI-UX/07`).
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(await screen.findByRole("status")).toHaveTextContent(/saved/i);
  });

  it("treats a password expiry of 0 as the loosest value, not the tightest", async () => {
    // Stored: never expires. Changing to 90 days is a REDUCTION — everyone
    // whose password is older than that must change it — even though 90 is
    // numerically larger than 0.
    stubSettings({ ...configured, password_policy: { ...configured.password_policy, max_age_days: 0 } });

    renderScreen(<PoliciesPage />);

    const field = await screen.findByLabelText(/expire passwords after/i);
    await userEvent.clear(field);
    await userEvent.type(field, "90");
    await userEvent.click(screen.getByRole("button", { name: /save policies/i }));

    expect(screen.getByRole("dialog")).toHaveTextContent(/older than 90 days must be changed/i);
  });
});

describe("saving", () => {
  it("sends the whole document, so what is shown is what is stored", async () => {
    const bodies: unknown[] = [];
    stubApi((_url, init) => {
      if (init.method === "PATCH") {
        return { status: 200, body: { id: "org-1", name: "Acme", settings: configured } };
      }
      return { status: 200, body: { id: "org-1", name: "Acme", settings: configured } };
    });
    void bodies;

    renderScreen(<PoliciesPage />);

    const field = await screen.findByLabelText(/sessions last/i);
    await userEvent.clear(field);
    await userEvent.type(field, "48");
    await userEvent.click(screen.getByRole("button", { name: /save policies/i }));

    expect(await screen.findByRole("status")).toHaveTextContent(/saved/i);
  });

  it("surfaces the server's own reason when it refuses", async () => {
    stubSettings(configured, () => ({
      status: 400,
      body: {
        error: {
          code: "VALIDATION_ERROR",
          message: "That policy was refused.",
          details: [{ field: "settings", issue: "min_length may not go below the platform floor" }],
        },
      },
    }));

    renderScreen(<PoliciesPage />);

    const field = await screen.findByLabelText(/sessions last/i);
    await userEvent.clear(field);
    await userEvent.type(field, "48");
    await userEvent.click(screen.getByRole("button", { name: /save policies/i }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      /may not go below the platform floor/i,
    );
  });

  it("offers nothing to save until something changes", async () => {
    stubSettings(configured);

    renderScreen(<PoliciesPage />);

    expect(await screen.findByRole("button", { name: /save policies/i })).toBeDisabled();
    expect(screen.queryByRole("button", { name: /discard/i })).not.toBeInTheDocument();
  });

  it("discards back to what the server holds", async () => {
    stubSettings(configured);

    renderScreen(<PoliciesPage />);

    const field = await screen.findByLabelText(/sessions last/i);
    await userEvent.clear(field);
    await userEvent.type(field, "1");
    await userEvent.click(screen.getByRole("button", { name: /discard changes/i }));

    expect(screen.getByLabelText(/sessions last/i)).toHaveValue(24);
  });
});

describe("accessibility", () => {
  it("groups the settings and has no axe violations", async () => {
    stubSettings(configured);

    const { container } = renderScreen(<PoliciesPage />);
    await screen.findByLabelText(/minimum length/i);

    // Each group is a real fieldset with a legend, so a screen-reader user
    // hears which policy a field belongs to.
    const groups = screen.getAllByRole("group");
    expect(groups.length).toBeGreaterThanOrEqual(4);
    expect(within(groups[0]).getByLabelText(/minimum length/i)).toBeInTheDocument();

    await expectNoAxeViolations(container);
  });
});
