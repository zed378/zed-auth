import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ProjectGrantsPage } from "./ProjectGrantsPage";
import { queryKeys } from "../lib/api/client";
import { AuthProvider } from "../lib/auth/AuthProvider";
import { AuthError } from "../lib/auth/oidc";
import { clearToken } from "../lib/auth/tokens";
import { signIn } from "../test/harness";

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return { ...actual, renewSilently: () => Promise.reject(new AuthError("login_required", null)) };
});

const partner = "3f2b8c1e-5d4a-4f7b-9c2e-1a6d8e0b7c45";
const grant = {
  id: "g1", project_id: "p1", granting_org_id: "org-1", granted_org_id: partner,
  granted_org_name: "Private partner", granted_role_keys: ["cashier"], holder_count: 3,
  status: "active", created_at: "2026-09-10T00:00:00Z", revoked_at: null,
};
const key = [...queryKeys.projectGrants, "org-1", "p1"];
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), {
  status, headers: { "Content-Type": "application/json" },
});

function stub(grants: () => Response | Promise<Response>) {
  const requests: string[] = [];
  vi.stubGlobal("fetch", async (request: Request) => {
    requests.push(request.url);
    if (request.url.includes("/grants")) return grants();
    if (request.url.includes("/roles")) return json({ roles: [{ key: "cashier", display_name: "Cashier" }] });
    return json({ projects: [{ id: "p1", name: "Private project" }] });
  });
  return requests;
}

function mount(cached = false) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  if (cached) client.setQueryData(key, [grant], { updatedAt: Date.now() - 60_000 });
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/projects/p1/grants"]}>
    <AuthProvider><Routes><Route path="/projects/:projectId/grants" element={<ProjectGrantsPage />} /></Routes></AuthProvider>
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

describe("Project Grants shared state adoption", () => {
  it("announces pending data with a table skeleton before offering the first grant", async () => {
    let finish!: (response: Response) => void;
    stub(() => new Promise<Response>((resolve) => { finish = resolve; }));
    mount();
    expect(await screen.findByText("Loading Project Grants")).toBeInTheDocument();
    expect(screen.getByRole("table", { name: "Project Grants" })).toBeInTheDocument();
    expect(screen.queryByText("No Project Grants yet")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create the first grant" })).not.toBeInTheDocument();
    await act(async () => { finish(json({ grants: [] })); });
    expect(await screen.findByText("No Project Grants yet")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create the first grant" })).toBeInTheDocument();
  });

  it("retains stale rows and retries just the grants request without replacing the table", async () => {
    let fail = true;
    const requests = stub(() => fail ? json({ error: { code: "SERVER_ERROR" } }, 500) : json({ grants: [grant] }));
    mount(true);
    const table = screen.getByRole("table");
    expect(await screen.findByRole("alert")).toHaveTextContent(/last-known data/i);
    expect(screen.getByText("Private partner")).toBeInTheDocument();
    expect(screen.getByRole("table")).toBe(table);
    await screen.findByText("Private project");
    const before = requests.length;
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
    expect(requests.slice(before)).toHaveLength(1);
    expect(new URL(requests[before]).pathname).toBe("/v1/organizations/org-1/projects/p1/grants");
    expect(screen.getByRole("table")).toBe(table);
  });

  it("preserves a creation draft when the list refresh loses its connection", async () => {
    let fail = false;
    stub(() => {
      if (fail) throw new TypeError("Failed to fetch");
      return json({ grants: [grant] });
    });
    const client = mount();
    await screen.findByText("Private partner");
    await userEvent.click(screen.getByRole("button", { name: "Create grant" }));
    await userEvent.type(screen.getByLabelText("Organization ID"), partner);
    await userEvent.click(screen.getByLabelText(/cashier/));
    fail = true;
    await act(async () => { await client.invalidateQueries({ queryKey: key }); });
    expect(await screen.findByRole("alert")).toHaveTextContent(/connection/i);
    expect(screen.getByRole("dialog", { name: "Create a Project Grant" })).toBeInTheDocument();
    expect(screen.getByLabelText("Organization ID")).toHaveValue(partner);
    expect(screen.getByLabelText(/cashier/)).toBeChecked();
  });

  it("suppresses cached partners, project context, resource actions and server detail after denial", async () => {
    stub(() => json({ error: { code: "UNKNOWN_DENIAL", message: "Confidential partner access" } }, 403));
    mount(true);
    expect(await screen.findByRole("alert")).toHaveTextContent(/review your access/i);
    expect(screen.queryByText(/Private partner|Private project|Confidential partner access/)).not.toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create grant" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });

  it.each(["creation", "revocation"])("hides an open %s and its cached contents when access is refused", async (action) => {
    let deny = false;
    stub(() => deny ? json({ error: { code: "PERMISSION_DENIED" } }, 403) : json({ grants: [grant] }));
    const client = mount();
    await screen.findByText("Private partner");
    if (action === "creation") {
      await userEvent.click(screen.getByRole("button", { name: "Create grant" }));
      await userEvent.type(screen.getByLabelText("Organization ID"), partner);
    } else {
      await userEvent.click(screen.getByRole("button", { name: "Revoke" }));
    }
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    deny = true;
    await act(async () => { await client.invalidateQueries({ queryKey: key }); });
    expect(await screen.findByRole("alert")).toHaveTextContent(/review your access/i);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByDisplayValue(partner)).not.toBeInTheDocument();
    expect(screen.queryByText(/Private partner|Private project/)).not.toBeInTheDocument();
  });
});
