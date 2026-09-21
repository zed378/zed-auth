import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { GrantedProjectsPage } from "./GrantedProjectsPage";
import { AuthError } from "../lib/auth/oidc";
import { clearToken } from "../lib/auth/tokens";
import { expectNoAxeViolations } from "../test/axe";
import { renderScreen, signIn } from "../test/harness";

/**
 * Granted Projects — the receiving side (P4-06).
 *
 * The failures worth catching are the ones a plausible screen commits: a role
 * the grant does not delegate offered anyway, a delegated role that looks like
 * one of this organization's own, and a revoked grant that either disappears or
 * fails on the next click without saying why.
 */

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return {
    ...actual,
    renewSilently: () => Promise.reject(new AuthError("login_required", null)),
  };
});

const VENDOR = "3f2b8c1e-5d4a-4f7b-9c2e-1a6d8e0b7c45";

const active = {
  id: "g1",
  project_id: "p1",
  project_name: "Till",
  granting_org_id: VENDOR,
  granting_org_name: "Acme Vendor",
  granted_role_keys: ["cashier", "manager"],
  holder_count: 0,
  status: "active",
  created_at: "2026-09-10T00:00:00Z",
  revoked_at: null,
};

const ended = {
  ...active,
  id: "g2",
  project_name: "Billing",
  granting_org_name: "Charlie Former",
  holder_count: 1,
  status: "revoked",
  revoked_at: "2026-09-12T00:00:00Z",
};

const users = [
  { id: "u1", email: "budi@bravo.test", display_name: "Budi Santoso", status: "active" },
  { id: "u2", email: "sari@bravo.test", display_name: null, status: "active" },
];

type Call = { url: string; method: string; body?: unknown };

/**
 * Answers received grants, their assignments and this organization's users;
 * records every write.
 *
 * Stubbed at `fetch` so the generated client and the envelope handling stay in
 * the path, and so the assertions can be about the BODY the console sends — a
 * role nobody ticked is exactly the bug this screen could have.
 */
function stub(
  grants: unknown[],
  assignments: unknown[] = [],
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
    } else if (url.includes("/user-grants")) {
      answer = { status: 200, body: { grants: assignments } };
    } else if (url.includes("/project-grants")) {
      answer = { status: 200, body: { grants } };
    } else {
      answer = { status: 200, body: { users } };
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

async function openPanel(grantName = "Till") {
  renderScreen(<GrantedProjectsPage />, "/granted-projects");
  const row = (await screen.findByText(grantName)).closest("tr") as HTMLElement;
  await userEvent.click(within(row).getByRole("button", { name: /assign roles/i }));
  return within(await screen.findByRole("dialog"));
}

describe("the list", () => {
  it("names the project and the organization that lent it", async () => {
    stub([active]);

    renderScreen(<GrantedProjectsPage />, "/granted-projects");

    const row = (await screen.findByText("Till")).closest("tr") as HTMLElement;
    // The names are the whole point of the migration behind this screen: the
    // project and the organization are the granting side's rows.
    expect(within(row).getByText("Acme Vendor")).toBeInTheDocument();
    expect(within(row).getByText(VENDOR)).toBeInTheDocument();
    expect(within(row).getByText("cashier")).toBeInTheDocument();
    expect(within(row).getByText("manager")).toBeInTheDocument();
    expect(within(row).getByText("Active")).toBeInTheDocument();
    expect(within(row).getByText("Nobody")).toBeInTheDocument();
  });

  it("marks every role as delegated, naming the organization it came from", async () => {
    stub([active]);

    renderScreen(<GrantedProjectsPage />, "/granted-projects");

    const row = (await screen.findByText("Till")).closest("tr") as HTMLElement;
    // Not the tooltip alone: hover is an affordance for a mouse and nothing
    // else (`docs/UI-UX/13`).
    expect(within(row).getAllByText(/delegated from Acme Vendor/i).length).toBe(2);
  });

  it("keeps an ended grant listed, says who ended it, and offers no assignment", async () => {
    stub([ended]);

    renderScreen(<GrantedProjectsPage />, "/granted-projects");

    const row = (await screen.findByText("Billing")).closest("tr") as HTMLElement;
    expect(within(row).getByText("Ended")).toBeInTheDocument();
    expect(within(row).getByText(/Charlie Former ended this/)).toBeInTheDocument();
    expect(within(row).queryByRole("button", { name: /assign roles/i })).not.toBeInTheDocument();
  });

  it("explains what a granted project is when there are none", async () => {
    stub([]);

    renderScreen(<GrantedProjectsPage />, "/granted-projects");

    // An empty list here is not something this organization can act on, so the
    // empty state says who can rather than offering a dead "create" button.
    expect(
      await screen.findByText(/Only another organization can grant a project to yours/),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /create/i })).not.toBeInTheDocument();
  });

  it("has no accessibility violations", async () => {
    stub([active, ended]);

    const { container } = renderScreen(<GrantedProjectsPage />, "/granted-projects");
    await screen.findByText("Till");

    await expectNoAxeViolations(container);
  });
});

