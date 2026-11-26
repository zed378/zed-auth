import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
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

const application = {
  id: "a1", name: "Payroll", type: "saml", has_secret: false, redirect_uris: [],
  saml: { entity_id: "urn:payroll", acs_url: "https://payroll.example/acs", certificate: "",
    attribute_release: ["email"], want_signed_requests: false, allow_idp_initiated: false },
};
type Write = { method: string; path: string; body: unknown };

function stub(write: (request: Write) => Promise<Response> | Response, applications = [application]) {
  const calls: Write[] = [];
  vi.stubGlobal("fetch", async (request: Request) => {
    const path = new URL(request.url).pathname;
    if (request.method === "GET") return Response.json({ applications });
    const call = { method: request.method, path, body: await request.json() };
    calls.push(call);
    return write(call);
  });
  return calls;
}

function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  client.setQueryData([...queryKeys.projects, "org-1"], [
    { id: "p1", name: "Payroll", created_at: "2026-11-26T00:00:00Z" },
    { id: "p2", name: "Other", created_at: "2026-11-26T00:00:00Z" },
  ]);
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/projects/p1"]}>
    <AuthProvider><Link to="/projects/p2">Other project</Link>
      <Routes><Route path="/projects/:projectId" element={<ApplicationsPage />} /></Routes>
    </AuthProvider>
  </MemoryRouter></QueryClientProvider>);
  return client;
}

async function register() {
  await userEvent.click(await screen.findByRole("button", { name: "Register application" }));
  await userEvent.type(screen.getByLabelText("Name"), "New payroll");
  await userEvent.selectOptions(screen.getByLabelText("Type"), "saml");
}

async function edit(name = "Payroll") {
  const row = await waitFor(() => {
    const label = screen.getAllByText(name).find((element) => element.closest("tr"));
    if (!label) throw new Error(`Application row ${name} is not loaded.`);
    return label.closest("tr") as HTMLElement;
  });
  await userEvent.click(within(row).getByRole("button", { name: "SAML settings" }));
}

beforeEach(() => {
  clearToken(); sessionStorage.clear();
  vi.stubEnv("VITE_AUTH_CLIENT_ID", "console-test-client");
  vi.stubEnv("VITE_AUTH_ISSUER", "https://auth.example.test");
  signIn();
});

