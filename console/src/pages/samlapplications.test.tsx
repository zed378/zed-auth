import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ApplicationsPage } from "./ApplicationsPage";
import { AuthError } from "../lib/auth/oidc";
import { clearToken } from "../lib/auth/tokens";
import { expectNoAxeViolations } from "../test/axe";
import { renderScreen, signIn } from "../test/harness";

/**
 * SAML applications on the Applications tab (P4-09).
 *
 * The failures worth catching are the ones a plausible screen commits: a SAML
 * registration sent with redirect URIs, metadata sent alongside the fields it
 * replaces, a switch that changes in the form and is never sent, and a
 * certificate about to expire shown as healthy.
 */

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return {
    ...actual,
    renewSilently: () => Promise.reject(new AuthError("login_required", null)),
  };
});

const project = { id: "p1", name: "Payroll", org_id: "o1", created_at: "2026-09-01T00:00:00Z" };

const samlApp = {
  id: "a1",
  project_id: "p1",
  name: "Legacy payroll",
  type: "saml",
  has_secret: false,
  redirect_uris: [],
  post_logout_redirect_uris: [],
  allowed_origins: [],
  grant_types: [],
  created_at: "2026-09-01T00:00:00Z",
  updated_at: "2026-09-01T00:00:00Z",
  saml: {
    entity_id: "https://payroll.example.test",
    acs_url: "https://payroll.example.test/acs",
    attribute_release: ["email"],
    want_signed_requests: false,
    allow_idp_initiated: false,
    certificate_expires_at: "2027-09-01T00:00:00Z",
    certificate_expires_soon: false,
  },
};

type Call = { url: string; method: string; body?: Record<string, unknown> };

function stub(
  applications: unknown[],
  write: (call: Call) => { status: number; body: unknown } = () => ({
    status: 201,
    body: { ...samlApp, id: "new" },
  }),
): Call[] {
  const calls: Call[] = [];
  vi.stubGlobal("fetch", async (input: RequestInfo | URL) => {
    const request = input as Request;
    const url = request.url;
    const method = request.method;
    let answer: { status: number; body: unknown };
    if (method !== "GET") {
      const text = await request.text();
      const call = { url, method, body: text === "" ? undefined : JSON.parse(text) };
      calls.push(call);
      answer = write(call);
    } else if (url.includes("/applications")) {
      answer = { status: 200, body: { applications } };
    } else {
      answer = { status: 200, body: { projects: [project] } };
    }
    return new Response(JSON.stringify(answer.body), {
      status: answer.status,
      headers: { "Content-Type": "application/json" },
    });
  });
  return calls;
}

beforeEach(() => {
  clearToken();
  sessionStorage.clear();
  vi.stubEnv("VITE_AUTH_CLIENT_ID", "console-test-client");
  vi.stubEnv("VITE_AUTH_ISSUER", "https://auth.example.test");
  signIn();
});

async function openRegister() {
  renderScreen(<ApplicationsPage />, "/projects/p1", "/projects/:projectId");
  await userEvent.click(await screen.findByRole("button", { name: /register application/i }));
  const dialog = within(await screen.findByRole("dialog"));
  await userEvent.selectOptions(dialog.getByLabelText("Type"), "saml");
  return dialog;
}

