import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { AuthorizationsPage } from "./AuthorizationsPage";
import { UserDetailPage } from "./UserDetailPage";
import { RoleSourceBadge } from "../components/RoleSourceBadge";
import { AuthError } from "../lib/auth/oidc";
import { clearToken } from "../lib/auth/tokens";
import { expectNoAxeViolations } from "../test/axe";
import { renderScreen, signIn, stubApi } from "../test/harness";

/**
 * The Authorizations tab and the User Grants tab (P2-12).
 *
 * Both screens show roles, so both are subject to `docs/UI-UX/08`'s
 * cross-screen rule that the role-source badge is not optional per screen —
 * which is why the badge is tested directly as well as through each screen.
 */

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return {
    ...actual,
    renewSilently: () => Promise.reject(new AuthError("login_required", null)),
  };
});

const PATH = "/projects/p1/authorizations";
const ROUTE = "/projects/:projectId/authorizations";

const budi = { id: "u1", email: "budi@example.test", status: "active", display_name: "Budi" };

const cashier = {
  id: "r1",
  project_id: "p1",
  key: "cashier",
  display_name: "Cashier",
  permission_keys: ["sale:create", "sale:read"],
  is_builtin: false,
  grant_count: 1,
};

const auditor = {
  ...cashier,
  id: "r2",
  key: "auditor",
  display_name: "Auditor",
  permission_keys: ["sale:read"],
  grant_count: 0,
};

/**
 * One stub for the whole screen: projects, roles, users and grants.
 *
 * `grants` is what each test varies — it is the thing the screen is about.
 */
function stubAll(options: {
  users?: unknown[];
  grants?: unknown[];
  roles?: unknown[];
  onWrite?: (method: string, url: string) => { status: number; body: unknown } | undefined;
}) {
  const { users = [budi], grants = [], roles = [cashier, auditor], onWrite } = options;

  stubApi((url, init) => {
    if (init.method !== "GET" && onWrite !== undefined) {
      const answer = onWrite(init.method, url);
      if (answer !== undefined) return answer;
    }
    if (url.includes("/grants")) return { status: 200, body: { grants } };
    if (url.includes("/roles")) return { status: 200, body: { roles } };
    if (url.includes("/users")) return { status: 200, body: { users } };
    return { status: 200, body: { projects: [{ id: "p1", name: "Till" }] } };
  });
}

/** Types into the search box and picks the first result. */
async function pick(email = budi.email) {
  await userEvent.type(screen.getByLabelText(/find a user/i), "budi");
  await userEvent.click(await screen.findByRole("button", { name: new RegExp(email, "i") }));
}

beforeEach(() => {
  clearToken();
  sessionStorage.clear();
  vi.stubEnv("VITE_AUTH_CLIENT_ID", "console-test-client");
  vi.stubEnv("VITE_AUTH_ISSUER", "https://auth.example.test");
  signIn();
});

describe("the role-source badge", () => {
  it("renders a direct role as a plain badge with no origin claim", () => {
    const { container } = renderScreen(<RoleSourceBadge roleKey="cashier" />);

    expect(screen.getByText("cashier")).toBeInTheDocument();
    expect(container.querySelector("svg")).toBeNull();
    expect(screen.queryByText(/delegated/i)).not.toBeInTheDocument();
  });

  it("names the source organization in text, not only on hover", () => {
    // `docs/UI-UX/06` asks for the origin "on hover/tap". A tooltip is an
    // affordance for a mouse and nothing else, so the same sentence is in the
    // accessibility tree. Phase 4 turns this branch on; it is built and tested
    // now so that turning it on is not an audit of every screen.
    renderScreen(<RoleSourceBadge roleKey="cashier" delegatedFrom="Acme Corp" />);

    expect(screen.getByText(/delegated from Acme Corp/i)).toBeInTheDocument();
    expect(screen.getByTitle("Delegated from Acme Corp")).toBeInTheDocument();
  });
});

