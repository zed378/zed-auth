import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { RolesPage } from "./RolesPage";
import { AuthError } from "../lib/auth/oidc";
import { clearToken } from "../lib/auth/tokens";
import { expectNoAxeViolations } from "../test/axe";
import { renderScreen, signIn, stubApi } from "../test/harness";

/**
 * The Roles tab (P2-11).
 *
 * Every test here is against a behaviour the task card names, and each one
 * fails if the corresponding control is removed — the mutation check for a
 * screen. The two that matter most are the ones about **shared validation**
 * and the **affected-grant count**, because both are cases where a plausible
 * implementation looks right and misleads the administrator.
 */

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return {
    ...actual,
    renewSilently: () => Promise.reject(new AuthError("login_required", null)),
  };
});

const PATH = "/projects/p1/roles";
const ROUTE = "/projects/:projectId/roles";

const cashier = {
  id: "r1",
  project_id: "p1",
  key: "cashier",
  display_name: "Cashier",
  permission_keys: ["sale:create", "sale:read"],
  is_builtin: false,
  grant_count: 3,
  created_at: "2026-09-01T00:00:00Z",
  updated_at: "2026-09-01T00:00:00Z",
};

const unused = { ...cashier, id: "r2", key: "auditor", display_name: "Auditor", grant_count: 0 };

const builtin = {
  ...cashier,
  id: "r3",
  key: "owner",
  display_name: "Project Owner",
  is_builtin: true,
  grant_count: 1,
};

/** Answers the roles list with the given roles, and projects with one project. */
function stubRoles(roles: unknown[]) {
  stubApi((url) =>
    url.includes("/roles")
      ? { status: 200, body: { roles } }
      : { status: 200, body: { projects: [{ id: "p1", name: "Till" }] } },
  );
}

beforeEach(() => {
  clearToken();
  sessionStorage.clear();
  vi.stubEnv("VITE_AUTH_CLIENT_ID", "console-test-client");
  vi.stubEnv("VITE_AUTH_ISSUER", "https://auth.example.test");
  signIn();
});

describe("the roles table", () => {
  it("shows the key, the name, a permission count and a grant count", async () => {
    stubRoles([cashier]);

    renderScreen(<RolesPage />, PATH, ROUTE);

    const row = (await screen.findByText("cashier")).closest("tr") as HTMLElement;
    expect(within(row).getByText("Cashier")).toBeInTheDocument();
    // Step 2: a count, not the raw array — but the keys stay reachable, so
    // the number is something an administrator can act on.
    expect(within(row).getByText("2")).toBeInTheDocument();
    expect(within(row).getByTitle("sale:create, sale:read")).toBeInTheDocument();
    expect(within(row).getByText(/sale:create, sale:read/)).toBeInTheDocument();
    expect(within(row).getByText("3 users")).toBeInTheDocument();
  });

  it("says 'Nobody' rather than 0 for a role nobody holds", async () => {
    stubRoles([unused]);

    renderScreen(<RolesPage />, PATH, ROUTE);

    // "0" in a column headed "Assigned to" reads as a missing value as
    // readily as an answer. The word cannot be misread.
    expect(await screen.findByText("Nobody")).toBeInTheDocument();
  });

  it("distinguishes no roles at all from none matching the search", async () => {
    stubRoles([]);

    const { unmount } = renderScreen(<RolesPage />, PATH, ROUTE);
    expect(await screen.findByText(/No roles yet/i)).toBeInTheDocument();
    // Step 6: the genuinely-empty case offers the way out of it.
    expect(screen.getByRole("button", { name: /define the first role/i })).toBeInTheDocument();
    unmount();

    stubRoles([cashier]);
    renderScreen(<RolesPage />, PATH, ROUTE);
    await screen.findByText("cashier");
    await userEvent.type(screen.getByLabelText(/search/i), "zzz");

    expect(await screen.findByText(/No roles match that search/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /clear the search/i })).toBeInTheDocument();
  });

  it("renders a refusal without offering a retry", async () => {
    stubApi((url) =>
      url.includes("/roles")
        ? { status: 403, body: { error: { code: "PERMISSION_DENIED", message: "no" } } }
        : { status: 200, body: { projects: [] } },
    );

    renderScreen(<RolesPage />, PATH, ROUTE);

    expect(await screen.findByRole("alert")).toHaveTextContent(/do not have access/i);
    expect(screen.queryByRole("button", { name: /try again/i })).not.toBeInTheDocument();
  });
});

