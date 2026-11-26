import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { MfaTab } from "./MfaTab";
import { SessionsTab } from "./SessionsTab";
import { queryKeys } from "../lib/api/client";
import { AuthProvider } from "../lib/auth/AuthProvider";
import { AuthError } from "../lib/auth/oidc";
import { clearToken } from "../lib/auth/tokens";
import { signIn } from "../test/harness";

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return { ...actual, renewSilently: () => Promise.reject(new AuthError("login_required", null)) };
});

const session = {
  id: "s1", device: { browser: "Chrome", os: "Windows" }, location: "Jakarta, ID",
  auth_methods: ["pwd"], created_at: "2026-09-10T00:00:00Z", last_active_at: "2026-09-10T00:00:00Z",
  expires_at: "2026-12-10T00:00:00Z", current: true,
};
const factor = { id: "f1", type: "totp", label: "Private phone", created_at: "2026-09-10T00:00:00Z", last_used_at: null };
const mfa = { factors: [factor], recovery_codes_remaining: 7, mfa_required: false, available_types: ["totp"] };
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), {
  status, headers: { "Content-Type": "application/json" },
});
const denied = () => json({ error: { code: "unknown_gateway_code", message: "Private server detail" } }, 403);

function mount(tab: "sessions" | "mfa", self = false, cached = false) {
  const actor = self ? "me" : "member-1";
  const key = [...(tab === "sessions" ? queryKeys.sessions : queryKeys.mfa), "org-1", actor];
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  if (cached) client.setQueryData(key, tab === "sessions" ? [session] : mfa, { updatedAt: Date.now() - 60_000 });
  const tree = (orgId: string, userId: string) => <QueryClientProvider client={client}><MemoryRouter><AuthProvider>
    {tab === "sessions" ? <SessionsTab orgId={orgId} userId={userId} /> : <MfaTab orgId={orgId} userId={userId} />}
  </AuthProvider></MemoryRouter></QueryClientProvider>;
  const view = render(tree("org-1", self ? "admin-1" : "member-1"));
  return { client, key, switchScope: (orgId: string, userId: string) => view.rerender(tree(orgId, userId)) };
}

beforeEach(() => {
  clearToken();
  sessionStorage.clear();
  vi.stubEnv("VITE_AUTH_CLIENT_ID", "console-test-client");
  vi.stubEnv("VITE_AUTH_ISSUER", "https://auth.example.test");
  signIn();
});

