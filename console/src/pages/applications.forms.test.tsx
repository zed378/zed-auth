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

function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData([...queryKeys.projects, "org-1"], [
    { id: "p1", name: "Inventory", created_at: "2026-11-25T00:00:00Z" },
    { id: "p2", name: "Other", created_at: "2026-11-25T00:00:00Z" },
  ]);
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/projects/p1"]}>
    <AuthProvider><Link to="/projects/p2">Other project</Link>
      <Routes><Route path="/projects/:projectId" element={<ApplicationsPage />} /></Routes>
    </AuthProvider>
  </MemoryRouter></QueryClientProvider>);
  return client;
}

async function open() {
  await userEvent.click(await screen.findByRole("button", { name: "Register application" }));
}

beforeEach(() => {
  clearToken(); sessionStorage.clear();
  vi.stubEnv("VITE_AUTH_CLIENT_ID", "console-test-client");
  vi.stubEnv("VITE_AUTH_ISSUER", "https://auth.example.test");
  signIn();
});

describe("application registration shared form", () => {
  it("validates blank names on blur and submit, keeping submit available", async () => {
    const posts = vi.fn();
    vi.stubGlobal("fetch", async (request: Request) => {
      if (request.method === "POST") posts();
      return Response.json({ applications: [] });
    });
    mount(); await open();
    const name = screen.getByLabelText("Name");
    expect(name).not.toHaveAttribute("aria-invalid", "true");
    await userEvent.type(name, " ");
    expect(name).not.toHaveAttribute("aria-invalid", "true");
    await userEvent.tab();
    expect(name).toHaveAccessibleDescription("Enter an application name.");
    await userEvent.click(screen.getByRole("button", { name: "Register" }));
    expect(name).toHaveFocus();
    expect(posts).not.toHaveBeenCalled();
  });

  it("maps redirect field errors, preserves drafts and sends trimmed retry payloads", async () => {
    const bodies: unknown[] = [];
    let reject = true;
    vi.stubGlobal("fetch", async (request: Request) => {
      if (request.method !== "POST") return Response.json({ applications: [] });
      bodies.push(await request.json());
      return reject ? Response.json({ error: { code: "VALIDATION_ERROR", details: [{ field: "redirect_uris", issue: "Choose an exact callback URL." }] } }, { status: 400 })
        : Response.json({ id: "a1", name: "Portal", type: "spa", redirect_uris: [], has_secret: false }, { status: 201 });
    });
    mount(); await open();
    await userEvent.type(screen.getByLabelText("Name"), "  Portal  ");
    await userEvent.selectOptions(screen.getByLabelText("Type"), "spa");
    await userEvent.type(screen.getByLabelText("Redirect URIs"), "  https://portal.example/callback  ");
    await userEvent.click(screen.getByRole("button", { name: "Register" }));
    const redirects = screen.getByLabelText("Redirect URIs");
    await waitFor(() => expect(redirects).toHaveAccessibleDescription("Choose an exact callback URL."));
    expect(redirects).toHaveFocus();
    expect(screen.getByLabelText("Name")).toHaveValue("  Portal  ");
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await open();
    expect(screen.getByLabelText("Name")).toHaveValue("  Portal  ");
    reject = false;
    await userEvent.click(screen.getByRole("button", { name: "Register" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(bodies).toEqual(Array(2).fill({ name: "Portal", type: "spa", redirect_uris: ["https://portal.example/callback"] }));
    await open();
    expect(screen.getByLabelText("Name")).toHaveValue("");
    expect(screen.getByLabelText("Type")).toHaveValue("web");
  });

  it("retains network-failed entries with actionable guidance", async () => {
    vi.stubGlobal("fetch", async (request: Request) => {
      if (request.method === "POST") throw new TypeError("Failed to fetch");
      return Response.json({ applications: [] });
    });
    mount(); await open();
    await userEvent.type(screen.getByLabelText("Name"), "Draft portal");
    await userEvent.click(screen.getByRole("button", { name: "Register" }));
    expect(await screen.findByText("Changes could not be saved. Your entries are retained. Try again.")).toBeInTheDocument();
    expect(screen.getByLabelText("Name")).toHaveValue("Draft portal");
  });

  it("validates a browser-autofilled name and preserves the typed API payload", async () => {
    const bodies: unknown[] = [];
    vi.stubGlobal("fetch", async (request: Request) => {
      if (request.method !== "POST") return Response.json({ applications: [] });
      bodies.push(await request.json());
      return Response.json({ id: "a1", name: "Autofilled portal", has_secret: false }, { status: 201 });
    });
    mount(); await open();
    const name = screen.getByLabelText("Name") as HTMLInputElement;
    name.value = "  Autofilled portal  ";
    await userEvent.click(screen.getByRole("button", { name: "Register" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(bodies).toEqual([{ name: "Autofilled portal", type: "web", redirect_uris: [] }]);
  });

  it("shows the issued secret even when cache invalidation rejects", async () => {
    const invalidation = vi.spyOn(QueryClient.prototype, "invalidateQueries").mockRejectedValue(new Error("Refresh failed"));
    const posts = vi.fn();
    try {
      vi.stubGlobal("fetch", async (request: Request) => {
        if (request.method !== "POST") return Response.json({ applications: [] });
        posts();
        return Response.json({ id: "a1", name: "Portal", client_secret: "issued-once" }, { status: 201 });
      });
      mount(); await open();
      await userEvent.type(screen.getByLabelText("Name"), "Portal");
      await userEvent.click(screen.getByRole("button", { name: "Register" }));
      expect(await screen.findByLabelText("Client secret")).toHaveValue("issued-once");
      expect(posts).toHaveBeenCalledTimes(1);
      expect(screen.queryByRole("dialog", { name: "Register an application" })).not.toBeInTheDocument();
    } finally { invalidation.mockRestore(); }
  });

  it("locks pending controls and isolates late issuance from a new project draft", async () => {
    let finish!: (response: Response) => void;
    const posts: string[] = [];
    vi.stubGlobal("fetch", async (request: Request) => {
      if (request.method !== "POST") return Response.json({ applications: [] });
      posts.push(new URL(request.url).pathname);
      return new Promise<Response>((resolve) => { finish = resolve; });
    });
    mount(); await open();
    await userEvent.type(screen.getByLabelText("Name"), "Old project");
    await userEvent.click(screen.getByRole("button", { name: "Register" }));
    await waitFor(() => expect(finish).toBeTypeOf("function"));
    expect(screen.getByLabelText("Name")).toBeDisabled();
    expect(screen.getByLabelText("Type")).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    await userEvent.keyboard("{Escape}");
    expect(screen.getByRole("dialog", { name: "Register an application" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("link", { name: "Other project" }));
    await open();
    await userEvent.type(screen.getByLabelText("Name"), "New project draft");
    await act(async () => { finish(Response.json({ id: "a1", name: "Old project", client_secret: "old-project-secret" }, { status: 201 })); });
    expect(screen.getByLabelText("Name")).toHaveValue("New project draft");
    expect(screen.queryByLabelText("Client secret")).not.toBeInTheDocument();
    expect(posts).toEqual(["/v1/organizations/org-1/projects/p1/applications"]);
  });
});
