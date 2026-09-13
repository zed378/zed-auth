import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { AccountPage } from "./AccountPage";
import { AuthError } from "../lib/auth/oidc";
import { clearToken } from "../lib/auth/tokens";
import { expectNoAxeViolations } from "../test/axe";
import { renderScreen, signIn, stubApi } from "../test/harness";

/**
 * Personal account settings (P3-12).
 *
 * What these protect: the password rules are visible before anything is typed,
 * a refusal lands on the field it is about, the unshipped social sign-in says
 * it is unavailable rather than looking broken, and the screen is clean for
 * assistive technology.
 */

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return {
    ...actual,
    renewSilently: () => Promise.reject(new AuthError("login_required", null)),
  };
});

const me = {
  id: "11111111-1111-1111-1111-111111111111",
  email: "admin@example.test",
  display_name: "Admin",
  organization: { id: "22222222-2222-2222-2222-222222222222", name: "Acme" },
  password_policy: { min_length: 14, require_uppercase: true, max_age_days: 90, breach_checked: true },
  password_changed_at: null,
};

function api(onPassword?: () => { status: number; body: unknown }) {
  stubApi((url, init) => {
    if (url.endsWith("/v1/me/password") && init.method === "POST" && onPassword !== undefined) {
      return onPassword();
    }
    if (url.endsWith("/v1/me")) return { status: 200, body: me };
    if (url.endsWith("/v1/me/mfa")) {
      return {
        status: 200,
        body: { factors: [], recovery_codes_remaining: 0, mfa_required: false, available_types: ["totp"] },
      };
    }
    if (url.includes("/v1/me/sessions")) return { status: 200, body: { sessions: [] } };
    return { status: 404, body: { error: { code: "NOT_FOUND", message: "x" } } };
  });
}

beforeEach(() => {
  clearToken();
  sessionStorage.clear();
  vi.stubEnv("VITE_AUTH_CLIENT_ID", "console-test-client");
  vi.stubEnv("VITE_AUTH_ISSUER", "https://auth.example.test");
  signIn();
});

describe("personal account settings", () => {
  it("shows the profile and the password rules before anything is typed", async () => {
    api();
    const { container } = renderScreen(<AccountPage />);

    expect(await screen.findByText("admin@example.test")).toBeInTheDocument();
    expect(screen.getByText("Acme")).toBeInTheDocument();
    expect(screen.getByText("At least 14 characters")).toBeInTheDocument();
    expect(screen.getByText("At least one uppercase letter")).toBeInTheDocument();
    expect(screen.getByText(/known data breach/)).toBeInTheDocument();
    expect(screen.getByText(/expire after 90 days/)).toBeInTheDocument();
    // The requirements describe the field that must meet them.
    expect(screen.getByLabelText("New password")).toHaveAttribute("aria-describedby", "password-requirements");
    await expectNoAxeViolations(container);
  });

  it("puts a refusal on the field it is about", async () => {
    api(() => ({
      status: 400,
      body: {
        error: {
          code: "VALIDATION_ERROR",
          message: "no",
          details: [{ field: "current_password", issue: "is not correct" }],
        },
      },
    }));
    renderScreen(<AccountPage />);

    await userEvent.type(await screen.findByLabelText("Current password"), "wrong");
    await userEvent.type(screen.getByLabelText("New password"), "A New Password 123");
    await userEvent.type(screen.getByLabelText("Confirm new password"), "A New Password 123");
    await userEvent.click(screen.getByRole("button", { name: "Change password" }));

    const current = screen.getByLabelText("Current password");
    expect(await screen.findByText("is not correct")).toBeInTheDocument();
    expect(current).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByLabelText("New password")).toHaveAttribute("aria-invalid", "false");
  });

  it("refuses to submit when the two new passwords differ", async () => {
    let posted = 0;
    api(() => {
      posted++;
      return { status: 204, body: null };
    });
    renderScreen(<AccountPage />);

    await userEvent.type(await screen.findByLabelText("Current password"), "current");
    await userEvent.type(screen.getByLabelText("New password"), "one thing");
    await userEvent.type(screen.getByLabelText("Confirm new password"), "another");

    expect(screen.getByText("The two new passwords do not match.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Change password" })).toBeDisabled();
    expect(posted).toBe(0);
  });

  it("says the change happened and that other sessions were signed out", async () => {
    api(() => ({ status: 204, body: null }));
    renderScreen(<AccountPage />);

    await userEvent.type(await screen.findByLabelText("Current password"), "current");
    await userEvent.type(screen.getByLabelText("New password"), "A New Password 123");
    await userEvent.type(screen.getByLabelText("Confirm new password"), "A New Password 123");
    await userEvent.click(screen.getByRole("button", { name: "Change password" }));

    expect(await screen.findByRole("status", { name: "" })).toHaveTextContent(/signed out everywhere else/);
    expect(screen.getByLabelText("Current password")).toHaveValue("");
  });

  it("shows social sign-in as unavailable, with nothing to press", async () => {
    api();
    renderScreen(<AccountPage />);

    expect(await screen.findByText("Not available")).toBeInTheDocument();
    const section = screen.getByRole("region", { name: "Linked sign-ins" });
    expect(section.querySelector("button, a")).toBeNull();
  });
});