describe("SAML registration form fields", () => {
  it("validates active fields on blur and submit and keeps both source drafts", async () => {
    const calls = stub(() => Response.json(application, { status: 201 }));
    mount(); await register();
    const metadata = screen.getByLabelText("Metadata document");
    await userEvent.type(metadata, " ");
    expect(metadata).not.toHaveAttribute("aria-invalid", "true");
    await userEvent.tab();
    expect(metadata).toHaveAccessibleDescription("Paste the service provider's metadata document.");
    await userEvent.clear(metadata); await userEvent.type(metadata, "<saved-draft/>");
    await userEvent.click(screen.getByLabelText("Enter the details"));
    await userEvent.click(screen.getByRole("button", { name: "Register" }));
    expect(screen.getByLabelText("Entity ID")).toHaveFocus();
    expect(screen.getByLabelText("Entity ID")).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByLabelText("Assertion Consumer Service URL")).toHaveAttribute("aria-invalid", "true");
    expect(calls).toHaveLength(0);
    await userEvent.type(screen.getByLabelText("Entity ID"), "urn:draft");
    await userEvent.click(screen.getByLabelText("Paste its metadata"));
    expect(screen.getByLabelText("Metadata document")).toHaveValue("<saved-draft/>");
    await userEvent.click(screen.getByLabelText("Enter the details"));
    expect(screen.getByLabelText("Entity ID")).toHaveValue("urn:draft");
  });

  it("maps nested manual errors and preserves the exact metadata/manual request exclusion", async () => {
    let reject = true;
    const calls = stub(() => reject
      ? Response.json({ error: { code: "VALIDATION_ERROR", details: [{ field: "saml.acs_url", issue: "Use an HTTPS ACS address." }] } }, { status: 400 })
      : Response.json(application, { status: 201 }));
    mount(); await register();
    await userEvent.type(screen.getByLabelText("Metadata document"), "<not-sent/>");
    await userEvent.click(screen.getByLabelText("Enter the details"));
    await userEvent.type(screen.getByLabelText("Entity ID"), "  urn:manual  ");
    const acs = screen.getByLabelText("Assertion Consumer Service URL");
    await userEvent.type(acs, "http://manual.example/acs");
    await userEvent.click(screen.getByLabelText("Email address"));
    await userEvent.click(screen.getByRole("button", { name: "Register" }));
    await waitFor(() => expect(acs).toHaveAccessibleDescription("Use an HTTPS ACS address."));
    expect(acs).toHaveFocus();
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await userEvent.click(screen.getByRole("button", { name: "Register application" }));
    expect(screen.getByLabelText("Entity ID")).toHaveValue("  urn:manual  ");
    reject = false;
    const reopenedAcs = screen.getByLabelText("Assertion Consumer Service URL");
    await userEvent.clear(reopenedAcs); await userEvent.type(reopenedAcs, "  https://manual.example/acs  ");
    await userEvent.click(screen.getByRole("button", { name: "Register" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(calls[1].body).toEqual({ name: "New payroll", type: "saml", saml: {
      entity_id: "urn:manual", acs_url: "https://manual.example/acs", attribute_release: ["email"],
      want_signed_requests: false, allow_idp_initiated: false,
    } });
  });

  it.each(["saml.metadata_xml", "saml.acs_url"])("maps %s from metadata parsing to the document", async (field) => {
    stub(() => Response.json({ error: { code: "VALIDATION_ERROR", details: [{ field, issue: "Publish an HTTPS POST endpoint in this document." }] } }, { status: 400 }));
    mount(); await register();
    await userEvent.type(screen.getByLabelText("Metadata document"), "<broken/>");
    await userEvent.click(screen.getByRole("button", { name: "Register" }));
    const metadata = screen.getByLabelText("Metadata document");
    await waitFor(() => expect(metadata).toHaveAccessibleDescription("Publish an HTTPS POST endpoint in this document."));
    expect(metadata).toHaveFocus();
    expect(metadata).toHaveValue("<broken/>");
  });

  it("reads SAML text from browser autofill and keeps checkbox choices in the payload", async () => {
    const calls = stub(() => Response.json(application, { status: 201 }));
    mount(); await register();
    (screen.getByLabelText("Metadata document") as HTMLTextAreaElement).value = "<autofilled/>";
    await userEvent.click(screen.getByLabelText("Allow sign-on started from this service"));
    // Autofill can occur after the last React change, without a change event.
    (screen.getByLabelText("Metadata document") as HTMLTextAreaElement).value = "<autofilled/>";
    await userEvent.click(screen.getByRole("button", { name: "Register" }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0].body).toEqual({ name: "New payroll", type: "saml", saml: {
      metadata_xml: "<autofilled/>", attribute_release: [], allow_idp_initiated: true,
    } });
  });
});

describe("SAML settings form", () => {
  it("maps certificate errors, retains a cancelled draft and isolates a different application", async () => {
    stub(() => Response.json({ error: { code: "VALIDATION_ERROR", details: [{ field: "saml.certificate", issue: "Use a PEM X.509 certificate." }] } }, { status: 400 }),
      [application, { ...application, id: "a2", name: "Second payroll" }]);
    mount(); await edit();
    const certificate = screen.getByLabelText("Signing certificate (optional)");
    await userEvent.type(certificate, "draft certificate");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(certificate).toHaveAccessibleDescription("Use a PEM X.509 certificate."));
    expect(certificate).toHaveFocus();
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await edit();
    expect(screen.getByLabelText("Signing certificate (optional)")).toHaveValue("draft certificate");
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await edit("Second payroll");
    expect(screen.getByLabelText("Signing certificate (optional)")).toHaveValue("");
  });

  it("reseeds a reopened editor when the registration changed on the server", async () => {
    const applications = [application];
    const calls = stub(() => Response.json(application), applications);
    const client = mount(); await edit();
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    // The certificate is rotated elsewhere while the editor is closed.
    applications[0] = { ...application, saml: { ...application.saml, certificate: "rotated certificate" } };
    await act(() => client.invalidateQueries({ queryKey: queryKeys.applications }));
    await edit();
    expect(screen.getByLabelText("Signing certificate (optional)")).toHaveValue("rotated certificate");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0].body).toMatchObject({ saml: { certificate: "rotated certificate" } });
  });

  it("maps indexed attribute errors to the editable release choices", async () => {
    stub(() => Response.json({ error: { code: "VALIDATION_ERROR", details: [{ field: "saml.attribute_release[0]", issue: "Choose a supported attribute." }] } }, { status: 400 }));
    mount(); await edit();
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    const email = screen.getByLabelText("Email address");
    await waitFor(() => expect(email).toHaveAccessibleDescription("Choose a supported attribute."));
    expect(email).toHaveFocus();
    expect(email).toBeChecked();
  });

  it("locks saving controls and dismissal, blocks duplicate requests and restores fields after rejection", async () => {
    let finish!: (response: Response) => void;
    const calls = stub(() => new Promise<Response>((resolve) => { finish = resolve; }));
    mount(); await edit();
    const entity = screen.getByLabelText("Entity ID");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(entity).toBeDisabled());
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Close dialog" })).not.toBeInTheDocument();
    fireEvent.submit(entity.closest("form") as HTMLFormElement);
    await userEvent.keyboard("{Escape}");
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(calls).toHaveLength(1);
    await act(async () => finish(Response.json({ error: { details: [{ field: "saml.entity_id", issue: "That entity ID is already registered." }] } }, { status: 409 })));
    await waitFor(() => expect(entity).toBeEnabled());
    expect(entity).toHaveAccessibleDescription("That entity ID is already registered.");
    expect(entity).toHaveFocus();
  });

  it("closes a committed edit even when cache invalidation rejects", async () => {
    const calls = stub(() => Response.json(application));
    mount(); await edit();
    const invalidation = vi.spyOn(QueryClient.prototype, "invalidateQueries").mockRejectedValue(new Error("Refresh unavailable"));
    try {
      await userEvent.click(screen.getByLabelText("Allow sign-on started from this service"));
      await userEvent.click(screen.getByRole("button", { name: "Save" }));
      await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
      expect(calls).toHaveLength(1);
      expect(calls[0].body).toEqual({ saml: { entity_id: "urn:payroll", acs_url: "https://payroll.example/acs",
        attribute_release: ["email"], want_signed_requests: false, allow_idp_initiated: true } });
    } finally { invalidation.mockRestore(); }
  });

  it("isolates a late edit completion from a new project's draft", async () => {
    let finish!: (response: Response) => void;
    stub(() => new Promise<Response>((resolve) => { finish = resolve; }));
    mount(); await edit();
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await userEvent.click(screen.getByRole("link", { name: "Other project" }));
    await edit();
    await userEvent.clear(screen.getByLabelText("Entity ID"));
    await userEvent.type(screen.getByLabelText("Entity ID"), "urn:new-project");
    await act(async () => finish(Response.json(application)));
    expect(screen.getByRole("dialog")).toHaveAccessibleName("SAML settings — Payroll");
    expect(screen.getByLabelText("Entity ID")).toHaveValue("urn:new-project");
  });
});
