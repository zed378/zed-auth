import { screen, waitFor, within } from "@testing-library/react";
import { QueryClient } from "@tanstack/react-query";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ProjectsPage } from "./ProjectsPage";
import { AuthError } from "../lib/auth/oidc";
import { clearToken } from "../lib/auth/tokens";
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

describe("project creation form", () => {
  it("does not offer a committed create for retry when cache refresh fails", async () => {
    const invalidation = vi.spyOn(QueryClient.prototype, "invalidateQueries").mockRejectedValue(new Error("Refresh failed"));
    const posts = vi.fn();
    try {
      stubApi((_url, { method }) => {
        if (method === "POST") { posts(); return { status: 201, body: { id: "p1", name: "Created project" } }; }
        return { status: 200, body: { projects: [] } };
      });
      renderScreen(<ProjectsPage />, "/projects");
      const user = userEvent.setup();
      await user.click(await screen.findByRole("button", { name: "New project" }));
      await user.type(screen.getByLabelText("Name"), "Created project");
      await user.click(screen.getByRole("button", { name: "Create project" }));
      await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
      expect(posts).toHaveBeenCalledTimes(1);
      await user.click(screen.getByRole("button", { name: "New project" }));
      expect(screen.getByLabelText("Name")).toHaveValue("");
    } finally { invalidation.mockRestore(); }
  });

  it("validates untouched and duplicate names on submit or blur, never while typing", async () => {
    const posts = vi.fn();
    stubApi((_url, { method }) => {
      if (method === "POST") posts();
      return { status: 200, body: { projects: [{ id: "p1", name: "Existing", created_at: "2026-11-25T00:00:00Z" }] } };
    });
    renderScreen(<ProjectsPage />, "/projects");
    const user = userEvent.setup();
    await screen.findByText("Existing");
    await user.click(screen.getByRole("button", { name: "New project" }));
    const dialog = screen.getByRole("dialog", { name: "New project" });
    const name = within(dialog).getByLabelText("Name");
    await user.click(within(dialog).getByRole("button", { name: "Create project" }));
    expect(name).toHaveFocus();
    expect(name).toHaveAccessibleDescription("Enter a project name.");
    await user.type(name, "existing");
    expect(name).toHaveAccessibleDescription("Enter a project name.");
    await user.tab();
    expect(name).toHaveAccessibleDescription("A project with that name already exists in this organization.");
    expect(posts).not.toHaveBeenCalled();
  });

  it("maps a real API-client validation envelope to Name and keeps the draft for retry", async () => {
    const bodies: unknown[] = [];
    let reject = true;
    vi.stubGlobal("fetch", async (request: Request) => {
      if (request.method !== "POST") return Response.json({ projects: [] });
      bodies.push(await request.json());
      return reject
        ? Response.json({ error: { code: "VALIDATION_ERROR", message: "Invalid name", details: [{ field: "name", issue: "Choose another project name." }] } }, { status: 400 })
        : Response.json({ id: "p2", name: "Draft project", created_at: "2026-11-25T00:00:00Z" }, { status: 201 });
    });
    renderScreen(<ProjectsPage />, "/projects");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "New project" }));
    const name = screen.getByLabelText("Name");
    await user.type(name, "  Draft project  ");
    await user.click(screen.getByRole("button", { name: "Create project" }));
    await waitFor(() => expect(name).toHaveAccessibleDescription("Choose another project name."));
    expect(name).toHaveFocus();
    expect(name).toHaveValue("  Draft project  ");
    expect(screen.getByRole("status")).toHaveTextContent("Check 1 field: Name.");
    expect(bodies).toEqual([{ name: "Draft project" }]);
    reject = false;
    await user.click(screen.getByRole("button", { name: "Create project" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(bodies).toHaveLength(2);
    await user.click(screen.getByRole("button", { name: "New project" }));
    expect(screen.getByLabelText("Name")).toHaveValue("");
  });

  it("retains a failed network draft when the dialog is closed and reopened", async () => {
    vi.stubGlobal("fetch", async (request: Request) => {
      if (request.method === "POST") throw new TypeError("Failed to fetch");
      return Response.json({ projects: [] });
    });
    renderScreen(<ProjectsPage />, "/projects");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "New project" }));
    await user.type(screen.getByLabelText("Name"), "Retained draft");
    await user.click(screen.getByRole("button", { name: "Create project" }));
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("Your entries are retained. Try again."));
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    await user.click(screen.getByRole("button", { name: "New project" }));
    expect(screen.getByLabelText("Name")).toHaveValue("Retained draft");
  });
});
