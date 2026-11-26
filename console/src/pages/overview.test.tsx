import { screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { OverviewPage } from "./OverviewPage";
import { AuthError } from "../lib/auth/oidc";
import { clearToken } from "../lib/auth/tokens";
import { expectNoAxeViolations } from "../test/axe";
import { renderScreen, signIn, stubApi } from "../test/harness";

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return { ...actual, renewSilently: () => Promise.reject(new AuthError("login_required", null)) };
});

beforeEach(() => {
  clearToken();
  sessionStorage.clear();
  vi.stubEnv("VITE_AUTH_CLIENT_ID", "console-test-client");
  vi.stubEnv("VITE_AUTH_ISSUER", "https://auth.example.test");
  signIn();
});

describe("organization overview", () => {
  it("shows policy summaries and real destinations", async () => {
    stubApi((url) => {
      if (url.includes("/events")) return { status: 200, body: { events: [] } };
      if (url.includes("/projects")) return { status: 200, body: { projects: [{ id: "p1", name: "One" }] } };
      if (url.includes("/users")) return { status: 200, body: { users: [{ id: "u1", status: "active" }] } };
      return {
        status: 200,
        body: {
          id: "org-1",
          name: "Acme",
          settings: {
            mfa_required: true,
            password_policy: { min_length: 16, require_uppercase: false, max_age_days: 0 },
          },
        },
      };
    });

    const { container } = renderScreen(<OverviewPage />);

    expect(await screen.findByRole("heading", { name: "Acme" })).toBeInTheDocument();
    expect(screen.getByText("Required")).toBeInTheDocument();
    expect(screen.getByText("At least 16 characters; passwords do not expire.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "View projects" })).toHaveAttribute("href", "/projects");
    expect(screen.getByRole("link", { name: "Manage users" })).toHaveAttribute("href", "/users");
    expect(screen.getByRole("link", { name: "View audit log" })).toHaveAttribute("href", "/audit-log");
    await expectNoAxeViolations(container);
  });

  it("orients an organization with no members, projects, or recent activity", async () => {
    stubApi((url) => {
      if (url.includes("/events")) return { status: 200, body: { events: [] } };
      if (url.includes("/projects")) return { status: 200, body: { projects: [] } };
      if (url.includes("/users")) return { status: 200, body: { users: [] } };
      return { status: 200, body: { id: "org-1", name: "New workspace", settings: {} } };
    });

    renderScreen(<OverviewPage />);

    expect(await screen.findByRole("heading", { name: "New workspace" })).toBeInTheDocument();
    expect(await screen.findByText(/Nothing has happened in this organization/)).toBeInTheDocument();
    expect(screen.getByText("Not required")).toBeInTheDocument();
    expect(screen.getByText("At least 12 characters, including uppercase; expire every 90 days.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "View audit log" })).toBeInTheDocument();
  });
});