describe("assigning", () => {
  it("offers the delegated roles and NOTHING else", async () => {
    stub([active]);

    const panel = await openPanel();

    expect(panel.getByRole("checkbox", { name: /cashier/ })).toBeInTheDocument();
    expect(panel.getByRole("checkbox", { name: /manager/ })).toBeInTheDocument();
    // `docs/UI-UX/08`: a role the grant does not delegate is not rendered at
    // all — not disabled, not greyed. Absent from the DOM.
    expect(panel.queryByText("owner")).not.toBeInTheDocument();
    expect(panel.queryByText("admin")).not.toBeInTheDocument();
    expect(panel.getAllByRole("checkbox")).toHaveLength(2);
  });

  it("never asks for the granting project's roles at all", async () => {
    // The strongest form of F-5: the screen cannot render a role the grant
    // withholds because it never learns one exists. `granted_role_keys` is its
    // only source, and the roles endpoint belongs to the granting organization
    // anyway — a request for it would 404 in production and read as a bug here.
    const requested: string[] = [];
    vi.stubGlobal("fetch", async (input: RequestInfo | URL) => {
      const request = input as Request;
      requested.push(request.url);
      const body = request.url.includes("/user-grants")
        ? { grants: [] }
        : request.url.includes("/project-grants")
          ? { grants: [active] }
          : { users };
      return new Response(JSON.stringify(body), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });

    await openPanel();

    expect(requested.some((url) => /\/roles(\?|$)/.test(url))).toBe(false);
  });

  it("lists only this organization's own people", async () => {
    stub([active]);

    const panel = await openPanel();

    const select = panel.getByLabelText(/person/i);
    expect(within(select).getByRole("option", { name: /Budi Santoso/ })).toBeInTheDocument();
    // No display name: the address, not a blank row.
    expect(within(select).getByRole("option", { name: /sari@bravo.test/ })).toBeInTheDocument();
  });

  it("sends exactly the person and the roles that were ticked", async () => {
    const calls = stub([active], [], () => ({ status: 201, body: {} }));

    const panel = await openPanel();
    await userEvent.selectOptions(panel.getByLabelText(/person/i), "u1");
    await userEvent.click(panel.getByRole("checkbox", { name: /cashier/ }));
    await userEvent.click(panel.getByRole("button", { name: /^assign roles$/i }));

    const post = calls.find((call) => call.method === "POST");
    expect(post?.url).toContain("/project-grants/g1/user-grants");
    expect(post?.body).toEqual({ user_id: "u1", role_keys: ["cashier"] });
  });

  it("cannot be submitted with no person or no role", async () => {
    stub([active]);

    const panel = await openPanel();
    const submit = panel.getByRole("button", { name: /^assign roles$/i });
    expect(submit).toBeDisabled();

    await userEvent.selectOptions(panel.getByLabelText(/person/i), "u1");
    expect(submit).toBeDisabled();

    await userEvent.click(panel.getByRole("checkbox", { name: /cashier/ }));
    expect(submit).toBeEnabled();
  });

  it("says what the server refused, rather than failing silently", async () => {
    stub([active], [], () => ({
      status: 409,
      body: { error: { code: "CONFLICT", message: "This grant has been revoked." } },
    }));

    const panel = await openPanel();
    await userEvent.selectOptions(panel.getByLabelText(/person/i), "u1");
    await userEvent.click(panel.getByRole("checkbox", { name: /cashier/ }));
    await userEvent.click(panel.getByRole("button", { name: /^assign roles$/i }));

    expect(await panel.findByRole("alert")).toHaveTextContent("This grant has been revoked.");
  });
});

describe("what is already assigned", () => {
  const assignment = {
    user_id: "u1",
    project_id: "p1",
    project_grant_id: "g1",
    role_keys: ["cashier"],
    created_at: "2026-09-11T00:00:00Z",
  };

  it("names the holder and marks the role as delegated", async () => {
    stub([active], [assignment]);

    const panel = await openPanel();

    expect(await panel.findByText("budi@bravo.test")).toBeInTheDocument();
    expect(panel.getAllByText(/delegated from Acme Vendor/i).length).toBeGreaterThan(0);
  });

  it("states the consequence before removing", async () => {
    const calls = stub([active], [assignment]);

    const panel = await openPanel();
    await userEvent.click(await panel.findByRole("button", { name: /remove/i }));

    // Two dialogs are open: the panel underneath and the confirmation over it.
    // The confirmation is the last one mounted.
    const dialogs = await screen.findAllByRole("dialog");
    const confirm = within(dialogs[dialogs.length - 1]);
    expect(confirm.getByText(/budi@bravo.test/)).toBeInTheDocument();
    expect(confirm.getByText(/Acme Vendor/)).toBeInTheDocument();
    await userEvent.click(confirm.getByRole("button", { name: /remove roles/i }));

    const call = calls.find((c) => c.method === "DELETE");
    expect(call?.url).toContain("/project-grants/g1/user-grants/u1");
  });

  it("shows an ended grant's leftovers as a state, with no way to add more", async () => {
    stub([ended], [{ ...assignment, project_grant_id: "g2" }]);

    renderScreen(<GrantedProjectsPage />, "/granted-projects");
    const row = (await screen.findByText("Billing")).closest("tr") as HTMLElement;
    await userEvent.click(within(row).getByRole("button", { name: /review/i }));

    const panel = within(await screen.findByRole("dialog"));
    // F-7: the revocation is a state the screen explains, not an opaque
    // failure on the next action.
    expect(panel.getByRole("status")).toHaveTextContent(/Charlie Former ended this grant/);
    expect(panel.queryByRole("checkbox")).not.toBeInTheDocument();
    expect(panel.queryByLabelText(/person/i)).not.toBeInTheDocument();
    // The leftovers are still there to clear.
    expect(await panel.findByRole("button", { name: /remove/i })).toBeInTheDocument();
  });
});