describe("finding a user", () => {
  it("waits for typing to settle before asking the server", async () => {
    const calls: string[] = [];
    stubApi((url) => {
      if (url.includes("/users")) calls.push(url);
      if (url.includes("/grants")) return { status: 200, body: { grants: [] } };
      if (url.includes("/roles")) return { status: 200, body: { roles: [] } };
      if (url.includes("/users")) return { status: 200, body: { users: [budi] } };
      return { status: 200, body: { projects: [{ id: "p1", name: "Till" }] } };
    });

    renderScreen(<AuthorizationsPage />, PATH, ROUTE);
    await screen.findByLabelText(/find a user/i);

    const before = calls.length;
    await userEvent.type(screen.getByLabelText(/find a user/i), "budi");
    await screen.findByRole("button", { name: /budi@example.test/i });

    // Four characters, not four searches. Without the debounce each keystroke
    // is a request, and they can come back out of order — so the list settles
    // on the results for a prefix of what was typed.
    expect(calls.length - before).toBeLessThanOrEqual(2);
  });

  it("keeps the input ahead of the request", async () => {
    stubAll({});

    renderScreen(<AuthorizationsPage />, PATH, ROUTE);
    const input = await screen.findByLabelText(/find a user/i);
    await userEvent.type(input, "budi");

    // The field is controlled by the raw value. Debouncing the input itself
    // would make typing feel broken.
    expect(input).toHaveValue("budi");
  });

  it("says nobody matched rather than showing an empty list", async () => {
    stubAll({ users: [] });

    renderScreen(<AuthorizationsPage />, PATH, ROUTE);
    await userEvent.type(await screen.findByLabelText(/find a user/i), "nobody");

    expect(await screen.findByText(/Nobody in this organization matches/i)).toBeInTheDocument();
  });

  it("invites a search rather than claiming there is nothing", async () => {
    stubAll({ users: [] });

    renderScreen(<AuthorizationsPage />, PATH, ROUTE);

    // An untouched search box is not an empty state — it is a screen waiting
    // to be used, and "no users" would be a false statement about the
    // organization.
    expect(await screen.findByText(/Type an email address or name above/i)).toBeInTheDocument();
  });
});

describe("a user's access to the project", () => {
  it("states no access explicitly rather than leaving a blank", async () => {
    stubAll({ grants: [] });

    renderScreen(<AuthorizationsPage />, PATH, ROUTE);
    await pick();

    // Step 6 and `docs/PLAN/08` § Least Privilege: no grant is no access, not
    // a default. A blank would be ambiguous with "we did not load it".
    expect(await screen.findByText(/No access\./i)).toBeInTheDocument();
    expect(screen.getByText(/no implicit or default role/i)).toBeInTheDocument();
  });

  it("names the roles rather than counting them", async () => {
    stubAll({ grants: [{ user_id: "u1", project_id: "p1", role_keys: ["cashier", "auditor"] }] });

    renderScreen(<AuthorizationsPage />, PATH, ROUTE);
    await pick();

    // `docs/UI-UX/18`: never "2 roles" — knowing exactly which is the point.
    expect(await screen.findByText("cashier")).toBeInTheDocument();
    expect(screen.getByText("auditor")).toBeInTheDocument();
    expect(screen.queryByText(/2 roles/i)).not.toBeInTheDocument();
  });

  it("shows only this project's grant, not every project's", async () => {
    stubAll({
      // The OTHER project first, deliberately. With this project's grant first
      // a filter that ignored the project id would still pick the right row by
      // accident, and the test would pass against a screen that shows whatever
      // grant happened to come back first. Caught by the mutation run.
      grants: [
        { user_id: "u1", project_id: "p2", role_keys: ["elsewhere-only"] },
        { user_id: "u1", project_id: "p1", role_keys: ["cashier"] },
      ],
    });

    renderScreen(<AuthorizationsPage />, PATH, ROUTE);
    await pick();

    expect(await screen.findByText("cashier")).toBeInTheDocument();
    expect(screen.queryByText("elsewhere-only")).not.toBeInTheDocument();
  });
});

