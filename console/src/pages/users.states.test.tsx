import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { UsersPage } from "./UsersPage";
import { queryKeys } from "../lib/api/client";
import { AuthProvider } from "../lib/auth/AuthProvider";
import { AuthError } from "../lib/auth/oidc";
import { clearToken } from "../lib/auth/tokens";
import { signIn } from "../test/harness";

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return { ...actual, renewSilently: () => Promise.reject(new AuthError("login_required", null)) };
});

const member = { id: "u1", email: "private@example.test", display_name: "Private member", status: "active", email_verified: true };
const key = [...queryKeys.users, "org-1", ""];
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), {
  status, headers: { "Content-Type": "application/json" },
});

function mount(cached = false, route = "/users") {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  if (cached) client.setQueryData(key, { items: [member], complete: false }, { updatedAt: Date.now() - 60_000 });
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[route]}>
    <AuthProvider><UsersPage /></AuthProvider>
  </MemoryRouter></QueryClientProvider>);
  return client;
}

beforeEach(() => {
  clearToken();
  sessionStorage.clear();
  vi.stubEnv("VITE_AUTH_CLIENT_ID", "console-test-client");
  vi.stubEnv("VITE_AUTH_ISSUER", "https://auth.example.test");
  signIn();
});

describe("Users shared state adoption", () => {
  it("announces pending users without flashing initial empty or fetching closed-panel projects", async () => {
    let finish!: (response: Response) => void;
    const fetch = vi.fn(() => new Promise<Response>((resolve) => { finish = resolve; }));
    vi.stubGlobal("fetch", fetch);
    mount();
    expect(await screen.findByText("Loading users")).toBeInTheDocument();
    expect(screen.getByRole("table", { name: "Users in this organization" })).toBeInTheDocument();
    expect(screen.queryByText("No users yet")).not.toBeInTheDocument();
    await act(async () => { finish(json({ users: [] })); });
    expect(await screen.findByText("No users yet")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Invite the first user" })).toBeInTheDocument();
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it("treats an empty server search as filtered and clears search plus status", async () => {
    const requests: URL[] = [];
    vi.stubGlobal("fetch", async (request: Request) => {
      const url = new URL(request.url);
      requests.push(url);
      return json({ users: url.searchParams.has("search") ? [] : [member] });
    });
    mount(false, "/users?status=invited");
    expect(await screen.findByText("No users match that search")).toBeInTheDocument();
    await userEvent.type(screen.getByRole("searchbox"), "missing");
    await waitFor(() => expect(requests.some((url) => url.searchParams.get("search") === "missing")).toBe(true));
    expect(await screen.findByText("No users match that search")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Invite the first user" })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Clear the search" }));
    expect(await screen.findByText("Private member")).toBeInTheDocument();
    expect(screen.getByRole("searchbox")).toHaveValue("");
    expect(screen.getByRole("combobox", { name: "Filter by status" })).toHaveValue("");
  });

  it("keeps stale rows mounted and retries only the failed users query", async () => {
    const requests: string[] = [];
    let fail = true;
    vi.stubGlobal("fetch", async (request: Request) => {
      requests.push(request.url);
      return fail ? json({ error: { code: "SERVER_ERROR" } }, 500) : json({ users: [member] });
    });
    mount(true);
    const table = screen.getByRole("table");
    expect(await screen.findByRole("alert")).toHaveTextContent(/last-known data/i);
    expect(screen.getByText("Private member")).toBeInTheDocument();
    const beforeRetry = requests.length;
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
    expect(screen.getByRole("table")).toBe(table);
    expect(requests.length).toBe(beforeRetry + 1);
    expect(requests.every((url) => new URL(url).pathname === "/v1/organizations/org-1/users")).toBe(true);
  });

  it("offers connection recovery for a rejected fetch", async () => {
    vi.stubGlobal("fetch", async () => { throw new TypeError("Failed to fetch"); });
    mount();
    expect(await screen.findByRole("alert")).toHaveTextContent("Check your connection and try again.");
    expect(screen.queryByText("No users yet")).not.toBeInTheDocument();
  });

  it("withholds cached users, count hints and actions after denied access", async () => {
    vi.stubGlobal("fetch", async () => json({ error: {
      code: "PERMISSION_DENIED", message: "Private member is confidential",
    } }, 403));
    mount(true);
    expect(await screen.findByRole("alert")).toHaveTextContent(/review your access/i);
    expect(screen.queryByText(/Private member|private@example|Showing the first/)).not.toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.queryByRole("searchbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Invite user" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });

  it("recognizes a forbidden response even when its error code is unclassified", async () => {
    vi.stubGlobal("fetch", async () => json({ error: {
      code: "UNCLASSIFIED", message: "Private member is confidential",
    } }, 403));
    mount(true);
    expect(await screen.findByRole("alert")).toHaveTextContent(/review your access/i);
    expect(screen.queryByText(/Private member|private@example|Showing the first/)).not.toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });

  it("preserves an invite draft on list failure and hides it when access is refused", async () => {
    let status = 200;
    vi.stubGlobal("fetch", async (request: Request) => {
      if (new URL(request.url).pathname.endsWith("/projects")) return json({ projects: [] });
      return status === 200 ? json({ users: [member] })
        : json({ error: { code: status === 403 ? "PERMISSION_DENIED" : "SERVER_ERROR" } }, status);
    });
    const client = mount();
    await screen.findByText("Private member");
    await userEvent.click(screen.getByRole("button", { name: "Invite user" }));
    await userEvent.type(screen.getByLabelText("Email address"), "draft@example.test");
    status = 500;
    await act(async () => { await client.invalidateQueries({ queryKey: key }); });
    expect(await screen.findByRole("alert")).toHaveTextContent(/last-known data/i);
    expect(screen.getByLabelText("Email address")).toHaveValue("draft@example.test");
    status = 403;
    await act(async () => { await client.invalidateQueries({ queryKey: key }); });
    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent(/review your access/i));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByDisplayValue("draft@example.test")).not.toBeInTheDocument();
    expect(screen.queryByText("Private member")).not.toBeInTheDocument();
  });
});