describe("built-in roles", () => {
  it("explains why one cannot be deleted instead of showing a dead control", async () => {
    stubRoles([builtin]);

    renderScreen(<RolesPage />, PATH, ROUTE);

    const row = (await screen.findByText("owner")).closest("tr") as HTMLElement;
    // Step 5: the reason is present as text. A disabled button would be
    // skipped by keyboard navigation entirely, so the explanation would never
    // reach a screen-reader user.
    expect(within(row).getByText(/Built-in — cannot be deleted/i)).toBeInTheDocument();
    expect(within(row).queryByRole("button", { name: /delete/i })).not.toBeInTheDocument();
  });

  it("marks its origin in text, not by colour alone", async () => {
    stubRoles([builtin, cashier]);

    renderScreen(<RolesPage />, PATH, ROUTE);

    const row = (await screen.findByText("owner")).closest("tr") as HTMLElement;
    expect(within(row).getByText("Built-in")).toBeInTheDocument();

    const other = (await screen.findByText("cashier")).closest("tr") as HTMLElement;
    expect(within(other).getByText("Defined here")).toBeInTheDocument();
  });
});

describe("the role form", () => {
  it("refuses a key the server would refuse, with the server's own rule", async () => {
    stubRoles([]);

    renderScreen(<RolesPage />, PATH, ROUTE);
    await userEvent.click(await screen.findByRole("button", { name: /define role/i }));

    // Upper case is outside `ROLE_KEY_PATTERN`. Checked here so a rule that
    // drifted from the generated pattern turns this red rather than producing
    // a rejection the user cannot explain.
    const form = within(screen.getByRole("dialog"));
    await userEvent.type(form.getByLabelText("Key"), "Cashier");

    expect(form.getByLabelText("Key")).toHaveAttribute("aria-invalid", "true");
    expect(form.getByRole("button", { name: /define role/i })).toBeDisabled();
  });

  it("names the permission that is wrong rather than saying one of them is", async () => {
    stubRoles([]);

    renderScreen(<RolesPage />, PATH, ROUTE);
    await userEvent.click(await screen.findByRole("button", { name: /define role/i }));

    await userEvent.type(screen.getByLabelText("Key"), "cashier");
    await userEvent.type(screen.getByLabelText("Name"), "Cashier");
    await userEvent.type(screen.getByLabelText("Permissions"), "sale:create\nnot a key");

    expect(screen.getByText(/“not a key” is not a permission key/i)).toBeInTheDocument();
  });

  it("refuses a duplicate permission rather than quietly folding it", async () => {
    stubRoles([]);

    renderScreen(<RolesPage />, PATH, ROUTE);
    await userEvent.click(await screen.findByRole("button", { name: /define role/i }));

    await userEvent.type(screen.getByLabelText("Key"), "cashier");
    await userEvent.type(screen.getByLabelText("Name"), "Cashier");
    await userEvent.type(screen.getByLabelText("Permissions"), "sale:read\nsale:read");

    // The server refuses duplicates rather than folding them. A client that
    // deduplicated would send something other than what was typed.
    expect(screen.getByText(/is listed twice/i)).toBeInTheDocument();
  });

  it("accepts a role with no permissions at all", async () => {
    stubRoles([]);

    renderScreen(<RolesPage />, PATH, ROUTE);
    await userEvent.click(await screen.findByRole("button", { name: /define role/i }));

    const form = within(screen.getByRole("dialog"));
    await userEvent.type(form.getByLabelText("Key"), "placeholder");
    await userEvent.type(form.getByLabelText("Name"), "Placeholder");

    // A role with no permissions is a label, and labels are useful before the
    // permissions exist — the API says so, so the form must not be stricter.
    expect(form.getByRole("button", { name: /define role/i })).toBeEnabled();
  });

  it("explains why an existing role's key cannot change, in a focusable field", async () => {
    stubRoles([cashier]);

    renderScreen(<RolesPage />, PATH, ROUTE);
    await userEvent.click(await screen.findByRole("button", { name: /edit/i }));

    const key = screen.getByLabelText("Key");
    expect(key).toHaveValue("cashier");
    expect(key).toHaveAttribute("readonly");
    // Not `disabled` — a disabled input is unreachable, so the sentence
    // explaining the constraint would never be announced.
    expect(key).not.toBeDisabled();
    expect(screen.getByText(/a rename that revokes/i)).toBeInTheDocument();
  });

  it("surfaces the server's own reason when it refuses", async () => {
    stubApi((url, init) => {
      if (url.includes("/roles") && init.method === "POST") {
        return {
          status: 400,
          body: {
            error: {
              code: "VALIDATION_ERROR",
              message: "That key is reserved.",
              details: [{ field: "key", issue: "org_admin is an administrative role name" }],
            },
          },
        };
      }
      return url.includes("/roles")
        ? { status: 200, body: { roles: [] } }
        : { status: 200, body: { projects: [{ id: "p1", name: "Till" }] } };
    });

    renderScreen(<RolesPage />, PATH, ROUTE);
    await userEvent.click(await screen.findByRole("button", { name: /define role/i }));
    const form = within(screen.getByRole("dialog"));
    await userEvent.type(form.getByLabelText("Key"), "org_admin");
    await userEvent.type(form.getByLabelText("Name"), "Admin");
    await userEvent.click(form.getByRole("button", { name: /^define role$/i }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      /org_admin is an administrative role name/i,
    );
  });
});

describe("deleting a role", () => {
  it("states how many users hold it before asking", async () => {
    stubRoles([cashier]);

    renderScreen(<RolesPage />, PATH, ROUTE);
    await userEvent.click(await screen.findByRole("button", { name: /delete/i }));

    const dialog = screen.getByRole("dialog");
    // Step 4 and the DoD: an accurate affected-grant count, before the
    // request. Without it the administrator finds out from a refusal and has
    // to work out what it means.
    expect(dialog).toHaveTextContent(/3 users currently hold this role/i);
    expect(dialog).toHaveTextContent(/will refuse the deletion/i);
    // The verb, not "OK" or "Confirm".
    expect(screen.getByRole("button", { name: "Delete role" })).toBeInTheDocument();
  });

  it("says plainly when nobody holds it", async () => {
    stubRoles([unused]);

    renderScreen(<RolesPage />, PATH, ROUTE);
    await userEvent.click(await screen.findByRole("button", { name: /delete/i }));

    // Silence here would read as "we could not work it out", which is the
    // one thing this dialog exists to avoid.
    expect(screen.getByRole("dialog")).toHaveTextContent(/Nobody currently holds this role/i);
  });

  it("shows the server's refusal in the dialog rather than closing over it", async () => {
    stubApi((url, init) => {
      if (url.includes("/roles/") && init.method === "DELETE") {
        return {
          status: 409,
          body: { error: { code: "CONFLICT", message: "3 grants still reference this role." } },
        };
      }
      return url.includes("/roles")
        ? { status: 200, body: { roles: [cashier] } }
        : { status: 200, body: { projects: [{ id: "p1", name: "Till" }] } };
    });

    renderScreen(<RolesPage />, PATH, ROUTE);
    await userEvent.click(await screen.findByRole("button", { name: /delete/i }));
    await userEvent.click(screen.getByRole("button", { name: "Delete role" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(/3 grants still reference/i);
    // Still open: closing on failure would leave the administrator believing
    // it worked.
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });
});

describe("permission and accessibility", () => {
  it("hides the management controls from a caller whose token does not carry the role", async () => {
    clearToken();
    signIn(["MEMBER"]);
    stubRoles([cashier]);

    renderScreen(<RolesPage />, PATH, ROUTE);
    await screen.findByText("cashier");

    // UX only — the API refuses independently on every request, and this
    // assertion is not a security test (`docs/PLAN/08`).
    expect(screen.queryByRole("button", { name: /define role/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /delete/i })).not.toBeInTheDocument();
  });

  it("has no axe violations, table and form both", async () => {
    stubRoles([cashier, builtin]);

    const { container } = renderScreen(<RolesPage />, PATH, ROUTE);
    await screen.findByText("cashier");
    await expectNoAxeViolations(container);

    await userEvent.click(screen.getAllByRole("button", { name: /edit/i })[0]);
    await expectNoAxeViolations(container);
  });
});
