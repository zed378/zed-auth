import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { UserDetailPage } from "./UserDetailPage";
import { queryKeys } from "../lib/api/client";
import { AuthProvider } from "../lib/auth/AuthProvider";
import { AuthError } from "../lib/auth/oidc";
import { clearToken } from "../lib/auth/tokens";
import { signIn } from "../test/harness";

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return { ...actual, renewSilently: () => Promise.reject(new AuthError("login_required", null)) };
});

const member = {
  id: "member-1",
  email: "private@example.test",
  username: "private-handle",
  display_name: "Private member",
  status: "active",
  email_verified: true,
  created_at: "2026-09-10T00:00:00Z",
};
const key = [...queryKeys.users, "org-1", "detail", "member-1"];
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), {
  status, headers: { "Content-Type": "application/json" },
});
const denied = () => json({ error: { code: "unknown_gateway_code", message: "Private server detail" } }, 403);

function mount(cached = false) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  if (cached) client.setQueryData(key, member, { updatedAt: Date.now() - 60_000 });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/users/member-1"]}>
        <AuthProvider>
          <Routes>
            <Route path="/users/:userId" element={<UserDetailPage />} />
          </Routes>
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return client;
}

beforeEach(() => {
  clearToken();
  sessionStorage.clear();
  vi.stubEnv("VITE_AUTH_CLIENT_ID", "console-test-client");
  vi.stubEnv("VITE_AUTH_ISSUER", "https://auth.example.test");
  signIn();
});

describe("User detail profile states", () => {
  it("announces the pending profile and does not flash blank profile fields", async () => {
    let finish!: (response: Response) => void;
    vi.stubGlobal("fetch", () => new Promise<Response>((resolve) => { finish = resolve; }));
    mount();
    expect(await screen.findByText("Loading user profile")).toBeInTheDocument();
    expect(screen.queryByText("No users yet")).not.toBeInTheDocument();
    expect(screen.queryByText("private@example.test")).not.toBeInTheDocument();
    await act(async () => { finish(json(member)); });
    expect(await screen.findByRole("heading", { name: "Private member" })).toBeInTheDocument();
    expect(screen.getByText("Private member", { selector: "dd" })).toBeInTheDocument();
    expect(screen.getByText("private@example.test", { selector: "dd" })).toBeInTheDocument();
  });

  it("treats a missing user response as unavailable instead of rendering an empty profile", async () => {
    vi.stubGlobal("fetch", async () => json({ error: { code: "NOT_FOUND" } }, 404));
    mount();
    expect(await screen.findByRole("alert")).toHaveTextContent("You do not have access to this");
    expect(screen.queryByText("Not asserted")).not.toBeInTheDocument();
    expect(screen.queryByText("private@example.test")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });

  it("shows an actionable error when the profile has no cached result", async () => {
    vi.stubGlobal("fetch", async () => json({ error: { code: "internal_error" } }, 500));
    mount();
    expect(await screen.findByRole("alert")).toHaveTextContent("The request could not be completed");
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
    expect(screen.queryByText("Not asserted")).not.toBeInTheDocument();
    expect(screen.queryByText("No users yet")).not.toBeInTheDocument();
  });

  it("keeps last-known profile details visible and retries only that user's detail read", async () => {
    const requests: string[] = [];
    let fail = true;
    vi.stubGlobal("fetch", async (request: Request) => {
      requests.push(request.url);
      return fail ? json({ error: { code: "internal_error" } }, 500) : json(member);
    });
    mount(true);
    const details = screen.getByText("Display name").closest("dl");
    expect(await screen.findByRole("alert")).toHaveTextContent(/last-known profile details/i);
    expect(screen.getByRole("heading", { name: "Private member" })).toBeInTheDocument();
    expect(screen.getByText("Private member", { selector: "dd" })).toBeInTheDocument();
    const countBeforeRetry = requests.length;
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
    expect(screen.getByText("Display name").closest("dl")).toBe(details);
    expect(requests).toHaveLength(countBeforeRetry + 1);
    expect(requests.every((url) => new URL(url).pathname === "/v1/organizations/org-1/users/member-1")).toBe(true);
  });

  it("hides stale profile data and dismisses the deactivate dialog after HTTP 403", async () => {
    let deny = false;
    vi.stubGlobal("fetch", async () => deny ? denied() : json(member));
    const client = mount(true);
    await userEvent.click(await screen.findByRole("button", { name: "Deactivate this user" }));
    expect(await screen.findByRole("dialog")).toBeInTheDocument();
    deny = true;
    await act(async () => { await client.invalidateQueries({ queryKey: key }); });
    expect(await screen.findByRole("alert")).toHaveTextContent(/review your access/i);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByText("Private member")).not.toBeInTheDocument();
    expect(screen.queryByText("private@example.test")).not.toBeInTheDocument();
    expect(screen.queryByText("Private server detail")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });
});
