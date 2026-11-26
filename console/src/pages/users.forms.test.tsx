import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { UsersPage } from "./UsersPage";
import { AuthProvider } from "../lib/auth/AuthProvider";
import { AuthError } from "../lib/auth/oidc";
import { clearToken } from "../lib/auth/tokens";
import { signIn } from "../test/harness";

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return { ...actual, renewSilently: () => Promise.reject(new AuthError("login_required", null)) };
});

const existingUser = { id: "u1", email: "member@example.test", status: "active", email_verified: true };
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), {
  status, headers: { "Content-Type": "application/json" },
});

function mount(handler: (request: Request) => Promise<Response>) {
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = input instanceof Request ? input : new Request(input, init);
    return handler(request);
  });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/users"]}>
    <AuthProvider><UsersPage /></AuthProvider>
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

describe("Users invitation form adoption", () => {
  it("validates the email on Next and focuses the field that needs correction", async () => {
    mount(async () => json({ users: [existingUser] }));
    await screen.findAllByText("member@example.test");
    await userEvent.click(screen.getByRole("button", { name: "Invite user" }));

    const email = screen.getByLabelText("Email address");
    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Enter an email address.");
    expect(email).toHaveFocus();
    expect(screen.getByText("Step 1 of 2")).toBeInTheDocument();

    await userEvent.type(email, "not-an-email");
    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Enter a valid email address.");
    expect(email).toHaveFocus();
  });

  it("retains invite details through review and back navigation, then sends trimmed API values", async () => {
    const posts: unknown[] = [];
    mount(async (request: Request) => {
      const url = new URL(request.url);
      if (url.pathname.endsWith("/projects")) return json({ projects: [] });
      if (request.method === "POST") {
        posts.push(await request.json());
        return json({ user: { ...existingUser, email: "invite@example.test", status: "invited" } });
      }
      return json({ users: [existingUser] });
    });
    await screen.findAllByText("member@example.test");
    await userEvent.click(screen.getByRole("button", { name: "Invite user" }));
    await userEvent.type(screen.getByLabelText("Email address"), "invite@example.test");
    await userEvent.type(screen.getByLabelText(/Display name/), "  Ada Lovelace  ");
    await userEvent.click(screen.getByRole("button", { name: "Next" }));

    expect(await screen.findByText("invite@example.test", { selector: "dd" })).toBeInTheDocument();
    expect(screen.getByText("Ada Lovelace", { selector: "dd" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Back" }));
    expect(screen.getByLabelText("Email address")).toHaveValue("invite@example.test");
    expect(screen.getByLabelText(/Display name/)).toHaveValue("  Ada Lovelace  ");
    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    await userEvent.click(screen.getByLabelText("Invite with no access yet. I will grant access separately."));
    await userEvent.click(screen.getByRole("button", { name: "Send invitation" }));

    await waitFor(() => expect(posts).toHaveLength(1));
    expect(posts[0]).toEqual({
      email: "invite@example.test",
      display_name: "Ada Lovelace",
      send_invite_email: true,
    });
  });

  it("maps API field details back to the email and retains the draft after rejection", async () => {
    let posts = 0;
    mount(async (request: Request) => {
      if (request.method === "POST") {
        posts += 1;
        return json({ error: {
          code: "VALIDATION_ERROR",
          message: "Invitation could not be sent.",
          details: [{ field: "email", issue: "That address is already in this organization." }],
        } }, 400);
      }
      return json({ users: [existingUser] });
    });
    await screen.findAllByText("member@example.test");
    await userEvent.click(screen.getByRole("button", { name: "Invite user" }));
    await userEvent.type(screen.getByLabelText("Email address"), "existing@example.test");
    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    await userEvent.click(screen.getByLabelText("Invite with no access yet. I will grant access separately."));
    await userEvent.click(screen.getByRole("button", { name: "Send invitation" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("That address is already in this organization.");
    expect(screen.getByLabelText("Email address")).toHaveValue("existing@example.test");
    expect(screen.getByLabelText("Email address")).toHaveAttribute("aria-invalid", "true");
    expect(posts).toBe(1);
  });
});
