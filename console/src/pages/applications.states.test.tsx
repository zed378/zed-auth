import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Link, MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ApplicationsPage } from "./ApplicationsPage";
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
const application = { id: "app-1", name: "Private application", type: "web", has_secret: true, redirect_uris: ["https://app.example/callback"] };
const samlApplication = { ...application, type: "saml", saml: {
  entity_id: "https://private.example/entity", acs_url: "https://private.example/acs",
  attribute_release: [], want_signed_requests: false, allow_idp_initiated: false,
} };
const key = [...queryKeys.applications, "org-1", "p1"];
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), {
  status, headers: { "Content-Type": "application/json" },
});

function mount(cached = false) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData([...queryKeys.projects, "org-1"], [project]);
  if (cached) client.setQueryData(key, [application], { updatedAt: Date.now() - 60_000 });
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/projects/p1"]}>
    <AuthProvider>
      <Link to="/projects/p2">Other project</Link>
      <Routes><Route path="/projects/:projectId" element={<ApplicationsPage />} /></Routes>
    </AuthProvider>
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

describe("Applications shared state adoption", () => {
  it("announces pending without empty, then offers the first registration action", async () => {
    let finish!: (response: Response) => void;
    vi.stubGlobal("fetch", () => new Promise<Response>((resolve) => { finish = resolve; }));
    mount();
    expect(await screen.findByText("Loading applications")).toBeInTheDocument();
    expect(screen.queryByText("No applications yet")).not.toBeInTheDocument();
    expect(screen.getByRole("table", { name: "Applications in Inventory" })).toBeInTheDocument();
    await act(async () => { finish(json({ applications: [] })); });
    expect(await screen.findByText("No applications yet")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Register the first application" })).toBeInTheDocument();
  });

  it("retains stale rows and retries only the failed applications request", async () => {
    const requests: string[] = [];
    let fail = true;
    vi.stubGlobal("fetch", async (request: Request) => {
      requests.push(request.url);
      return fail ? json({ error: { code: "internal_error" } }, 500) : json({ applications: [application] });
    });
    mount(true);
    const table = screen.getByRole("table", { name: "Applications in Inventory" });
    expect(await screen.findByRole("alert")).toHaveTextContent(/last-known data/i);
    expect(screen.getByText(application.name)).toBeInTheDocument();
    const beforeRetry = requests.length;
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
    expect(requests.length).toBe(beforeRetry + 1);
    expect(requests.every((url) => new URL(url).pathname === "/v1/organizations/org-1/projects/p1/applications")).toBe(true);
    expect(screen.getByRole("table")).toBe(table);
  });

  it("preserves an open registration draft after a list refresh failure", async () => {
    let fail = false;
    vi.stubGlobal("fetch", async () => fail ? json({ error: { code: "internal_error" } }, 500) : json({ applications: [application] }));
    const client = mount();
    await screen.findByText(application.name);
    await userEvent.click(screen.getByRole("button", { name: "Register application" }));
    await userEvent.type(screen.getByLabelText("Name"), "Unsaved application");
    fail = true;
    await act(async () => { await client.invalidateQueries({ queryKey: key }); });
    expect(await screen.findByRole("alert")).toHaveTextContent(/last-known data/i);
    expect(screen.getByLabelText("Name")).toHaveValue("Unsaved application");
    expect(screen.getByRole("dialog", { name: "Register an application" })).toBeInTheDocument();
  });

  it("hides cached rows, project name, actions and server details on an unknown-code HTTP 403", async () => {
    vi.stubGlobal("fetch", async () => json({ error: { code: "unrecognized_refusal", message: "Private server detail" } }, 403));
    mount(true);
    expect(await screen.findByRole("alert")).toHaveTextContent(/review your access/i);
    expect(screen.queryByText(/Inventory|Private application|Private server detail/)).not.toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Register/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });

  it("suppresses an open registration draft when a refresh refuses access", async () => {
    let deny = false;
    vi.stubGlobal("fetch", async () => deny ? json({ error: { code: "unrecognized_refusal" } }, 403) : json({ applications: [application] }));
    const client = mount();
    await screen.findByText(application.name);
    await userEvent.click(screen.getByRole("button", { name: "Register application" }));
    await userEvent.type(screen.getByLabelText("Name"), "Private draft");
    deny = true;
    await act(async () => { await client.invalidateQueries({ queryKey: key }); });
    expect(await screen.findByRole("alert")).toHaveTextContent(/review your access/i);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByDisplayValue("Private draft")).not.toBeInTheDocument();
  });

  it("suppresses an open SAML settings dialog after access refusal", async () => {
    let deny = false;
    vi.stubGlobal("fetch", async () => deny ? json({ error: { code: "PERMISSION_DENIED" } }, 403) : json({ applications: [samlApplication] }));
    const client = mount();
    await userEvent.click(await screen.findByRole("button", { name: "SAML settings" }));
    expect(screen.getByRole("dialog", { name: `SAML settings — ${application.name}` })).toBeInTheDocument();
    deny = true;
    await act(async () => { await client.invalidateQueries({ queryKey: key }); });
    expect(await screen.findByRole("alert")).toHaveTextContent(/review your access/i);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByDisplayValue(samlApplication.saml.entity_id)).not.toBeInTheDocument();
    expect(screen.queryByText(application.name)).not.toBeInTheDocument();
  });

  it("suppresses an issued client secret when access is subsequently refused", async () => {
    let deny = false;
    vi.stubGlobal("fetch", async (request: Request) => request.method === "POST"
      ? json({ ...application, name: "New application", client_secret: "one-time-private-secret" }, 201)
      : deny ? json({ error: { code: "PERMISSION_DENIED" } }, 403) : json({ applications: [application] }));
    const client = mount();
    await screen.findByText(application.name);
    await userEvent.click(screen.getByRole("button", { name: "Register application" }));
    await userEvent.type(screen.getByLabelText("Name"), "New application");
    await userEvent.click(screen.getByRole("button", { name: "Register" }));
    expect(await screen.findByLabelText("Client secret")).toHaveValue("one-time-private-secret");
    deny = true;
    await act(async () => { await client.invalidateQueries({ queryKey: key }); });
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(screen.getByRole("alert")).toHaveTextContent(/review your access/i);
    expect(screen.queryByDisplayValue("one-time-private-secret")).not.toBeInTheDocument();
  });

  it("clears dialogs and drafts when navigating to a different project", async () => {
    vi.stubGlobal("fetch", async () => json({ applications: [application] }));
    mount();
    await screen.findByText(application.name);
    await userEvent.click(screen.getByRole("button", { name: "Register application" }));
    await userEvent.type(screen.getByLabelText("Name"), "Previous project draft");
    await act(async () => { screen.getByRole("link", { name: "Other project", hidden: true }).click(); });
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    await userEvent.click(screen.getByRole("button", { name: "Register application" }));
    expect(screen.getByLabelText("Name")).toHaveValue("");
  });
});
