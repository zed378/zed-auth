import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { RolesPage } from "./RolesPage";
import { queryKeys } from "../lib/api/client";
import { AuthProvider } from "../lib/auth/AuthProvider";
import { AuthError } from "../lib/auth/oidc";
import { clearToken } from "../lib/auth/tokens";
import { signIn } from "../test/harness";

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return { ...actual, renewSilently: () => Promise.reject(new AuthError("login_required", null)) };
});

const project = { id: "p1", name: "Till" };
const role = {
  id: "r1", project_id: "p1", key: "cashier", display_name: "Cashier",
  permission_keys: ["sale:read"], is_builtin: false, grant_count: 0,
};
const rolesKey = [...queryKeys.roles, "org-1", "p1"];
const projectsKey = [...queryKeys.projects, "org-1"];

function mount(cached = false) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(projectsKey, [project], { updatedAt: Date.now() - 60_000 });
  if (cached) client.setQueryData(rolesKey, [role], { updatedAt: Date.now() - 60_000 });
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/projects/p1/roles"]}>
    <AuthProvider><Routes><Route path="/projects/:projectId/roles" element={<RolesPage />} /></Routes></AuthProvider>
  </MemoryRouter></QueryClientProvider>);
  return client;
}

function stubRoles(handler: (request: Request) => Response | Promise<Response>) {
  const requests: string[] = [];
  vi.stubGlobal("fetch", async (request: Request) => {
    requests.push(request.url);
    return request.url.includes("/roles") ? handler(request) : Response.json({ projects: [project] });
  });
  return requests;
}

beforeEach(() => {
  clearToken();
  sessionStorage.clear();
  vi.stubEnv("VITE_AUTH_CLIENT_ID", "console-test-client");
  vi.stubEnv("VITE_AUTH_ISSUER", "https://auth.example.test");
  signIn();
});

describe("Roles shared collection states", () => {
  it("reseeds a reopened editor when the role changed on the server", async () => {
    let current = role;
    const bodies: unknown[] = [];
    stubRoles(async (request) => {
      if (request.method !== "PATCH") return Response.json({ roles: [current] });
      bodies.push(await request.json());
      return Response.json(current);
    });
    const client = mount();
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Edit" }));
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    // Another administrator revokes the permission while the editor is closed.
    current = { ...role, permission_keys: [] };
    await act(() => client.invalidateQueries({ queryKey: rolesKey }));
    await waitFor(() => expect(screen.getByText("None — a label only")).toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: "Edit" }));
    await user.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(bodies).toEqual([{ display_name: "Cashier", permission_keys: [] }]));
  });

  it("announces pending rows before showing a genuine empty list", async () => {
    let finish!: (response: Response) => void;
    stubRoles(() => new Promise<Response>((resolve) => { finish = resolve; }));
    mount();
    expect(await screen.findByText("Loading roles")).toBeInTheDocument();
    expect(screen.getByRole("table", { name: "Roles in this project" })).toBeInTheDocument();
    expect(screen.queryByText("No roles yet")).not.toBeInTheDocument();
    await act(async () => { finish(Response.json({ roles: [] })); });
    expect(await screen.findByText("No roles yet")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Define the first role" })).toBeInTheDocument();
  });

  it("recovers filtered empty results locally even when the collection itself is empty", async () => {
    const requests = stubRoles(() => Response.json({ roles: [] }));
    mount();
    await screen.findByText("No roles yet");
    await userEvent.type(screen.getByRole("searchbox"), "missing");
    expect(screen.getByText("No roles match that search")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Define the first role" })).not.toBeInTheDocument();
    const before = requests.length;
    await userEvent.click(screen.getByRole("button", { name: "Clear the search" }));
    expect(screen.getByText("No roles yet")).toBeInTheDocument();
    expect(screen.getByRole("searchbox")).toHaveValue("");
    expect(requests).toHaveLength(before);
  });

  it("keeps stale rows mounted and retries only roles after a network failure", async () => {
    let fail = true;
    const requests = stubRoles(() => {
      if (fail) throw new TypeError("Failed to fetch");
      return Response.json({ roles: [role] });
    });
    mount(true);
    const table = screen.getByRole("table", { name: "Roles in Till" });
    expect(await screen.findByRole("alert")).toHaveTextContent(/check your connection/i);
    expect(screen.getByRole("alert")).toHaveTextContent(/last-known data/i);
    expect(screen.getByText("Cashier")).toBeInTheDocument();
    const before = requests.length;
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
    expect(requests.slice(before)).toHaveLength(1);
    expect(new URL(requests[before]).pathname).toBe("/v1/organizations/org-1/projects/p1/roles");
    expect(screen.getByRole("table")).toBe(table);
  });

  it("retains an open edit and its draft after a server refresh failure", async () => {
    let fail = false;
    stubRoles(() => fail
      ? Response.json({ error: { code: "SERVER_ERROR" } }, { status: 500 })
      : Response.json({ roles: [role] }));
    const client = mount();
    await userEvent.click(await screen.findByRole("button", { name: "Edit" }));
    await userEvent.type(screen.getByLabelText("Name"), " unsaved");
    fail = true;
    await act(async () => { await client.invalidateQueries({ queryKey: rolesKey }); });
    expect(await screen.findByRole("alert")).toHaveTextContent(/last-known data/i);
    expect(screen.getByRole("dialog", { name: "Edit Cashier" })).toBeInTheDocument();
    expect(screen.getByLabelText("Name")).toHaveValue("Cashier unsaved");
  });

  it.each(["create", "edit", "delete"])("suppresses cached content and an open %s dialog after permission denial", async (action) => {
    let deny = false;
    stubRoles(() => deny
      ? Response.json({ error: { code: "PERMISSION_DENIED", message: "Private cashier permissions" } }, { status: 403 })
      : Response.json({ roles: [role] }));
    const client = mount();
    await screen.findByText("Cashier");
    await userEvent.click(screen.getByRole("button", {
      name: action === "create" ? "Define role" : action === "edit" ? "Edit" : "Delete",
    }));
    if (action !== "delete") await userEvent.type(screen.getByLabelText("Name"), " private draft");
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    deny = true;
    await act(async () => { await client.invalidateQueries({ queryKey: rolesKey }); });
    expect(await screen.findByRole("alert")).toHaveTextContent(/review your access/i);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.queryByRole("searchbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Define role" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
    expect(screen.queryByText(/Cashier|Till|Private cashier permissions/)).not.toBeInTheDocument();
    expect(screen.queryByDisplayValue(/private draft/)).not.toBeInTheDocument();
  });

  it("treats an HTTP 403 with an unclassified JSON envelope as permission denial", async () => {
    stubRoles(() => Response.json({ message: "Private cashier permissions" }, { status: 403 }));
    mount(true);
    expect(await screen.findByRole("alert")).toHaveTextContent(/review your access/i);
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.queryByText(/Cashier|Till|Private cashier permissions/)).not.toBeInTheDocument();
  });

  it("hides role data when the project context request refuses access", async () => {
    vi.stubGlobal("fetch", async (request: Request) => request.url.includes("/roles")
      ? Response.json({ roles: [role] })
      : Response.json({ error: { code: "PERMISSION_DENIED" } }, { status: 403 }));
    mount(true);
    expect(await screen.findByRole("alert")).toHaveTextContent(/review your access/i);
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.queryByText(/Cashier|Till/)).not.toBeInTheDocument();
  });
});
