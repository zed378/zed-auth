import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { AuditLogPage } from "./AuditLogPage";
import { UserDetailPage } from "./UserDetailPage";
import { UsersPage } from "./UsersPage";
import { AuthError } from "../lib/auth/oidc";
import { clearToken } from "../lib/auth/tokens";
import { expectNoAxeViolations } from "../test/axe";
import { renderScreen, signIn, stubApi } from "../test/harness";

/**
 * The user and audit-log screens (P1-23, P1-24).
 *
 * The API is stubbed at `fetch`, not at the query layer. That keeps the
 * generated client, the bearer middleware and the envelope handling in the
 * path — a test that mocks `useUsers` proves the component renders an array,
 * which is not the thing that breaks.
 */

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return {
    ...actual,
    renewSilently: () => Promise.reject(new AuthError("login_required", null)),
  };
});

beforeEach(() => {
  clearToken();
  sessionStorage.clear();
  vi.stubEnv("VITE_AUTH_CLIENT_ID", "console-test-client");
  vi.stubEnv("VITE_AUTH_ISSUER", "https://auth.example.test");
  signIn();
});

describe("the users list", () => {
  it("shows a status badge with text, not colour alone", async () => {
    stubApi(() => ({
      status: 200,
      body: {
        users: [
          { id: "u1", email: "one@example.test", status: "invited", email_verified: false },
        ],
      },
    }));

    renderScreen(<UsersPage />);

    expect(await screen.findByText("invited")).toBeInTheDocument();
  });

  it("says 'not asserted' rather than 'unverified' for an unproven address", async () => {
    stubApi(() => ({
      status: 200,
      body: {
        users: [{ id: "u1", email: "one@example.test", status: "active", email_verified: false }],
      },
    }));

    renderScreen(<UsersPage />);

    // PG-18: absent means we have not checked, not that we checked and it
    // failed. The console must not make a claim the service deliberately
    // withholds.
    expect(await screen.findByText("Not asserted")).toBeInTheDocument();
  });

  it("distinguishes a filtered empty from a genuinely empty one", async () => {
    stubApi(() => ({ status: 200, body: { users: [] } }));

    const { unmount } = renderScreen(<UsersPage />);
    expect(await screen.findByText(/No users yet/i)).toBeInTheDocument();
    unmount();

    // With a status filter in the URL and users in the organization, the
    // empty result means the filter matched none.
    stubApi(() => ({
      status: 200,
      body: {
        users: [{ id: "u1", email: "one@example.test", status: "active", email_verified: true }],
      },
    }));
    renderScreen(<UsersPage />, "/users?status=deactivated");
    expect(await screen.findByText(/No users match that search/i)).toBeInTheDocument();
  });

  it("renders a refusal without offering a retry", async () => {
    stubApi(() => ({
      status: 403,
      body: { error: { code: "PERMISSION_DENIED", message: "no" } },
    }));

    renderScreen(<UsersPage />);

    expect(await screen.findByRole("alert")).toHaveTextContent(/do not have access/i);
    expect(screen.queryByRole("button", { name: /try again/i })).not.toBeInTheDocument();
  });
});

describe("the invite flow", () => {
  it("is one flow in two steps, with the step announced", async () => {
    stubApi(() => ({ status: 200, body: { users: [], projects: [] } }));

    renderScreen(<UsersPage />);
    await userEvent.click(await screen.findByRole("button", { name: /invite user/i }));

    expect(screen.getByText("Step 1 of 2")).toBeInTheDocument();

    await userEvent.type(screen.getByLabelText(/email address/i), "new@example.test");
    await userEvent.click(screen.getByRole("button", { name: /next/i }));

    expect(screen.getByText("Step 2 of 2")).toBeInTheDocument();
  });

  it("makes 'no access yet' an explicit choice rather than a skip", async () => {
    stubApi(() => ({ status: 200, body: { users: [], projects: [] } }));

    renderScreen(<UsersPage />);
    await userEvent.click(await screen.findByRole("button", { name: /invite user/i }));
    await userEvent.type(screen.getByLabelText(/email address/i), "new@example.test");
    await userEvent.click(screen.getByRole("button", { name: /next/i }));

    // docs/UI-UX/04 Flow 1's safeguard: an admin must never walk away
    // believing they granted access when they did not. So the send button is
    // disabled until the choice is made deliberately.
    const send = screen.getByRole("button", { name: /send invitation/i });
    expect(send).toBeDisabled();

    await userEvent.click(screen.getByRole("checkbox", { name: /no access yet/i }));
    expect(send).toBeEnabled();
  });

  it("renders a field error against its own field", async () => {
    stubApi((url) =>
      url.includes("/users") && !url.includes("page_size")
        ? {
            status: 400,
            body: {
              error: {
                code: "VALIDATION_ERROR",
                message: "That email address is not valid.",
                details: [{ field: "email", issue: "must look like name@example.com" }],
              },
            },
          }
        : { status: 200, body: { users: [], projects: [] } },
    );

    renderScreen(<UsersPage />);
    await userEvent.click(await screen.findByRole("button", { name: /invite user/i }));
    await userEvent.type(screen.getByLabelText(/email address/i), "nonsense");
    await userEvent.click(screen.getByRole("button", { name: /next/i }));
    await userEvent.click(screen.getByRole("checkbox", { name: /no access yet/i }));
    await userEvent.click(screen.getByRole("button", { name: /send invitation/i }));

    // docs/UI-UX/15: against the FIELD, not as a banner the user has to map
    // back onto the form themselves.
    const field = await screen.findByLabelText(/email address/i);
    expect(field).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByText("must look like name@example.com")).toBeInTheDocument();
  });
});

