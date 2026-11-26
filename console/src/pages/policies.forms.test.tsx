import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { PoliciesPage } from "./PoliciesPage";
import { AuthProvider } from "../lib/auth/AuthProvider";
import { AuthError } from "../lib/auth/oidc";
import { clearToken } from "../lib/auth/tokens";
import { OrgProvider, useOrgContext } from "../lib/org/OrgProvider";
import { signIn } from "../test/harness";

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return { ...actual, renewSilently: () => Promise.reject(new AuthError("login_required", null)) };
});

const settings = {
  password_policy: { min_length: 16, require_uppercase: false, max_age_days: 30 },
  mfa_required: false,
  session_lifetime_hours: 24,
  allowed_login_methods: ["password"],
};
const impact = { members: 5, without_factor: 2, mfa_required: false, grace_period_days: 14, grace_ends_at: null };

function SwitchOrganization() {
  const { switchTo } = useOrgContext();
  return <button onClick={() => switchTo("org-2")}>Switch organization</button>;
}

function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  render(<QueryClientProvider client={client}><MemoryRouter><AuthProvider><OrgProvider>
    <SwitchOrganization /><PoliciesPage />
  </OrgProvider></AuthProvider></MemoryRouter></QueryClientProvider>);
  return client;
}

function stub(write: (request: Request) => Response | Promise<Response>) {
  vi.stubGlobal("fetch", async (request: Request) => {
    if (request.method === "PATCH") return write(request);
    if (request.url.endsWith("/mfa-impact")) return Response.json(impact);
    const id = new URL(request.url).pathname.split("/").at(-1);
    return Response.json({ id, name: id === "org-2" ? "Other organization" : "Acme", settings });
  });
}

beforeEach(() => {
  clearToken(); sessionStorage.clear();
  vi.stubEnv("VITE_AUTH_CLIENT_ID", "console-test-client");
  vi.stubEnv("VITE_AUTH_ISSUER", "https://auth.example.test");
  signIn(["ORG_OWNER"]);
});

