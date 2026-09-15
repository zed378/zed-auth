import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ProjectGrantsPage } from "./ProjectGrantsPage";
import { AuthError } from "../lib/auth/oidc";
import { clearToken, storeToken } from "../lib/auth/tokens";
import { expectNoAxeViolations } from "../test/axe";
import { renderScreen, signIn, stubApi } from "../test/harness";

/**
 * The Project Grants tab (P4-05).
 *
 * The behaviours that matter most are the ones where a plausible screen
 * misleads: a grant whose withheld roles are invisible, a "no roles" state
 * coloured as though something were destroyed, and a revocation that does not
 * say how many people it affects or that can be confirmed without reading.
 */

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return {
    ...actual,
    renewSilently: () => Promise.reject(new AuthError("login_required", null)),
  };
});

const PATH = "/projects/p1/grants";
const ROUTE = "/projects/:projectId/grants";
const PARTNER = "3f2b8c1e-5d4a-4f7b-9c2e-1a6d8e0b7c45";

const roles = [
  { id: "r1", key: "cashier", display_name: "Cashier" },
  { id: "r2", key: "manager", display_name: "Manager" },
  { id: "r3", key: "owner", display_name: "Owner" },
].map((role) => ({
  ...role,
  project_id: "p1",
  permission_keys: [],
  is_builtin: false,
  grant_count: 0,
  created_at: "2026-09-01T00:00:00Z",
  updated_at: "2026-09-01T00:00:00Z",
}));

const active = {
  id: "g1",
  project_id: "p1",
  granting_org_id: "org-1",
  granted_org_id: PARTNER,
  granted_org_name: "Bravo Client",
  granted_role_keys: ["cashier"],
  holder_count: 0,
  status: "active",
  created_at: "2026-09-10T00:00:00Z",
  revoked_at: null,
};

const held = { ...active, id: "g2", holder_count: 3 };

const revoked = {
  ...active,
  id: "g3",
  granted_org_name: "Charlie Former",
  status: "revoked",
  revoked_at: "2026-09-12T00:00:00Z",
};

type Call = { url: string; method: string; body?: unknown };

/**
 * Answers grants, roles and projects; records every non-GET call.
 *
 * `fetch` is re-stubbed here rather than through `stubApi` for writes, because
 * the assertions below are about the BODY the console sends — a grant created
 * with a role nobody ticked is the failure worth catching.
 */