describe("User security collection states", () => {
  it.each(["sessions", "mfa"] as const)("waits for the current identity before requesting its own %s", async (tab) => {
    const requests: string[] = [];
    vi.stubGlobal("fetch", async (request: Request) => {
      requests.push(request.url);
      return json(tab === "sessions" ? { sessions: [session] } : mfa);
    });
    mount(tab, true);
    expect(await screen.findByText(tab === "sessions" ? "Chrome on Windows" : "Private phone")).toBeInTheDocument();
    expect(requests.length).toBeGreaterThan(0);
    expect(requests.every((url) => url.includes(`/v1/me/${tab}`))).toBe(true);
  });

  it("announces pending sessions before showing the empty result", async () => {
    let finish!: (response: Response) => void;
    vi.stubGlobal("fetch", () => new Promise<Response>((resolve) => { finish = resolve; }));
    mount("sessions");
    expect(await screen.findByText("Loading active sessions")).toBeInTheDocument();
    expect(screen.queryByText("No active sessions yet")).not.toBeInTheDocument();
    await act(async () => { finish(json({ sessions: [] })); });
    expect(await screen.findByText("No active sessions yet")).toBeInTheDocument();
  });

  it("retains the sessions table on failure and retries only its own request", async () => {
    const requests: string[] = [];
    let fail = true;
    vi.stubGlobal("fetch", async (request: Request) => {
      requests.push(request.url);
      return fail ? json({ error: { code: "internal_error" } }, 500) : json({ sessions: [session] });
    });
    mount("sessions", false, true);
    const table = screen.getByRole("table");
    expect(await screen.findByRole("alert")).toHaveTextContent(/last-known data/i);
    expect(screen.getByText("Chrome on Windows")).toBeInTheDocument();
    fail = false;
    const before = requests.length;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
    expect(screen.getByRole("table")).toBe(table);
    expect(requests).toHaveLength(before + 1);
    expect(requests.every((url) => url.includes("/users/member-1/sessions"))).toBe(true);
  });

  it("suppresses a cached current session and open sign-out dialog after HTTP 403", async () => {
    let deny = false;
    vi.stubGlobal("fetch", async () => deny ? denied() : json({ sessions: [session] }));
    const { client, key } = mount("sessions", true);
    await userEvent.click(await screen.findByRole("button", { name: "Revoke session on Chrome on Windows" }));
    expect(await screen.findByRole("dialog")).toBeInTheDocument();
    deny = true;
    await act(async () => { await client.invalidateQueries({ queryKey: key }); });
    expect(await screen.findByRole("alert")).toHaveTextContent(/review your access/i);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByText("Chrome on Windows")).not.toBeInTheDocument();
    expect(screen.queryByText("Private server detail")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });

  it("announces pending member MFA without flashing its empty state", async () => {
    let finish!: (response: Response) => void;
    vi.stubGlobal("fetch", () => new Promise<Response>((resolve) => { finish = resolve; }));
    mount("mfa");
    expect(await screen.findByText("Loading second factors")).toBeInTheDocument();
    expect(screen.queryByText("This user has no second factor.")).not.toBeInTheDocument();
    await act(async () => { finish(json({ factors: [], recovery_codes_remaining: 0 })); });
    expect(await screen.findByText("This user has no second factor.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Reset multi-factor" })).not.toBeInTheDocument();
  });

  it.each([false, true])("retains MFA metadata on stale failure and retries its read (self=%s)", async (self) => {
    let fail = true;
    const requests: string[] = [];
    vi.stubGlobal("fetch", async (request: Request) => {
      requests.push(request.url);
      return fail ? json({ error: { code: "internal_error" } }, 500) : json(mfa);
    });
    mount("mfa", self, true);
    const table = screen.getByRole("table");
    expect(await screen.findByRole("alert")).toHaveTextContent(/last-known data/i);
    expect(screen.getByText("Private phone")).toBeInTheDocument();
    fail = false;
    const before = requests.length;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
    expect(screen.getByRole("table")).toBe(table);
    expect(requests).toHaveLength(before + 1);
    expect(requests.every((url) => url.endsWith(self ? "/me/mfa" : "/users/member-1/mfa"))).toBe(true);
  });

  it.each([false, true])("suppresses MFA metadata and open confirmation after HTTP 403 (self=%s)", async (self) => {
    let deny = false;
    vi.stubGlobal("fetch", async () => deny ? denied() : json(mfa));
    const { client, key } = mount("mfa", self);
    await userEvent.click(await screen.findByRole("button", { name: self ? /Remove authenticator app/ : "Reset multi-factor" }));
    expect(await screen.findByRole("dialog")).toBeInTheDocument();
    deny = true;
    await act(async () => { await client.invalidateQueries({ queryKey: key }); });
    expect(await screen.findByRole("alert")).toHaveTextContent(/review your access/i);
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByText("Private phone")).not.toBeInTheDocument();
    expect(screen.queryByText(/Unused recovery codes/)).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });

  it.each(["sessions", "mfa"] as const)("clears open %s confirmations when the organization/user scope changes", async (tab) => {
    vi.stubGlobal("fetch", async (request: Request) => {
      const next = request.url.includes("/organizations/org-2/");
      return json(tab === "sessions" ? { sessions: next ? [] : [session] }
        : next ? { factors: [], recovery_codes_remaining: 0 } : mfa);
    });
    const { switchScope } = mount(tab, tab === "sessions");
    await userEvent.click(await screen.findByRole("button", {
      name: tab === "sessions" ? "Revoke session on Chrome on Windows" : "Reset multi-factor",
    }));
    expect(await screen.findByRole("dialog")).toBeInTheDocument();
    switchScope("org-2", "member-2");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(await screen.findByText(tab === "sessions" ? "No active sessions yet" : "This user has no second factor.")).toBeInTheDocument();
    expect(screen.queryByText("Private phone")).not.toBeInTheDocument();
    expect(screen.queryByText("Chrome on Windows")).not.toBeInTheDocument();
  });
});