describe("granting roles", () => {
  it("shows what each role carries, not just its name", async () => {
    stubAll({ grants: [] });

    renderScreen(<AuthorizationsPage />, PATH, ROUTE);
    await pick();
    await userEvent.click(await screen.findByRole("button", { name: /grant access/i }));

    const dialog = within(screen.getByRole("dialog"));
    // Step 3: the permission keys are visible at assignment time. A list of
    // role names is a list of words somebody has to already know.
    expect(dialog.getByText("sale:create, sale:read")).toBeInTheDocument();
    expect(dialog.getByText("sale:read")).toBeInTheDocument();
  });

  it("reflects the existing grant, so a change is a change rather than a reset", async () => {
    stubApi((url, init) => {
      if (url.includes("/grants") && init.method === "PATCH") {
        return { status: 200, body: { user_id: "u1", project_id: "p1", role_keys: [] } };
      }
      if (url.includes("/grants")) {
        return {
          status: 200,
          body: { grants: [{ user_id: "u1", project_id: "p1", role_keys: ["cashier"] }] },
        };
      }
      if (url.includes("/roles")) return { status: 200, body: { roles: [cashier, auditor] } };
      if (url.includes("/users")) return { status: 200, body: { users: [budi] } };
      return { status: 200, body: { projects: [{ id: "p1", name: "Till" }] } };
    });

    renderScreen(<AuthorizationsPage />, PATH, ROUTE);
    await pick();
    await userEvent.click(await screen.findByRole("button", { name: /change roles/i }));

    const dialog = within(screen.getByRole("dialog"));
    // The existing grant is reflected, so an admin is changing a set rather
    // than starting from nothing and silently dropping what was there.
    expect(dialog.getByRole("checkbox", { name: /cashier/i })).toBeChecked();
    expect(dialog.getByRole("checkbox", { name: /auditor/i })).not.toBeChecked();
  });

  it("explains that deselecting everything is not how access is removed", async () => {
    stubAll({ grants: [{ user_id: "u1", project_id: "p1", role_keys: ["cashier"] }] });

    renderScreen(<AuthorizationsPage />, PATH, ROUTE);
    await pick();
    await userEvent.click(await screen.findByRole("button", { name: /change roles/i }));

    const dialog = within(screen.getByRole("dialog"));
    await userEvent.click(dialog.getByRole("checkbox", { name: /cashier/i }));

    // The API refuses an empty set — "a grant with no roles grants nothing and
    // should not exist". Saying so beats a refusal the admin has to interpret.
    expect(dialog.getByText(/not the way to remove access/i)).toBeInTheDocument();
  });

  it("points at the Roles tab when the project has no roles to grant", async () => {
    stubAll({ grants: [], roles: [] });

    renderScreen(<AuthorizationsPage />, PATH, ROUTE);
    await pick();
    await userEvent.click(await screen.findByRole("button", { name: /grant access/i }));

    const dialog = within(screen.getByRole("dialog"));
    expect(dialog.getByText(/no roles yet, so there is nothing to grant/i)).toBeInTheDocument();
    expect(dialog.getByRole("link", { name: /roles tab/i })).toBeInTheDocument();
  });

  it("surfaces the server's refusal instead of closing over it", async () => {
    stubAll({
      grants: [],
      onWrite: (method) =>
        method === "POST"
          ? {
              status: 403,
              body: {
                error: {
                  code: "PERMISSION_DENIED",
                  message: "You cannot grant roles to yourself.",
                },
              },
            }
          : undefined,
    });

    renderScreen(<AuthorizationsPage />, PATH, ROUTE);
    await pick();
    await userEvent.click(await screen.findByRole("button", { name: /grant access/i }));

    const dialog = within(screen.getByRole("dialog"));
    await userEvent.click(dialog.getByRole("checkbox", { name: /cashier/i }));
    await userEvent.click(dialog.getByRole("button", { name: /save roles/i }));

    expect(await screen.findByRole("alert")).toHaveTextContent(/cannot grant roles to yourself/i);
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });
});