function stub(
  grants: unknown[],
  write: (call: Call) => { status: number; body: unknown } = () => ({ status: 204, body: null }),
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
    } else if (url.includes("/grants")) {
      answer = { status: 200, body: { grants } };
    } else if (url.includes("/roles")) {
      answer = { status: 200, body: { roles } };
    } else {
      answer = { status: 200, body: { projects: [{ id: "p1", name: "Till" }] } };
    }
    return new Response(answer.status === 204 ? null : JSON.stringify(answer.body), {
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

async function openCreate() {
  renderScreen(<ProjectGrantsPage />, PATH, ROUTE);
  await screen.findByText("Bravo Client").catch(() => undefined);
  await userEvent.click(await screen.findByRole("button", { name: /^create grant$/i }));
  const dialog = screen.getByRole("dialog");
  await within(dialog).findByLabelText(/cashier/);
  return within(dialog);
}

describe("the grants table", () => {
  it("names the partner, the shared roles, and the roles NOT shared", async () => {
    stub([active]);

    renderScreen(<ProjectGrantsPage />, PATH, ROUTE);

    const row = (await screen.findByText("Bravo Client")).closest("tr") as HTMLElement;
    expect(within(row).getByText(PARTNER)).toBeInTheDocument();
    expect(within(row).getByText("cashier")).toBeInTheDocument();
    // What is withheld is as visible as what is shared (step 3).
    expect(await within(row).findByText("Not shared: manager, owner")).toBeInTheDocument();
    expect(within(row).getByText("Active")).toBeInTheDocument();
    expect(within(row).getByText("Nobody")).toBeInTheDocument();
  });

  it("keeps a revoked grant listed, ended, with no revoke control", async () => {
    stub([revoked]);

    renderScreen(<ProjectGrantsPage />, PATH, ROUTE);

    const row = (await screen.findByText("Charlie Former")).closest("tr") as HTMLElement;
    expect(within(row).getByText("Revoked")).toBeInTheDocument();
    expect(within(row).getByText("Ended")).toBeInTheDocument();
    expect(within(row).queryByRole("button", { name: /revoke/i })).not.toBeInTheDocument();
  });

  it("offers the first grant when there are none", async () => {
    stub([]);

    renderScreen(<ProjectGrantsPage />, PATH, ROUTE);

    expect(await screen.findByText(/No Project Grants yet/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /create the first grant/i })).toBeInTheDocument();
  });

  it("renders a refusal without offering a retry", async () => {
    stubApi((url) =>
      url.includes("/grants")
        ? { status: 403, body: { error: { code: "PERMISSION_DENIED", message: "no" } } }
        : { status: 200, body: { projects: [], roles: [] } },
    );

    renderScreen(<ProjectGrantsPage />, PATH, ROUTE);

    expect(await screen.findByRole("alert")).toHaveTextContent(/do not have access/i);
    expect(screen.queryByRole("button", { name: /try again/i })).not.toBeInTheDocument();
  });
});

describe("creating a grant", () => {
  it("lists every project role as not shared until chosen, and warns — not danger — with none", async () => {
    stub([]);
    const form = await openCreate();

    for (const key of ["cashier", "manager", "owner"]) {
      expect(form.getByLabelText(new RegExp(key))).not.toBeChecked();
    }
    expect(form.getAllByText("Not shared")).toHaveLength(3);

    const warning = form.getByText("This Project Grant has no roles selected yet.");
    // `docs/UI-UX/05`'s example of a warning. Danger is for destruction.
    expect(warning).toHaveClass("text-warning");
    expect(warning).not.toHaveClass("text-danger");

    await userEvent.type(form.getByLabelText("Organization ID"), PARTNER);
    expect(form.getByRole("button", { name: "Review" })).toBeDisabled();
  });

  it("builds the plain-language summary live as roles are ticked", async () => {
    stub([]);
    const form = await openCreate();
    await userEvent.type(form.getByLabelText("Organization ID"), PARTNER);

    await userEvent.click(form.getByLabelText(/cashier/));
    expect(form.getByText(/will be able to give this role in Till to its own users/)).toHaveTextContent(
      "cashier",
    );

    await userEvent.click(form.getByLabelText(/manager/));
    expect(form.getByText(/will be able to give these roles/)).toHaveTextContent("cashier, manager");
    expect(form.queryByText("This Project Grant has no roles selected yet.")).not.toBeInTheDocument();
  });

  it("refuses something that is not an organization ID", async () => {
    stub([]);
    const form = await openCreate();

    await userEvent.type(form.getByLabelText("Organization ID"), "Bravo Client");
    await userEvent.click(form.getByLabelText(/cashier/));

    expect(form.getByLabelText("Organization ID")).toHaveAttribute("aria-invalid", "true");
    expect(form.getByRole("button", { name: "Review" })).toBeDisabled();
  });

  it("says so when the ID is this organization's own", async () => {
    const own = "11111111-2222-4333-8444-555555555555";
    clearToken();
    const payload = btoa(
      JSON.stringify({ sub: "admin-1", org_id: own, roles: ["ORG_ADMIN"], exp: 2_000_000_000 }),
    ).replace(/=+$/, "");
    storeToken(`header.${payload}.signature`, 3600);
    stub([]);
    const form = await openCreate();

    await userEvent.type(form.getByLabelText("Organization ID"), own);

    expect(form.getByText(/this organization's own ID/)).toBeInTheDocument();
  });

  it("confirms on a second step, then sends exactly the ticked roles", async () => {
    const calls = stub([], () => ({ status: 201, body: active }));
    const form = await openCreate();

    await userEvent.type(form.getByLabelText("Organization ID"), ` ${PARTNER.toUpperCase()} `);
    await userEvent.click(form.getByLabelText(/manager/));
    await userEvent.click(form.getByLabelText(/cashier/));
    await userEvent.click(form.getByRole("button", { name: "Review" }));

    // Flow 2's confirmation step: nothing is sent by Review.
    expect(calls).toHaveLength(0);
    const review = within(screen.getByRole("dialog"));
    expect(review.getByText("Confirm the grant")).toBeInTheDocument();
    expect(review.getByText(/Not shared: owner/)).toBeInTheDocument();

    await userEvent.click(review.getByRole("button", { name: "Create grant" }));

    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0].method).toBe("POST");
    expect(calls[0].body).toEqual({ granted_org_id: PARTNER, role_keys: ["cashier", "manager"] });
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("shows the server's refusal on the confirmation step and keeps the choices", async () => {
    stub([], () => ({
      status: 400,
      body: {
        error: {
          code: "VALIDATION_ERROR",
          message: "invalid",
          details: [
            {
              field: "granted_org_id",
              issue: "That organization cannot receive a grant of this project.",
            },
          ],
        },
      },
    }));
    const form = await openCreate();
    await userEvent.type(form.getByLabelText("Organization ID"), PARTNER);
    await userEvent.click(form.getByLabelText(/cashier/));
    await userEvent.click(form.getByRole("button", { name: "Review" }));
    await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Create grant" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(/cannot receive a grant/);

    await userEvent.click(screen.getByRole("button", { name: "Back" }));
    expect(screen.getByLabelText("Organization ID")).toHaveValue(PARTNER);
    expect(screen.getByLabelText(/cashier/)).toBeChecked();
  });
});

describe("revoking a grant", () => {
  it("says nobody is affected and asks for no typing when nobody is", async () => {
    const calls = stub([active]);

    renderScreen(<ProjectGrantsPage />, PATH, ROUTE);
    await userEvent.click(await screen.findByRole("button", { name: "Revoke" }));

    const dialog = within(screen.getByRole("dialog"));
    expect(dialog.getByText(/Nobody currently holds a role through this grant/)).toBeInTheDocument();
    expect(dialog.queryByLabelText(/to confirm/)).not.toBeInTheDocument();

    await userEvent.click(dialog.getByRole("button", { name: "Revoke grant" }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0].method).toBe("DELETE");
    expect(calls[0].url).toMatch(/\/projects\/p1\/grants\/g1$/);
  });

  it("states the count and requires the partner's exact name when people hold roles", async () => {
    const calls = stub([held]);

    renderScreen(<ProjectGrantsPage />, PATH, ROUTE);
    await userEvent.click(await screen.findByRole("button", { name: "Revoke" }));

    const dialog = within(screen.getByRole("dialog"));
    expect(dialog.getByText(/3 users in Bravo Client currently hold a role/)).toBeInTheDocument();

    const typed = dialog.getByLabelText(/to confirm/);
    // `docs/UI-UX/18`: the typed input takes focus when the dialog opens.
    expect(typed).toHaveFocus();

    const confirm = dialog.getByRole("button", { name: "Revoke grant" });
    expect(confirm).toBeDisabled();
    await userEvent.type(typed, "bravo client");
    expect(confirm).toBeDisabled();

    await userEvent.clear(typed);
    await userEvent.type(typed, "Bravo Client");
    expect(confirm).toBeEnabled();
    await userEvent.click(confirm);
    await waitFor(() => expect(calls).toHaveLength(1));
  });

  it("forgets what was typed when the dialog is cancelled", async () => {
    stub([held]);

    renderScreen(<ProjectGrantsPage />, PATH, ROUTE);
    await userEvent.click(await screen.findByRole("button", { name: "Revoke" }));
    await userEvent.type(screen.getByLabelText(/to confirm/), "Bravo Client");
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));

    await userEvent.click(screen.getByRole("button", { name: "Revoke" }));
    // Reopened already unlocked would spend the friction before the new
    // consequence was read.
    expect(screen.getByLabelText(/to confirm/)).toHaveValue("");
    expect(screen.getByRole("button", { name: "Revoke grant" })).toBeDisabled();
  });
});

describe("permission and accessibility", () => {
  it("hides the management controls from a caller whose token does not carry the role", async () => {
    clearToken();
    signIn(["MEMBER"]);
    stub([active]);

    renderScreen(<ProjectGrantsPage />, PATH, ROUTE);
    await screen.findByText("Bravo Client");

    expect(screen.queryByRole("button", { name: /create grant/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /revoke/i })).not.toBeInTheDocument();
  });

  it("has no axe violations: table, form, confirmation and revocation", async () => {
    stub([held, revoked]);

    const { container } = renderScreen(<ProjectGrantsPage />, PATH, ROUTE);
    await screen.findByText("Charlie Former");
    await expectNoAxeViolations(container);

    await userEvent.click(screen.getByRole("button", { name: /^create grant$/i }));
    const form = within(screen.getByRole("dialog"));
    await form.findByLabelText(/cashier/);
    await expectNoAxeViolations(document.body);

    await userEvent.type(form.getByLabelText("Organization ID"), PARTNER);
    await userEvent.click(form.getByLabelText(/cashier/));
    await userEvent.click(form.getByRole("button", { name: "Review" }));
    await expectNoAxeViolations(document.body);
    await userEvent.click(screen.getByRole("button", { name: "Back" }));
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));

    await userEvent.click(screen.getByRole("button", { name: "Revoke" }));
    await expectNoAxeViolations(document.body);
  });
});
