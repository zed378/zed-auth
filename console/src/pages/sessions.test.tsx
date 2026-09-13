import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { SessionsTab } from "./SessionsTab";
import { AuthError } from "../lib/auth/oidc";
import { clearToken } from "../lib/auth/tokens";
import { expectNoAxeViolations } from "../test/axe";
import { renderScreen, signIn, stubApi } from "../test/harness";

/**
 * The Sessions tab (P3-11), against `docs/UI-UX/19`'s worked example.
 *
 * The rollback test is the one the card's Definition of Done names: a row that
 * disappears optimistically must come back — with its error beside it — when
 * the API refuses, or the screen tells somebody worried about their account
 * that a session is gone when it is not.
 */

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return {
    ...actual,
    renewSilently: () => Promise.reject(new AuthError("login_required", null)),
  };
});

const SELF = "admin-1";

function session(id: string, browser: string, os: string, current = false) {
  return {
    id,
    device: { browser, os },
    location: "Jakarta, ID",
    auth_methods: ["pwd"],
    created_at: "2026-09-13T08:00:00Z",
    last_active_at: "2026-09-13T09:00:00Z",
    expires_at: "2026-09-13T20:00:00Z",
    current,
  };
}

const laptop = session("11111111-1111-1111-1111-111111111111", "Chrome", "Windows", true);
const phone = session("22222222-2222-2222-2222-222222222222", "Safari", "iOS");

beforeEach(() => {
  clearToken();
  sessionStorage.clear();
  vi.stubEnv("VITE_AUTH_CLIENT_ID", "console-test-client");
  vi.stubEnv("VITE_AUTH_ISSUER", "https://auth.example.test");
  signIn();
});

describe("one's own sessions", () => {
  it("names each revoke button by its device and marks this session", async () => {
    stubApi(() => ({ status: 200, body: { sessions: [laptop, phone] } }));
    const { container } = renderScreen(<SessionsTab orgId="org-1" userId={SELF} />);

    expect(await screen.findByRole("button", { name: "Revoke session on Safari on iOS" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Revoke session on Chrome on Windows" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^revoke$/i })).not.toBeInTheDocument();
    expect(screen.getByText("This session")).toBeInTheDocument();
    await expectNoAxeViolations(container);
  });

  it("removes the row at once and puts it back, with the error, when the API refuses", async () => {
    let release: () => void = () => undefined;
    const answered = new Promise<void>((resolve) => {
      release = resolve;
    });
    let deletes = 0;

    vi.stubGlobal("fetch", async (input: RequestInfo | URL) => {
      const request = input instanceof Request ? input : new Request(String(input));
      if (request.method === "DELETE") {
        deletes++;
        await answered;
        return new Response(JSON.stringify({ error: { code: "INTERNAL", message: "The server did not answer." } }), {
          status: 500,
          headers: { "Content-Type": "application/json" },
        });
      }
      return new Response(JSON.stringify({ sessions: [laptop, phone] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });

    renderScreen(<SessionsTab orgId="org-1" userId={SELF} />);
    await userEvent.click(await screen.findByRole("button", { name: "Revoke session on Safari on iOS" }));

    // Optimistic: gone before the API has answered, and without a modal.
    await waitFor(() => expect(screen.queryByText("Safari on iOS")).not.toBeInTheDocument());
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(deletes).toBe(1);

    // The API refuses: the row returns, with the reason in it.
    release();
    const row = (await screen.findByText("Safari on iOS")).closest("tr") as HTMLElement;
    expect(await within(row).findByRole("alert")).toHaveTextContent("The server did not answer.");
    expect(within(row).getByRole("button", { name: "Revoke session on Safari on iOS" })).toBeEnabled();
  });

  it("asks before revoking the session the console is using", async () => {
    let deletes = 0;
    stubApi((_url, init) => {
      if (init.method === "DELETE") deletes++;
      return { status: 200, body: { sessions: [laptop, phone] } };
    });
    renderScreen(<SessionsTab orgId="org-1" userId={SELF} />);

    await userEvent.click(await screen.findByRole("button", { name: "Revoke session on Chrome on Windows" }));
    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveTextContent(/signed out of the console/i);
    expect(deletes).toBe(0);

    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(deletes).toBe(0);
  });

  it("says 'no other active sessions' when only this one exists, and disables revoke-all", async () => {
    stubApi(() => ({ status: 200, body: { sessions: [laptop] } }));
    renderScreen(<SessionsTab orgId="org-1" userId={SELF} />);

    expect(await screen.findByText("No other active sessions.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Revoke all other sessions" })).toBeDisabled();
  });

  it("revokes all others in one action and says how many", async () => {
    stubApi((url, init) => {
      if (init.method === "POST" && url.endsWith("/revoke-others")) return { status: 200, body: { revoked: 1 } };
      return { status: 200, body: { sessions: [laptop, phone] } };
    });
    renderScreen(<SessionsTab orgId="org-1" userId={SELF} />);

    await userEvent.click(await screen.findByRole("button", { name: "Revoke all other sessions" }));
    expect(await screen.findByRole("status")).toHaveTextContent("1 other session was signed out.");
  });
});

describe("a member's sessions", () => {
  it("lists them with per-row revoke and no revoke-all", async () => {
    stubApi(() => ({ status: 200, body: { sessions: [{ ...phone, current: false }] } }));
    renderScreen(<SessionsTab orgId="org-1" userId="member-2" />);

    expect(await screen.findByRole("button", { name: "Revoke session on Safari on iOS" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Revoke all other sessions" })).not.toBeInTheDocument();
    expect(screen.queryByText("This session")).not.toBeInTheDocument();
  });
});
