import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, useLocation } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthProvider } from "../../lib/auth/AuthProvider";
import { clearToken, storeToken } from "../../lib/auth/tokens";
import { GlobalSearch } from "./GlobalSearch";

const token = (roles: string[]) => `header.${btoa(JSON.stringify({
  sub: "user-1", org_id: "org-1", roles, exp: 2_000_000_000,
})).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "")}.signature`;

function renderSearch(roles = ["ORG_ADMIN"]) {
  storeToken(token(roles), 3600);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>
    <MemoryRouter initialEntries={["/users"]}><AuthProvider><GlobalSearch /><LocationProbe /></AuthProvider></MemoryRouter>
  </QueryClientProvider>);
}

function LocationProbe() {
  const location = useLocation();
  return <output data-testid="location">{location.pathname + location.search}</output>;
}

beforeEach(() => {
  vi.stubEnv("VITE_AUTH_CLIENT_ID", "console-test-client");
  vi.stubEnv("VITE_AUTH_ISSUER", "https://auth.example.test");
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
    const url = new URL(typeof input === "string" ? input : input instanceof URL ? input.href : input.url);
    if (url.pathname === "/v1/me/organizations") {
      return Response.json({ organizations: [{ id: "org-1", name: "Acme", roles: ["ORG_ADMIN"] }] });
    }
    if (url.pathname === "/v1/organizations/org-1/projects") {
      return Response.json({ projects: [{ id: "project-1", name: "Billing", created_at: "", updated_at: "" }] });
    }
    if (url.pathname === "/v1/organizations/org-1/users") {
      return Response.json({ users: [{ id: "user-2", email: "billing@example.test", display_name: "Rina", status: "active", mfa_enabled: false, email_verified: true, created_at: "", updated_at: "" }] });
    }
    if (/^\/v1\/organizations\/org-1\/users\/[0-9a-f-]+$/i.test(url.pathname)) {
      return Response.json({ error: { code: "PERMISSION_DENIED", message: "Forbidden" } }, { status: 403 });
    }
    if (/^\/v1\/organizations\/org-1\/projects\/[0-9a-f-]+$/i.test(url.pathname)) {
      return Response.json({ error: { code: "NOT_FOUND", message: "Not found" } }, { status: 404 });
    }
    return Response.json({ error: { code: "NOT_FOUND", message: "Not found" } }, { status: 404 });
  }));
});

afterEach(() => {
  clearToken();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

describe("global search", () => {
  it("opens from Ctrl+K, groups matches, supports arrow and Enter selection", async () => {
    const user = userEvent.setup();
    renderSearch();
    await user.keyboard("{Control>}k{/Control}");

    const input = await screen.findByRole("combobox", { name: "Search organizations, projects, and users" });
    await user.type(input, "billing");

    const option = await screen.findByRole("option", { name: /Billing.*project-1.*Project/i });
    expect(screen.getByRole("group", { name: "Project results" })).toContainElement(option);
    expect(await screen.findByRole("option", { name: /Rina.*billing@example.test.*User/i })).toBeInTheDocument();

    await user.keyboard("{ArrowDown}{Enter}");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.getByTestId("location")).toHaveTextContent("/users/user-2?org=org-1");
  });

  it("keeps ordinary signed-in users out of directory search", async () => {
    renderSearch(["PROJECT_OWNER"]);
    fireEvent.keyDown(window, { key: "k", ctrlKey: true });
    expect(screen.queryByRole("button", { name: "Search" })).not.toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(vi.mocked(fetch)).not.toHaveBeenCalled();
  });

  it("scopes identifier lookups to the active org and does not distinguish forbidden from absent", async () => {
    const user = userEvent.setup();
    renderSearch();
    fireEvent.keyDown(window, { key: "k", ctrlKey: true });
    await user.type(await screen.findByRole("combobox"), "123e4567-e89b-42d3-a456-426614174000");

    expect(await screen.findByText("No matching resources.")).toBeInTheDocument();
    expect(screen.queryByText(/forbidden|not found|permission denied/i)).not.toBeInTheDocument();
    const requestedPaths = vi.mocked(fetch).mock.calls.map(([input]) =>
      new URL(typeof input === "string" ? input : input instanceof URL ? input.href : input.url).pathname,
    );
    expect(requestedPaths).toContain("/v1/organizations/org-1/projects/123e4567-e89b-42d3-a456-426614174000");
    expect(requestedPaths).toContain("/v1/organizations/org-1/users/123e4567-e89b-42d3-a456-426614174000");
    expect(requestedPaths.some((path) => path.includes("org-2"))).toBe(false);
  });

  it("dismisses with Escape and returns focus to the opener", async () => {
    const user = userEvent.setup();
    renderSearch();
    const opener = screen.getByRole("button", { name: "Search" });
    await user.click(opener);
    expect(screen.getByRole("combobox")).toHaveFocus();
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(opener).toHaveFocus();
  });
});
