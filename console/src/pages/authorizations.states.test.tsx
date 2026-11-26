import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Link, MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { AuthorizationsPage } from "./AuthorizationsPage";
import { queryKeys } from "../lib/api/client";
import { AuthProvider } from "../lib/auth/AuthProvider";
import { AuthError } from "../lib/auth/oidc";
import { clearToken } from "../lib/auth/tokens";
import { signIn } from "../test/harness";

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return { ...actual, renewSilently: () => Promise.reject(new AuthError("login_required", null)) };
});

const person = { id: "u1", email: "private@example.test", status: "active", display_name: "Private user" };
const role = { id: "r1", project_id: "p1", key: "cashier", display_name: "Cashier", permission_keys: ["sale:read"] };
const grant = { user_id: "u1", project_id: "p1", role_keys: ["cashier"] };
const keys = {
  projects: [...queryKeys.projects, "org-1"],
  users: [...queryKeys.users, "org-1", ""],
  roles: [...queryKeys.roles, "org-1", "p1"],
  grants: [...queryKeys.grants, "org-1", "u1"],
};

function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/projects/p1/authorizations"]}>
    <AuthProvider><Link to="/projects/p2/authorizations">Other project</Link><Routes>
      <Route path="/projects/:projectId/authorizations" element={<AuthorizationsPage />} />
    </Routes></AuthProvider>
  </MemoryRouter></QueryClientProvider>);
  return client;
}

function stub(handler?: (request: Request) => Response | Promise<Response> | undefined) {
  const requests: string[] = [];
  vi.stubGlobal("fetch", async (request: Request) => {
    requests.push(request.url);
    const response = handler?.(request);
    if (response !== undefined) return response;
    if (request.url.includes("/grants")) return Response.json({ grants: [grant] });
    if (request.url.includes("/roles")) return Response.json({ roles: [role] });
    if (request.url.includes("/users")) return Response.json({ users: [person] });
    return Response.json({ projects: [{ id: "p1", name: "Private project" }, { id: "p2", name: "Other project" }] });
  });
  return requests;
}

async function pick() {
  await userEvent.click(await screen.findByRole("button", { name: /private@example.test/i }));
  await screen.findByRole("button", { name: "Change roles" });
}

beforeEach(() => {
  clearToken();
  sessionStorage.clear();
  vi.stubEnv("VITE_AUTH_CLIENT_ID", "console-test-client");
  vi.stubEnv("VITE_AUTH_ISSUER", "https://auth.example.test");
  signIn();
});

describe("Authorizations shared collection states", () => {
  it("announces loading before the search prompt and clears a filtered empty result", async () => {
    let finish!: (response: Response) => void;
    let pending = true;
    stub((request) => request.url.includes("/users")
      ? pending ? new Promise<Response>((resolve) => { finish = resolve; }) : Response.json({ users: [] })
      : undefined);
    mount();
    expect(await screen.findByText("Searching")).toBeInTheDocument();
    expect(screen.queryByText(/Type an email address/)).not.toBeInTheDocument();
    pending = false;
    await act(async () => { finish(Response.json({ users: [] })); });
    await screen.findByText(/Type an email address/);
    await userEvent.type(screen.getByRole("searchbox"), "missing");
    expect(await screen.findByText("No users match that search")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Clear the search" }));
    await screen.findByText(/Type an email address/);
    expect(screen.getByRole("searchbox")).toHaveValue("");
  });

  it("retains stale search results and retries only the failed request", async () => {
    let fail = false;
    const requests = stub((request) => {
      if (fail && request.url.includes("/users")) throw new TypeError("Failed to fetch");
      return undefined;
    });
    const client = mount();
    const choice = await screen.findByRole("button", { name: /private@example.test/i });
    fail = true;
    await act(async () => { await client.invalidateQueries({ queryKey: keys.users }); });
    expect(await screen.findByRole("alert")).toHaveTextContent(/last-known data/i);
    expect(screen.getByRole("button", { name: /private@example.test/i })).toBe(choice);
    const before = requests.length;
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
    expect(requests.slice(before)).toHaveLength(1);
    expect(new URL(requests[before]).pathname).toBe("/v1/organizations/org-1/users");
  });

  it("retains an open role selection and retries only grants after a stale failure", async () => {
    let fail = false;
    const requests = stub((request) => fail && request.url.includes("/grants")
      ? Response.json({ error: { code: "SERVER_ERROR" } }, { status: 500 }) : undefined);
    const client = mount();
    await pick();
    await userEvent.click(screen.getByRole("button", { name: "Change roles" }));
    await userEvent.click(screen.getByRole("checkbox"));
    fail = true;
    await act(async () => { await client.invalidateQueries({ queryKey: keys.grants }); });
    expect(await screen.findByRole("alert")).toHaveTextContent(/last-known data/i);
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.getByRole("checkbox")).not.toBeChecked();
    const before = requests.length;
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
    expect(requests.slice(before)).toHaveLength(1);
    expect(new URL(requests[before]).pathname).toBe("/v1/organizations/org-1/users/u1/grants");
    expect(screen.getByRole("checkbox")).not.toBeChecked();
  });

  it.each(["projects", "roles", "users", "grants"] as const)("suppresses cached content and open dialogs after %s permission denial", async (resource) => {
    let deny = false;
    stub((request) => {
      const matches = resource === "projects" ? new URL(request.url).pathname.endsWith("/projects")
        : resource === "users" ? new URL(request.url).pathname.endsWith("/users")
        : request.url.includes(`/${resource}`);
      return deny && matches ? Response.json({ message: "Private server detail" }, { status: 403 }) : undefined;
    });
    const client = mount();
    await pick();
    await userEvent.click(screen.getByRole("button", { name: "Revoke all access" }));
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    deny = true;
    await act(async () => { await client.invalidateQueries({ queryKey: keys[resource] }); });
    expect(await screen.findByRole("alert")).toHaveTextContent(/review your access/i);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByRole("searchbox")).not.toBeInTheDocument();
    expect(screen.queryByText(/private@example.test|Private project|Private server detail|cashier/)).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });

  it("retains role choices on a stale role failure and retries only roles before saving", async () => {
    let fail = false;
    const requests = stub((request) => fail && request.url.includes("/roles")
      ? Response.json({ error: { code: "SERVER_ERROR" } }, { status: 500 }) : undefined);
    const client = mount();
    await pick();
    await userEvent.click(screen.getByRole("button", { name: "Change roles" }));
    fail = true;
    await act(async () => { await client.invalidateQueries({ queryKey: keys.roles }); });
    expect(await screen.findByRole("alert")).toHaveTextContent(/last-known data/i);
    expect(screen.getByRole("checkbox")).toBeChecked();
    const before = requests.length;
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
    expect(requests.slice(before)).toHaveLength(1);
    expect(new URL(requests[before]).pathname).toBe("/v1/organizations/org-1/projects/p1/roles");
    expect(screen.getByRole("checkbox")).toBeChecked();
  });

  it("clears the selected user and dialogs when the project changes", async () => {
    stub();
    mount();
    await pick();
    await userEvent.click(screen.getByRole("button", { name: "Change roles" }));
    await userEvent.click(screen.getByRole("link", { name: "Other project" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Change roles" })).not.toBeInTheDocument();
    expect(screen.getByRole("searchbox")).toHaveValue("");
  });
});