describe("policy explicit form", () => {
  it("validates on blur and submit, focuses the first invalid field, and never writes an invalid draft", async () => {
    const writes = vi.fn(() => Response.json({}));
    stub(writes); mount();
    const minimum = await screen.findByLabelText("Minimum length");
    await userEvent.clear(minimum);
    await userEvent.type(minimum, "4");
    expect(minimum).not.toHaveAttribute("aria-invalid");
    await userEvent.tab();
    expect(minimum).toHaveAccessibleDescription("Must be between 12 and 128.");
    const lifetime = screen.getByLabelText("Sessions last") as HTMLInputElement;
    lifetime.value = "0";
    fireEvent.submit(minimum.closest("form") as HTMLFormElement);
    await waitFor(() => expect(minimum).toHaveFocus());
    expect(screen.getByRole("status")).toHaveTextContent("Check 2 fields: Minimum length, Sessions last.");
    expect(lifetime).toHaveAttribute("aria-invalid", "true");
    expect(writes).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("reads browser-filled values before computing and confirming the consequences", async () => {
    const bodies: unknown[] = [];
    stub(async (request) => { bodies.push(await request.json()); return Response.json({}); });
    mount();
    const lifetime = await screen.findByLabelText("Sessions last") as HTMLInputElement;
    lifetime.value = "1";
    fireEvent.submit(lifetime.closest("form") as HTMLFormElement);
    expect(await screen.findByRole("dialog")).toHaveTextContent("Sessions drop from 24 to 1 hours.");
    expect(bodies).toHaveLength(0);
    await userEvent.click(screen.getByRole("button", { name: "Apply policies" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(bodies).toEqual([{ settings: { ...settings, session_lifetime_hours: 1 } }]);
  });

  it("maps nested API details to number and boolean controls and retains values for retry", async () => {
    let reject = true;
    const bodies: unknown[] = [];
    stub(async (request) => {
      bodies.push(await request.json());
      return reject ? Response.json({ error: { code: "VALIDATION_ERROR", details: [
        { field: "settings.password_policy.min_length", issue: "Use your organization's approved minimum." },
        { field: "password_policy.require_uppercase", issue: "Upper-case policy was refused." },
      ] } }, { status: 400 }) : Response.json({});
    });
    mount();
    const minimum = await screen.findByLabelText("Minimum length");
    await userEvent.clear(minimum); await userEvent.type(minimum, "18");
    await userEvent.click(screen.getByLabelText("Require an upper-case letter"));
    await userEvent.click(screen.getByRole("button", { name: "Save policies" }));
    await waitFor(() => expect(minimum).toHaveAccessibleDescription("Use your organization's approved minimum."));
    expect(minimum).toHaveFocus();
    expect(minimum).toHaveValue(18);
    expect(screen.getByLabelText("Require an upper-case letter")).toHaveAccessibleDescription("Upper-case policy was refused.");
    expect(screen.getByRole("status")).toHaveTextContent("Check 2 fields: Minimum length, Require an upper-case letter.");
    reject = false;
    await userEvent.click(screen.getByRole("button", { name: "Save policies" }));
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("Saved."));
    expect(bodies).toEqual(Array(2).fill({ settings: { ...settings, password_policy: { ...settings.password_policy, min_length: 18, require_uppercase: true } } }));
  });

  it("maps login-method errors back to their group after confirmation closes", async () => {
    stub(() => Response.json({ error: { code: "VALIDATION_ERROR", details: [
      { field: "settings.allowed_login_methods", issue: "This sign-in combination is unavailable." },
    ] } }, { status: 400 }));
    mount();
    const lifetime = await screen.findByLabelText("Sessions last");
    await userEvent.clear(lifetime); await userEvent.type(lifetime, "1");
    await userEvent.click(screen.getByRole("button", { name: "Save policies" }));
    await userEvent.click(screen.getByRole("button", { name: "Apply policies" }));
    const password = screen.getByRole("checkbox", { name: "password" });
    await waitFor(() => expect(password).toHaveFocus());
    expect(password).toHaveAccessibleDescription("This sign-in combination is unavailable.");
    expect(screen.getByRole("status")).toHaveTextContent("Check 1 field: Sign-in methods.");
  });

  it("keeps a failed network draft and gives retry guidance", async () => {
    let reject = true;
    const writes = vi.fn(() => { if (reject) throw new TypeError("Failed to fetch"); return Response.json({}); });
    stub(writes); mount();
    const lifetime = await screen.findByLabelText("Sessions last");
    await userEvent.clear(lifetime); await userEvent.type(lifetime, "48");
    await userEvent.click(screen.getByRole("button", { name: "Save policies" }));
    expect(await screen.findByRole("status")).toHaveTextContent("Your entries are retained. Try again.");
    expect(lifetime).toHaveValue(48);
    reject = false;
    await userEvent.click(screen.getByRole("button", { name: "Save policies" }));
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("Saved."));
    expect(writes).toHaveBeenCalledTimes(2);
  });

  it("locks editing, dismissal and duplicate saves until a confirmed save finishes", async () => {
    let finish!: (response: Response) => void;
    const writes = vi.fn(() => new Promise<Response>((resolve) => { finish = resolve; }));
    stub(writes); mount();
    const lifetime = await screen.findByLabelText("Sessions last");
    await userEvent.clear(lifetime); await userEvent.type(lifetime, "1");
    await userEvent.click(screen.getByRole("button", { name: "Save policies" }));
    expect(lifetime).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: "Apply policies" }));
    await waitFor(() => expect(writes).toHaveBeenCalledTimes(1));
    expect(screen.getByRole("button", { name: "Discard changes" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    await userEvent.keyboard("{Escape}");
    fireEvent.submit(lifetime.closest("form") as HTMLFormElement);
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(writes).toHaveBeenCalledTimes(1);
    await act(async () => { finish(Response.json({})); });
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("keeps a committed save successful when cache invalidation rejects", async () => {
    const invalidation = vi.spyOn(QueryClient.prototype, "invalidateQueries").mockRejectedValue(new Error("Refresh failed"));
    try {
      const writes = vi.fn(() => Response.json({}));
      stub(writes); mount();
      const lifetime = await screen.findByLabelText("Sessions last");
      await userEvent.clear(lifetime); await userEvent.type(lifetime, "48");
      await userEvent.click(screen.getByRole("button", { name: "Save policies" }));
      await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("Saved."));
      expect(screen.queryByText(/could not be saved/i)).not.toBeInTheDocument();
      expect(writes).toHaveBeenCalledTimes(1);
    } finally { invalidation.mockRestore(); }
  });

  it("isolates a late save response from the next organization's policy draft", async () => {
    let finish!: (response: Response) => void;
    stub(() => new Promise<Response>((resolve) => { finish = resolve; }));
    mount();
    const lifetime = await screen.findByLabelText("Sessions last");
    await userEvent.clear(lifetime); await userEvent.type(lifetime, "48");
    await userEvent.click(screen.getByRole("button", { name: "Save policies" }));
    await waitFor(() => expect(finish).toBeTypeOf("function"));
    await userEvent.click(screen.getByRole("button", { name: "Switch organization" }));
    await screen.findByText(/What Other organization requires/);
    const nextMinimum = screen.getByLabelText("Minimum length");
    await userEvent.clear(nextMinimum); await userEvent.type(nextMinimum, "20");
    await act(async () => { finish(Response.json({ error: { code: "VALIDATION_ERROR", details: [
      { field: "settings.password_policy.min_length", issue: "Previous organization refused." },
    ] } }, { status: 400 })); });
    expect(nextMinimum).toHaveValue(20);
    expect(nextMinimum).not.toHaveAttribute("aria-invalid");
    expect(screen.queryByText(/Previous organization refused|Saved\./)).not.toBeInTheDocument();
  });
});
