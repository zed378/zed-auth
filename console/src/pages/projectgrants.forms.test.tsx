import { QueryClient } from "@tanstack/react-query";
import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Link } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ProjectGrantsPage } from "./ProjectGrantsPage";
import { AuthError } from "../lib/auth/oidc";
import { clearToken } from "../lib/auth/tokens";
import { renderScreen, signIn } from "../test/harness";

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return { ...actual, renewSilently: () => Promise.reject(new AuthError("login_required", null)) };
});

const partner = "3f2b8c1e-5d4a-4f7b-9c2e-1a6d8e0b7c45";
function mount(post: (request: Request) => Promise<Response>) {
  vi.stubGlobal("fetch", async (request: Request) => {
    if (request.method === "POST") return post(request);
    if (request.url.includes("/roles")) return Response.json({ roles: [{ key: "cashier", display_name: "Cashier" }] });
    if (request.url.includes("/grants")) return Response.json({ grants: [] });
    return Response.json({ projects: [{ id: "p1", name: "First" }, { id: "p2", name: "Second" }] });
  });
  renderScreen(<><Link to="/projects/p2/grants">Other project</Link><ProjectGrantsPage /></>, "/projects/p1/grants", "/projects/:projectId/grants");
}
async function choose() {
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Create grant" }));
  await user.type(screen.getByLabelText("Organization ID"), partner);
  await user.click(await screen.findByLabelText(/cashier/));
  return user;
}
beforeEach(() => {
  clearToken(); sessionStorage.clear();
  vi.stubEnv("VITE_AUTH_CLIENT_ID", "console-test-client");
  vi.stubEnv("VITE_AUTH_ISSUER", "https://auth.example.test");
  signIn();
});

describe("Project Grant shared forms", () => {
  it("waits for blur or submit and focuses the first invalid field", async () => {
    const post = vi.fn(async () => Response.json({}));
    mount(post);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Create grant" }));
    const input = screen.getByLabelText("Organization ID");
    await user.type(input, "invalid");
    expect(input).toHaveAttribute("aria-invalid", "false");
    await user.tab();
    expect(input).toHaveAttribute("aria-invalid", "true");
    await user.click(screen.getByRole("button", { name: "Review" }));
    expect(input).toHaveFocus();
    expect(screen.getByRole("status")).toHaveTextContent("Check 2 fields: Organization ID, Roles to share.");
    expect(post).not.toHaveBeenCalled();
  });

  it.each(["granted_org_id", "role_keys"])("maps server %s errors back to editable controls", async (field) => {
    mount(async () => Response.json({ error: { code: "VALIDATION_ERROR", details: [{ field, issue: "Choose a different value." }] } }, { status: 400 }));
    const user = await choose();
    await user.click(screen.getByRole("button", { name: "Review" }));
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Create grant" }));
    await waitFor(() => expect(screen.getByRole("dialog")).toHaveAccessibleName("Create a Project Grant"));
    const input = field === "granted_org_id" ? screen.getByLabelText("Organization ID") : screen.getByLabelText(/cashier/);
    await waitFor(() => expect(input).toHaveFocus());
    expect(screen.getByRole("status")).toHaveTextContent("Check 1 field:");
    expect(screen.getByLabelText("Organization ID")).toHaveValue(partner);
    expect(screen.getByLabelText(/cashier/)).toBeChecked();
  });

  it("retains failed drafts through dismissal and review back-navigation", async () => {
    mount(async () => { throw new TypeError("Failed to fetch"); });
    const user = await choose();
    await user.click(screen.getByRole("button", { name: "Review" }));
    expect(screen.getByText("Step 2 of 2: Confirm")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Back" }));
    expect(screen.getByLabelText(/cashier/)).toBeChecked();
    await user.click(screen.getByRole("button", { name: "Review" }));
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Create grant" }));
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("Your entries are retained"));
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    await user.click(screen.getByRole("button", { name: "Create grant" }));
    expect(screen.getByLabelText("Organization ID")).toHaveValue(partner);
    expect(screen.getByLabelText(/cashier/)).toBeChecked();
  });

  it("clears the saved draft even when cache invalidation rejects", async () => {
    const invalidation = vi.spyOn(QueryClient.prototype, "invalidateQueries").mockRejectedValue(new Error("Refresh failed"));
    const post = vi.fn(async () => Response.json({}, { status: 201 }));
    try {
      mount(post);
      const user = await choose();
      await user.click(screen.getByRole("button", { name: "Review" }));
      await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Create grant" }));
      await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
      await user.click(screen.getByRole("button", { name: "Create grant" }));
      expect(screen.getByLabelText("Organization ID")).toHaveValue("");
      expect(screen.getByLabelText(/cashier/)).not.toBeChecked();
      expect(post).toHaveBeenCalledTimes(1);
    } finally { invalidation.mockRestore(); }
  });

  it("isolates drafts and a late save completion across project navigation", async () => {
    let finish!: (response: Response) => void;
    mount(() => new Promise<Response>((resolve) => { finish = resolve; }));
    const user = await choose();
    await user.click(screen.getByRole("button", { name: "Review" }));
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Create grant" }));
    await user.click(screen.getByRole("link", { name: "Other project" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    await user.click(await screen.findByRole("button", { name: "Create grant" }));
    expect(screen.getByLabelText("Organization ID")).toHaveValue("");
    await user.type(screen.getByLabelText("Organization ID"), "new draft");
    await act(async () => { finish(Response.json({}, { status: 201 })); });
    expect(screen.getByRole("dialog")).toHaveAccessibleName("Create a Project Grant");
    expect(screen.getByLabelText("Organization ID")).toHaveValue("new draft");
  });
});