describe("user detail", () => {
  const user = {
    id: "u1",
    email: "one@example.test",
    display_name: "One Person",
    status: "active",
    email_verified: true,
    created_at: "2026-09-01T00:00:00Z",
  };

  it("marks the not-yet-available tabs as unavailable rather than empty", async () => {
    stubApi(() => ({ status: 200, body: user }));

    renderScreen(<UserDetailPage />, "/users/u1", "/users/:userId");

    await userEvent.click(await screen.findByRole("tab", { name: /grants/i }));

    // P1-23 step 3: an empty tab and an unavailable one look similar and mean
    // opposite things. This one says which.
    expect(screen.getByText(/not available yet/i)).toBeInTheDocument();
    expect(screen.getByText(/does not exist yet, not because this user has none/i)).toBeInTheDocument();
  });

  it("states the consequence of deactivation, not just that it is irreversible", async () => {
    stubApi(() => ({ status: 200, body: user }));

    renderScreen(<UserDetailPage />, "/users/u1", "/users/:userId");
    await userEvent.click(await screen.findByRole("button", { name: /deactivate this user/i }));

    const dialog = screen.getByRole("dialog");
    // docs/UI-UX/07: a consequence summary in plain language, not "are you
    // sure?" — and specifically the part people do not expect.
    expect(dialog).toHaveTextContent(/every session they have open ends immediately/i);
    expect(dialog).toHaveTextContent(/refresh token/i);
    // The verb, not "OK" or "Confirm".
    expect(screen.getByRole("button", { name: "Deactivate" })).toBeInTheDocument();
  });

  it("has no axe violations", async () => {
    stubApi(() => ({ status: 200, body: user }));
    const { container } = renderScreen(<UserDetailPage />, "/users/u1", "/users/:userId");
    await screen.findByRole("tab", { name: /profile/i });
    await expectNoAxeViolations(container);
  });
});

describe("the audit log", () => {
  const events = {
    events: [
      {
        id: "1:2026-09-10T12:00:00Z",
        event_type: "user.login.failed",
        actor_user_id: null,
        occurred_at: "2026-09-10T12:00:00Z",
        ip: "10.0.0.1",
        payload: { email: "someone@example.test" },
      },
    ],
  };

  it("says an event has no actor rather than leaving it blank", async () => {
    stubApi(() => ({ status: 200, body: events }));

    renderScreen(<AuditLogPage />);

    // A failed login against an address that does not exist genuinely has no
    // actor. Blank would read as an incomplete record.
    expect(await screen.findByText(/no authenticated actor/i)).toBeInTheDocument();
  });

  it("keeps the UTC value inspectable while showing local time", async () => {
    stubApi(() => ({ status: 200, body: events }));

    renderScreen(<AuditLogPage />);

    const when = await screen.findByTitle("2026-09-10T12:00:00Z");
    // P1-24 step 5: incident timelines are reconstructed across timezones.
    expect(when).toHaveAttribute("dateTime", "2026-09-10T12:00:00Z");
  });

  it("shows the payload as it arrived, with a note about redaction", async () => {
    stubApi(() => ({ status: 200, body: events }));

    renderScreen(<AuditLogPage />);
    await userEvent.click(await screen.findByRole("button", { name: /^detail$/i }));

    expect(screen.getByText(/Redacted before storage/i)).toBeInTheDocument();
  });

  it("distinguishes no events at all from none matching the filters", async () => {
    stubApi(() => ({ status: 200, body: { events: [] } }));

    renderScreen(<AuditLogPage />);
    expect(await screen.findByText(/No events yet/i)).toBeInTheDocument();

    await userEvent.selectOptions(screen.getByLabelText(/event type/i), "user.login.success");
    expect(await screen.findByText(/No events match that search/i)).toBeInTheDocument();
  });
});