describe("revoking access", () => {
  it("says it takes effect immediately, and what it does not reach", async () => {
    stubAll({ grants: [{ user_id: "u1", project_id: "p1", role_keys: ["cashier"] }] });

    renderScreen(<AuthorizationsPage />, PATH, ROUTE);
    await pick();
    await userEvent.click(await screen.findByRole("button", { name: /revoke all access/i }));

    const dialog = screen.getByRole("dialog");
    // Step 5, both halves. The second is the one people get wrong: an access
    // token already minted keeps its claims, which is exactly why real-time
    // checks exist.
    expect(dialog).toHaveTextContent(/takes effect immediately/i);
    expect(dialog).toHaveTextContent(/keeps the roles it was minted with until it expires/i);
    expect(dialog).toHaveTextContent(/access to other projects is unaffected/i);
    expect(screen.getByRole("button", { name: "Revoke access" })).toBeInTheDocument();
  });

  it("offers no revoke control for a user who has no access to revoke", async () => {
    stubAll({ grants: [] });

    renderScreen(<AuthorizationsPage />, PATH, ROUTE);
    await pick();

    await screen.findByText(/No access\./i);
    expect(screen.queryByRole("button", { name: /revoke/i })).not.toBeInTheDocument();
  });
});

describe("the user detail Grants tab", () => {
  const user = {
    id: "u1",
    email: "budi@example.test",
    display_name: "Budi",
    status: "active",
    email_verified: true,
    created_at: "2026-09-01T00:00:00Z",
  };

  it("lists every project and names the roles, with the source badge", async () => {
    stubApi((url) => {
      if (url.includes("/grants")) {
        return {
          status: 200,
          body: { grants: [{ user_id: "u1", project_id: "p1", role_keys: ["cashier"] }] },
        };
      }
      if (url.includes("/projects")) {
        return { status: 200, body: { projects: [{ id: "p1", name: "Till" }] } };
      }
      return { status: 200, body: user };
    });

    renderScreen(<UserDetailPage />, "/users/u1", "/users/:userId");
    await userEvent.click(await screen.findByRole("tab", { name: /grants/i }));

    // The project's NAME, not its id — the id is not something an
    // administrator recognises.
    expect(await screen.findByRole("link", { name: "Till" })).toBeInTheDocument();
    expect(screen.getByText("cashier")).toBeInTheDocument();
  });

  it("says what no grants means, not just that there are none", async () => {
    stubApi((url) =>
      url.includes("/grants")
        ? { status: 200, body: { grants: [] } }
        : url.includes("/projects")
          ? { status: 200, body: { projects: [] } }
          : { status: 200, body: user },
    );

    renderScreen(<UserDetailPage />, "/users/u1", "/users/:userId");
    await userEvent.click(await screen.findByRole("tab", { name: /grants/i }));

    expect(await screen.findByText(/No grants yet/i)).toBeInTheDocument();
    expect(screen.getByText(/no implicit or default role/i)).toBeInTheDocument();
  });
});

describe("permission and accessibility", () => {
  it("hides the granting controls from a caller whose token does not carry the role", async () => {
    clearToken();
    signIn(["MEMBER"]);
    stubAll({ grants: [{ user_id: "u1", project_id: "p1", role_keys: ["cashier"] }] });

    renderScreen(<AuthorizationsPage />, PATH, ROUTE);
    await pick();
    await screen.findByText("cashier");

    // UX only — the API refuses independently on every request.
    expect(screen.queryByRole("button", { name: /change roles/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /revoke/i })).not.toBeInTheDocument();
  });

  it("has no axe violations, search results and assignment dialog both", async () => {
    stubAll({ grants: [] });

    const { container } = renderScreen(<AuthorizationsPage />, PATH, ROUTE);
    await screen.findByLabelText(/find a user/i);
    await expectNoAxeViolations(container);

    await pick();
    await userEvent.click(await screen.findByRole("button", { name: /grant access/i }));
    await expectNoAxeViolations(container);
  });
});
