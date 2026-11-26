import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { GrantedProjectsPage } from "./GrantedProjectsPage";
import { queryKeys } from "../lib/api/client";
import { AuthProvider } from "../lib/auth/AuthProvider";
import { AuthError } from "../lib/auth/oidc";
import { clearToken } from "../lib/auth/tokens";
import { signIn } from "../test/harness";

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return { ...actual, renewSilently: () => Promise.reject(new AuthError("login_required", null)) };
});

const grant = {
  id: "g1", project_id: "p1", project_name: "Till", granting_org_id: "vendor",
  granting_org_name: "Acme Vendor", granted_role_keys: ["cashier"], holder_count: 0,
  status: "active", created_at: "2026-09-10T00:00:00Z", revoked_at: null,
};
const key = [...queryKeys.receivedGrants, "org-1"];
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), {
  status, headers: { "Content-Type": "application/json" },
});

function mount(cached = false) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  if (cached) client.setQueryData(key, [grant], { updatedAt: Date.now() - 60_000 });
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/granted-projects"]}>
    <AuthProvider><GrantedProjectsPage /></AuthProvider>
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

describe("Granted Projects shared state adoption", () => {
  it("keeps the table skeleton visible without flashing empty until the request completes", async () => {
    let finish!: (response: Response) => void;
    vi.stubGlobal("fetch", () => new Promise<Response>((resolve) => { finish = resolve; }));
    mount();
    expect(await screen.findByText("Loading granted projects")).toBeInTheDocument();
    expect(screen.queryByText("No granted projects yet")).not.toBeInTheDocument();
    await act(async () => { finish(json({ grants: [] })); });
    expect(await screen.findByText("No granted projects yet")).toBeInTheDocument();
    expect(screen.getByText(/Only another organization can grant/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /create/i })).not.toBeInTheDocument();
  });

  it("retains last-known rows on refresh failure and retries only the grants request", async () => {
    const requests: string[] = [];
    let fail = true;
    vi.stubGlobal("fetch", async (request: Request) => {
      requests.push(request.url);
      return fail ? json({ error: { code: "internal_error" } }, 500) : json({ grants: [grant] });
    });
    mount(true);
    const table = screen.getByRole("table", { name: "Projects granted to this organization" });
    expect(await screen.findByRole("alert")).toHaveTextContent(/last-known data/i);
    expect(screen.getByText("Till")).toBeInTheDocument();
    expect(screen.getByRole("table")).toBe(table);
    const beforeRetry = requests.length;
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
    expect(requests.length).toBe(beforeRetry + 1);
    expect(requests.every((url) => url.includes("/organizations/org-1/project-grants"))).toBe(true);
    expect(screen.getByRole("table")).toBe(table);
  });

  it("removes cached grants and recovery actions when the API refuses access", async () => {
    vi.stubGlobal("fetch", async () => json({ error: { code: "PERMISSION_DENIED" } }, 403));
    mount(true);
    expect(await screen.findByRole("alert")).toHaveTextContent(/review your access/i);
    expect(screen.queryByText("Till")).not.toBeInTheDocument();
    expect(screen.queryByText("Acme Vendor")).not.toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });

  it("closes an open management panel when access to the received grants is denied", async () => {
    let deny = false;
    vi.stubGlobal("fetch", async (request: Request) => {
      if (request.url.includes("/user-grants")) return json({ grants: [] });
      if (request.url.includes("/users")) return json({ users: [] });
      return deny ? json({ error: { code: "PERMISSION_DENIED" } }, 403) : json({ grants: [grant] });
    });
    const client = mount();
    await userEvent.click(await screen.findByRole("button", { name: "Assign roles" }));
    expect(await screen.findByRole("dialog", { name: "Till from Acme Vendor" })).toBeInTheDocument();
    deny = true;
    await act(async () => { await client.invalidateQueries({ queryKey: key }); });
    expect(await screen.findByRole("alert")).toHaveTextContent(/review your access/i);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByText("Till")).not.toBeInTheDocument();
    expect(screen.queryByText(/These roles belong to/i)).not.toBeInTheDocument();
  });
});
