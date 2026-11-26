import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ProjectsPage } from "./ProjectsPage";
import { queryKeys } from "../lib/api/client";
import { AuthProvider } from "../lib/auth/AuthProvider";
import { AuthError } from "../lib/auth/oidc";
import { clearToken } from "../lib/auth/tokens";
import { signIn } from "../test/harness";

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return { ...actual, renewSilently: () => Promise.reject(new AuthError("login_required", null)) };
});

const project = { id: "p1", name: "Inventory", created_at: "2026-11-25T00:00:00Z" };
const key = [...queryKeys.projects, "org-1"];
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), {
  status, headers: { "Content-Type": "application/json" },
});

function mount(cached = false) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  if (cached) client.setQueryData(key, [project], { updatedAt: Date.now() - 60_000 });
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/projects"]}>
    <AuthProvider><ProjectsPage /></AuthProvider>
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

describe("Projects shared state adoption", () => {
  it("announces loading without flashing empty, then offers the initial create action", async () => {
    let finish!: (response: Response) => void;
    vi.stubGlobal("fetch", () => new Promise<Response>((resolve) => { finish = resolve; }));
    mount();
    expect(await screen.findByText("Loading projects")).toBeInTheDocument();
    expect(screen.getByRole("table", { name: "Projects in this organization" })).toBeInTheDocument();
    expect(screen.queryByText("No projects yet")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create the first project" })).not.toBeInTheDocument();
    await act(async () => { finish(json({ projects: [] })); });
    expect(await screen.findByText("No projects yet")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create the first project" })).toBeInTheDocument();
  });

  it("clears an unmatched filter without offering to create a first project or fetching again", async () => {
    const fetch = vi.fn(async () => json({ projects: [project] }));
    vi.stubGlobal("fetch", fetch);
    mount();
    await screen.findByText("Inventory");
    await userEvent.type(screen.getByRole("searchbox", { name: "Search projects" }), "missing");
    expect(screen.getByText("No projects match that search")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create the first project" })).not.toBeInTheDocument();
    const beforeClear = fetch.mock.calls.length;
    await userEvent.click(screen.getByRole("button", { name: "Clear the search" }));
    expect(screen.getByText("Inventory")).toBeInTheDocument();
    expect(screen.getByRole("searchbox")).toHaveValue("");
    expect(fetch.mock.calls.length).toBe(beforeClear);
  });

  it("keeps stale rows mounted and retries only the failed projects request", async () => {
    const requests: string[] = [];
    let fail = true;
    vi.stubGlobal("fetch", async (request: Request) => {
      requests.push(request.url);
      return fail ? json({ error: { code: "internal_error" } }, 500) : json({ projects: [project] });
    });
    mount(true);
    const table = screen.getByRole("table", { name: "Projects in this organization" });
    expect(await screen.findByRole("alert")).toHaveTextContent(/last-known data/i);
    expect(screen.getByText("Inventory")).toBeInTheDocument();
    expect(screen.getByRole("table")).toBe(table);
    const beforeRetry = requests.length;
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
    expect(requests.length).toBe(beforeRetry + 1);
    expect(requests.every((url) => new URL(url).pathname === "/v1/organizations/org-1/projects")).toBe(true);
    expect(screen.getByRole("table")).toBe(table);
  });

  it("keeps a creation draft and search intact when a list refresh fails", async () => {
    let fail = false;
    vi.stubGlobal("fetch", async () => fail
      ? json({ error: { code: "internal_error" } }, 500)
      : json({ projects: [project] }));
    const client = mount();
    await screen.findByText("Inventory");
    await userEvent.type(screen.getByRole("searchbox"), "Invent");
    await userEvent.click(screen.getByRole("button", { name: "New project" }));
    await userEvent.type(screen.getByLabelText("Name"), "Unsaved project");
    fail = true;
    await act(async () => { await client.invalidateQueries({ queryKey: key }); });
    expect(await screen.findByRole("alert")).toHaveTextContent(/last-known data/i);
    expect(screen.getByRole("dialog", { name: "New project" })).toBeInTheDocument();
    expect(screen.getByLabelText("Name")).toHaveValue("Unsaved project");
    expect(screen.getByRole("searchbox")).toHaveValue("Invent");
  });

  it("suppresses cached rows, resource actions and server details after access is refused", async () => {
    vi.stubGlobal("fetch", async () => json({ error: {
      code: "PERMISSION_DENIED", message: "Inventory is private", details: [{ issue: "Confidential project" }],
    } }, 403));
    mount(true);
    expect(await screen.findByRole("alert")).toHaveTextContent(/review your access/i);
    expect(screen.queryByText(/Inventory|Confidential project/)).not.toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.queryByRole("searchbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "New project" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });

  it("hides an open creation dialog and its draft when a refresh refuses access", async () => {
    let deny = false;
    vi.stubGlobal("fetch", async () => deny
      ? json({ error: { code: "PERMISSION_DENIED" } }, 403)
      : json({ projects: [project] }));
    const client = mount();
    await screen.findByText("Inventory");
    await userEvent.click(screen.getByRole("button", { name: "New project" }));
    await userEvent.type(screen.getByLabelText("Name"), "Private draft");
    deny = true;
    await act(async () => { await client.invalidateQueries({ queryKey: key }); });
    expect(await screen.findByRole("alert")).toHaveTextContent(/review your access/i);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByDisplayValue("Private draft")).not.toBeInTheDocument();
    expect(screen.queryByText("Inventory")).not.toBeInTheDocument();
  });
});