describe("registering a SAML application", () => {
  it("sends the metadata and nothing OIDC", async () => {
    const calls = stub([]);
    const dialog = await openRegister();

    await userEvent.type(dialog.getByLabelText("Name"), "Legacy payroll");
    await userEvent.type(dialog.getByLabelText("Metadata document"), "<md:EntityDescriptor/>");
    await userEvent.click(dialog.getByRole("button", { name: "Register" }));

    await vi.waitFor(() => expect(calls).toHaveLength(1));
    const body = calls[0].body as Record<string, unknown>;
    expect(body.type).toBe("saml");
    // The service refuses a SAML application carrying redirect URIs, so the
    // console must never build one.
    expect(body).not.toHaveProperty("redirect_uris");
    const saml = body.saml as Record<string, unknown>;
    expect(saml.metadata_xml).toBe("<md:EntityDescriptor/>");
    // Metadata OR fields: the half not in view is never sent.
    expect(saml).not.toHaveProperty("entity_id");
    expect(saml).not.toHaveProperty("acs_url");
  });

  it("sends the fields, and not the metadata, when the details are entered", async () => {
    const calls = stub([]);
    const dialog = await openRegister();

    await userEvent.type(dialog.getByLabelText("Name"), "Manual");
    await userEvent.click(dialog.getByLabelText("Enter the details"));
    await userEvent.type(dialog.getByLabelText("Entity ID"), "https://manual.example.test");
    await userEvent.type(
      dialog.getByLabelText("Assertion Consumer Service URL"),
      "https://manual.example.test/acs",
    );
    await userEvent.click(dialog.getByLabelText("Email address"));
    await userEvent.click(dialog.getByRole("button", { name: "Register" }));

    await vi.waitFor(() => expect(calls).toHaveLength(1));
    const saml = (calls[0].body as Record<string, unknown>).saml as Record<string, unknown>;
    expect(saml.entity_id).toBe("https://manual.example.test");
    expect(saml.acs_url).toBe("https://manual.example.test/acs");
    expect(saml.attribute_release).toEqual(["email"]);
    expect(saml).not.toHaveProperty("metadata_xml");
    // Off unless somebody ticked it.
    expect(saml.allow_idp_initiated).toBe(false);
  });

  it("will not register until the service provider is described", async () => {
    stub([]);
    const dialog = await openRegister();

    await userEvent.type(dialog.getByLabelText("Name"), "Empty");
    expect(dialog.getByRole("button", { name: "Register" })).toBeDisabled();
  });

  it("says what each switch costs, beside the switch", async () => {
    stub([]);
    const dialog = await openRegister();

    await userEvent.click(dialog.getByLabelText("Enter the details"));
    const signed = dialog.getByLabelText("Require its sign-in requests to be signed");
    expect(signed).toHaveAccessibleDescription(/HTTP-Redirect is refused/);
    const idp = dialog.getByLabelText("Allow sign-on started from this service");
    expect(idp).toHaveAccessibleDescription(/cannot tell such a sign-in apart/);
  });

  it("shows the server's reason when it refuses the document", async () => {
    stub([], () => ({
      status: 400,
      body: {
        error: {
          code: "VALIDATION_ERROR",
          message: "That metadata document could not be used.",
          details: [
            {
              field: "saml.metadata_xml",
              issue: "the metadata advertises no HTTPS HTTP-POST AssertionConsumerService",
            },
          ],
        },
      },
    }));
    const dialog = await openRegister();

    await userEvent.type(dialog.getByLabelText("Name"), "Broken");
    await userEvent.type(dialog.getByLabelText("Metadata document"), "<x/>");
    await userEvent.click(dialog.getByRole("button", { name: "Register" }));

    expect(await dialog.findByRole("alert")).toHaveTextContent(
      "the metadata advertises no HTTPS HTTP-POST AssertionConsumerService",
    );
    // The choice survives the refusal (docs/FRONTEND/07).
    expect(dialog.getByLabelText("Metadata document")).toHaveValue("<x/>");
  });
});

describe("the list", () => {
  it("shows where a SAML sign-in ends and the service provider's name", async () => {
    stub([samlApp]);

    renderScreen(<ApplicationsPage />, "/projects/p1", "/projects/:projectId");

    const row = (await screen.findByText("Legacy payroll")).closest("tr") as HTMLElement;
    expect(within(row).getByText("https://payroll.example.test/acs")).toBeInTheDocument();
    expect(within(row).getByText("https://payroll.example.test")).toBeInTheDocument();
    expect(within(row).getByText("None (SAML)")).toBeInTheDocument();
    expect(within(row).getByText(/Certificate valid until/)).toBeInTheDocument();
  });

  it("warns about a certificate the service says is expiring, in words", async () => {
    stub([{ ...samlApp, saml: { ...samlApp.saml, certificate_expires_soon: true } }]);

    renderScreen(<ApplicationsPage />, "/projects/p1", "/projects/:projectId");

    const row = (await screen.findByText("Legacy payroll")).closest("tr") as HTMLElement;
    expect(within(row).getByText(/ask for a new one/)).toBeInTheDocument();
  });

  it("has no accessibility violations with a SAML application listed", async () => {
    stub([samlApp]);
    const { container } = renderScreen(<ApplicationsPage />, "/projects/p1", "/projects/:projectId");
    await screen.findByText("Legacy payroll");
    await expectNoAxeViolations(container);
  });
});

describe("changing the settings", () => {
  it("sends the whole registration with the switch that changed", async () => {
    const calls = stub([samlApp], () => ({ status: 200, body: samlApp }));

    renderScreen(<ApplicationsPage />, "/projects/p1", "/projects/:projectId");
    const row = (await screen.findByText("Legacy payroll")).closest("tr") as HTMLElement;
    await userEvent.click(within(row).getByRole("button", { name: "SAML settings" }));
    const dialog = within(await screen.findByRole("dialog"));

    await userEvent.click(dialog.getByLabelText("Allow sign-on started from this service"));
    await userEvent.click(dialog.getByRole("button", { name: "Save" }));

    await vi.waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0].method).toBe("PATCH");
    const saml = (calls[0].body as Record<string, unknown>).saml as Record<string, unknown>;
    expect(saml.allow_idp_initiated).toBe(true);
    // Whole, because the service replaces it whole: a body carrying only the
    // switch would clear everything else.
    expect(saml.entity_id).toBe("https://payroll.example.test");
    expect(saml.acs_url).toBe("https://payroll.example.test/acs");
    expect(saml.attribute_release).toEqual(["email"]);
  });
});
