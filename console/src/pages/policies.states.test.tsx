import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { PoliciesPage } from "./PoliciesPage";
import { queryKeys } from "../lib/api/client";
import { AuthProvider } from "../lib/auth/AuthProvider";
import { AuthError } from "../lib/auth/oidc";
import { clearToken } from "../lib/auth/tokens";
import { signIn } from "../test/harness";

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return { ...actual, renewSilently: () => Promise.reject(new AuthError("login_required", null)) };
});

const orgKey = [...queryKeys.organizations, "org-1"];
const organization = {
  id: "org-1",
  name: "Private Organization",
  settings: { password_policy: { min_length: 16, require_uppercase: false, max_age_days: 30 } },
};
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), {
  status, headers: { "Content-Type": "application/json" },
});

function mount(cached = false) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  if (cached) client.setQueryData(orgKey, organization, { updatedAt: Date.now() - 60_000 });
  render(<QueryClientProvider client={client}><MemoryRouter>
    <AuthProvider><PoliciesPage /></AuthProvider>
  </MemoryRouter></QueryClientProvider>);
  return client;
}

function stub(handler: (request: Request) => Response | Promise<Response>) {
  const requests: string[] = [];
  vi.stubGlobal("fetch", async (request: Request) => {
    requests.push(request.url);
    return handler(request);
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

describe("Policies shared screen states", () => {
  it("announces loading before rendering defaults for an organization with no saved settings", async () => {
    let finish!: (response: Response) => void;
    stub((request) => request.url.endsWith("/mfa-impact")
      ? json({ members: 0, without_factor: 0, mfa_required: false, grace_period_days: 14, grace_ends_at: null })
      : new Promise<Response>((resolve) => { finish = resolve; }));
    mount();

    expect(await screen.findByText("Loading this organization's policies")).toBeInTheDocument();
    expect(screen.queryByLabelText(/minimum length/i)).not.toBeInTheDocument();
    await act(async () => { finish(json({ id: "org-1", name: "Acme", settings: {} })); });
    const field = await screen.findByLabelText(/minimum length/i);
    expect(field).toHaveValue(null);
    expect(field.closest("div")?.textContent).toMatch(/the service applies 12/i);
    expect(screen.queryByText(/no policies yet/i)).not.toBeInTheDocument();
  });

  it("offers retry after a first-read server failure without rendering policy fields", async () => {
    stub((request) => request.url.endsWith("/mfa-impact")
      ? json({ members: 0, without_factor: 0, mfa_required: false, grace_period_days: 14, grace_ends_at: null })
      : json({ error: { code: "SERVER_ERROR" } }, 500));
    mount();

    expect(await screen.findByRole("alert")).toHaveTextContent(/request could not be completed/i);
    expect(screen.getByRole("button", { name: /try again/i })).toBeInTheDocument();
    expect(screen.queryByLabelText(/minimum length/i)).not.toBeInTheDocument();
  });

  it("keeps the policy form and local edits on a refresh failure, then retries the organization read", async () => {
    let fail = false;
    const requests = stub((request) => {
      if (request.url.endsWith("/mfa-impact")) {
        return json({ members: 0, without_factor: 0, mfa_required: false, grace_period_days: 14, grace_ends_at: null });
      }
      return fail ? json({ error: { code: "SERVER_ERROR" } }, 500) : json(organization);
    });
    const client = mount();
    const field = await screen.findByLabelText(/minimum length/i);
    await userEvent.clear(field);
    await userEvent.type(field, "18");
    fail = true;
    await act(async () => { await client.invalidateQueries({ queryKey: orgKey }); });

    expect(await screen.findByRole("alert")).toHaveTextContent(/last-known policies/i);
    expect(screen.getByLabelText(/minimum length/i)).toHaveValue(18);
    const beforeRetry = requests.length;
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: /try again/i }));
    await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
    expect(requests.length).toBe(beforeRetry + 1);
    expect(new URL(requests[beforeRetry]).pathname).toBe("/v1/organizations/org-1");
    expect(screen.getByLabelText(/minimum length/i)).toHaveValue(18);
  });

  it("suppresses cached policies and local drafts after access is refused", async () => {
    let deny = false;
    stub((request) => {
      if (request.url.endsWith("/mfa-impact")) {
        return json({ members: 0, without_factor: 0, mfa_required: false, grace_period_days: 14, grace_ends_at: null });
      }
      return deny ? json({ error: { code: "PERMISSION_DENIED", message: "Private detail" } }, 403) : json(organization);
    });
    const client = mount();
    const field = await screen.findByLabelText(/minimum length/i);
    await userEvent.clear(field);
    await userEvent.type(field, "18");
    deny = true;
    await act(async () => { await client.invalidateQueries({ queryKey: orgKey }); });

    expect(await screen.findByRole("alert")).toHaveTextContent(/do not have access/i);
    expect(screen.queryByLabelText(/minimum length/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/Private Organization|Private detail/)).not.toBeInTheDocument();
    expect(screen.queryByDisplayValue("18")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /try again/i })).not.toBeInTheDocument();
  });
});
