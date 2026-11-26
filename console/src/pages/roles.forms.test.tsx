import { QueryClient } from "@tanstack/react-query";
import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Link } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { RolesPage } from "./RolesPage";
import { AuthError } from "../lib/auth/oidc";
import { clearToken } from "../lib/auth/tokens";
import { OrgProvider } from "../lib/org/OrgProvider";
import { renderScreen, signIn } from "../test/harness";

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return { ...actual, renewSilently: () => Promise.reject(new AuthError("login_required", null)) };
});

const role = {
  id: "r1", project_id: "p1", key: "cashier", display_name: "Cashier",
  permission_keys: ["sale:read"], is_builtin: false, grant_count: 0,
};
const path = "/projects/p1/roles";
const route = "/projects/:projectId/roles";

function listResponse(request: Request, roles: unknown[] = []) {
  return Response.json(request.url.includes("/roles") ? { roles } : {
    projects: [{ id: "p1", name: "Till" }, { id: "p2", name: "Inventory" }],
  });
}

beforeEach(() => {
  clearToken();
  sessionStorage.clear();
  vi.stubEnv("VITE_AUTH_CLIENT_ID", "console-test-client");
  vi.stubEnv("VITE_AUTH_ISSUER", "https://auth.example.test");
  signIn();
});

describe("shared role forms", () => {
  it("validates untouched fields on submit and focuses the first failure without posting", async () => {
    const posts = vi.fn();
    vi.stubGlobal("fetch", async (request: Request) => {
      if (request.method === "POST") posts();
      return listResponse(request);
    });
    renderScreen(<RolesPage />, path, route);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Define role" }));
    const dialog = within(screen.getByRole("dialog"));
    await user.click(dialog.getByRole("button", { name: "Define role" }));
    expect(dialog.getByLabelText("Key")).toHaveFocus();
    expect(dialog.getByLabelText("Key")).toHaveAccessibleDescription("A key is required.");
    expect(dialog.getByRole("status")).toHaveTextContent("Check 2 fields: Key, Name.");
    expect(posts).not.toHaveBeenCalled();
  });

  it("maps the API permission field back to its textarea and retains the draft for retry", async () => {
    const bodies: unknown[] = [];
    let reject = true;
    vi.stubGlobal("fetch", async (request: Request) => {
      if (request.method !== "POST") return listResponse(request);
      bodies.push(await request.json());
      return reject ? Response.json({ error: {
        code: "VALIDATION_ERROR", message: "Invalid permissions",
        details: [{ field: "permission_keys", issue: "That permission is not available in this project." }],
      } }, { status: 400 }) : Response.json(role, { status: 201 });
    });
    renderScreen(<RolesPage />, path, route);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Define role" }));
    const dialog = within(screen.getByRole("dialog"));
    await user.type(dialog.getByLabelText("Key"), "  cashier  ");
    await user.type(dialog.getByLabelText("Name"), "  Cashier  ");
    await user.type(dialog.getByLabelText("Permissions"), " sale:read, sale:create ");
    await user.click(dialog.getByRole("button", { name: "Define role" }));
    const permissions = dialog.getByLabelText("Permissions");
    await waitFor(() => expect(permissions).toHaveAccessibleDescription("That permission is not available in this project."));
    expect(permissions).toHaveFocus();
    expect(permissions).toHaveValue(" sale:read, sale:create ");
    expect(dialog.getByRole("status")).toHaveTextContent("Check 1 field: Permissions.");
    expect(bodies).toEqual([{ key: "cashier", display_name: "Cashier", permission_keys: ["sale:read", "sale:create"] }]);
    reject = false;
    await user.click(dialog.getByRole("button", { name: "Define role" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: "Define role" }));
    expect(screen.getByLabelText("Key")).toHaveValue("");
    expect(screen.queryByText("Changes saved.")).not.toBeInTheDocument();
    expect(bodies).toHaveLength(2);
  });

  it("retains a network-failed edit on cancel and sends only editable fields on retry", async () => {
    const bodies: unknown[] = [];
    let reject = true;
    vi.stubGlobal("fetch", async (request: Request) => {
      if (request.method !== "PATCH") return listResponse(request, [role]);
      bodies.push(await request.json());
      if (reject) throw new TypeError("Failed to fetch");
      return Response.json(role);
    });
    renderScreen(<RolesPage />, path, route);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Edit" }));
    const name = screen.getByLabelText("Name");
    await user.clear(name);
    await user.type(name, "Senior cashier");
    await user.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("Your entries are retained. Try again."));
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    await user.click(screen.getByRole("button", { name: "Edit" }));
    expect(screen.getByLabelText("Name")).toHaveValue("Senior cashier");
    expect(screen.getByLabelText("Key")).toHaveAttribute("readonly");
    reject = false;
    await user.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(bodies).toEqual([
      { display_name: "Senior cashier", permission_keys: ["sale:read"] },
      { display_name: "Senior cashier", permission_keys: ["sale:read"] },
    ]);
  });

  it("keeps a committed create successful when refreshing cached roles fails", async () => {
    const invalidate = vi.spyOn(QueryClient.prototype, "invalidateQueries").mockRejectedValue(new Error("Refresh failed"));
    const posts = vi.fn();
    try {
      vi.stubGlobal("fetch", async (request: Request) => {
        if (request.method !== "POST") return listResponse(request);
        posts();
        return Response.json(role, { status: 201 });
      });
      renderScreen(<RolesPage />, path, route);
      const user = userEvent.setup();
      await user.click(await screen.findByRole("button", { name: "Define role" }));
      const dialog = within(screen.getByRole("dialog"));
      await user.type(dialog.getByLabelText("Key"), "cashier");
      await user.type(dialog.getByLabelText("Name"), "Cashier");
      await user.click(dialog.getByRole("button", { name: "Define role" }));
      await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
      expect(posts).toHaveBeenCalledTimes(1);
    } finally { invalidate.mockRestore(); }
  });

  it("isolates drafts and late mutation completion across organization and project navigation", async () => {
    let complete: ((response: Response) => void) | undefined;
    vi.stubGlobal("fetch", async (request: Request) => {
      if (request.method === "POST") return new Promise<Response>((resolve) => { complete = resolve; });
      return listResponse(request);
    });
    renderScreen(<OrgProvider>
      <Link to="/projects/p2/roles?org=org-2">Switch scope</Link>
      <RolesPage />
    </OrgProvider>, path, route);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Define role" }));
    const first = within(screen.getByRole("dialog"));
    await user.type(first.getByLabelText("Key"), "cashier");
    await user.type(first.getByLabelText("Name"), "Cashier");
    await user.click(first.getByRole("button", { name: "Define role" }));
    await waitFor(() => expect(complete).toBeTypeOf("function"));
    await user.click(screen.getByRole("link", { name: "Switch scope" }));
    await user.click(await screen.findByRole("button", { name: "Define role" }));
    expect(screen.getByLabelText("Key")).toHaveValue("");
    await user.type(screen.getByLabelText("Key"), "inventory");
    await act(async () => { complete?.(Response.json(role, { status: 201 })); });
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.getByLabelText("Key")).toHaveValue("inventory");
  });
});
