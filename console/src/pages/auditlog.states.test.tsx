import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { AuditLogPage } from "./AuditLogPage";
import { queryKeys } from "../lib/api/client";
import { AuthProvider } from "../lib/auth/AuthProvider";
import { AuthError } from "../lib/auth/oidc";
import { clearToken } from "../lib/auth/tokens";
import { signIn } from "../test/harness";

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return { ...actual, renewSilently: () => Promise.reject(new AuthError("login_required", null)) };
});

const event = {
  id: "event-1", event_type: "user.login.success", actor_user_id: "user-1",
  occurred_at: "2026-09-10T12:00:00Z", payload: { reason: "retained payload" },
};
const key = [...queryKeys.events, "org-1", "", "", "", ""];
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), {
  status, headers: { "Content-Type": "application/json" },
});

function mount(cached = false) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  if (cached) client.setQueryData(key, { events: [event] }, { updatedAt: Date.now() - 60_000 });
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/audit"]}>
    <AuthProvider><AuditLogPage /></AuthProvider>
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

describe("Audit log shared collection states", () => {
  it("announces initial loading without showing an empty result before the response", async () => {
    let finish!: (response: Response) => void;
    vi.stubGlobal("fetch", () => new Promise<Response>((resolve) => { finish = resolve; }));
    mount();
    expect(await screen.findByText("Loading audit events")).toBeInTheDocument();
    expect(screen.queryByText("No events yet")).not.toBeInTheDocument();
    await act(async () => { finish(json({ events: [] })); });
    expect(await screen.findByText("No events yet")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /create/i })).not.toBeInTheDocument();
  });

  it("clears event and date filters after a filtered empty response", async () => {
    const requests: URL[] = [];
    vi.stubGlobal("fetch", async (request: Request) => {
      requests.push(new URL(request.url));
      return json({ events: [] });
    });
    mount();
    await screen.findByText("No events yet");
    await userEvent.selectOptions(screen.getByLabelText("Event type"), "user.login.success");
    expect(await screen.findByText("No events match that search")).toBeInTheDocument();
    // Date inputs use their native change path; typing is not supported consistently in jsdom.
    fireEvent.change(screen.getByLabelText("From"), { target: { value: "2026-09-01" } });
    fireEvent.change(screen.getByLabelText("To"), { target: { value: "2026-09-10" } });
    await waitFor(() => expect(requests.at(-1)?.searchParams.get("to")).toBe("2026-09-10T23:59:59Z"));
    await userEvent.click(await screen.findByRole("button", { name: "Clear the search" }));
    expect(await screen.findByText("No events yet")).toBeInTheDocument();
    expect(screen.getByLabelText("Event type")).toHaveValue("");
    expect(screen.getByLabelText("From")).toHaveValue("");
    expect(screen.getByLabelText("To")).toHaveValue("");
    const final = requests.at(-1)?.searchParams;
    expect(final?.has("event_type")).toBe(false);
    expect(final?.has("from")).toBe(false);
    expect(final?.has("to")).toBe(false);
  });

  it("retains the table and expanded payload after refresh failure and retries only that request", async () => {
    let fail = false;
    const requests: string[] = [];
    vi.stubGlobal("fetch", async (request: Request) => {
      requests.push(request.url);
      return fail ? json({ error: { code: "INTERNAL_ERROR" } }, 500) : json({ events: [event] });
    });
    const client = mount(true);
    await userEvent.click(await screen.findByRole("button", { name: "Detail" }));
    const table = screen.getByRole("table", { name: "Audit events, newest first" });
    const detail = screen.getByRole("region", { name: `Detail for ${event.event_type}` });
    const scroll = vi.spyOn(window, "scrollTo").mockImplementation(() => undefined);
    fail = true;
    await act(async () => { await client.invalidateQueries({ queryKey: key }); });
    expect(await screen.findByRole("alert")).toHaveTextContent(/last-known data/i);
    expect(detail).toHaveTextContent("retained payload");
    expect(screen.getByRole("table")).toBe(table);
    const beforeRetry = requests.length;
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
    expect(requests.length).toBe(beforeRetry + 1);
    expect(new Set(requests).size).toBe(1);
    expect(requests[0]).toContain("/organizations/org-1/events?");
    expect(screen.getByRole("table")).toBe(table);
    expect(screen.getByRole("region", { name: `Detail for ${event.event_type}` })).toBe(detail);
    expect(scroll).not.toHaveBeenCalled();
    scroll.mockRestore();
  });

  it("suppresses cached events, payload and pagination when the API denies access", async () => {
    let deny = false;
    vi.stubGlobal("fetch", async () => deny
      ? json({ error: { code: "PERMISSION_DENIED", message: "private server detail" } }, 403)
      : json({ events: [event] }));
    const client = mount();
    await userEvent.click(await screen.findByRole("button", { name: "Detail" }));
    expect(screen.getByText(/retained payload/)).toBeInTheDocument();
    deny = true;
    await act(async () => { await client.invalidateQueries({ queryKey: key }); });
    expect(await screen.findByRole("alert")).toHaveTextContent(/review your access/i);
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.queryByRole("region", { name: `Detail for ${event.event_type}` })).not.toBeInTheDocument();
    expect(screen.queryByText(event.event_type)).not.toBeInTheDocument();
    expect(screen.queryByText(/retained payload/)).not.toBeInTheDocument();
    expect(screen.queryByText("private server detail")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Try again|Next|Previous/ })).not.toBeInTheDocument();
  });

  it("keeps the cursor and filters when retrying a failed later page", async () => {
    const requests: URL[] = [];
    let fail = false;
    vi.stubGlobal("fetch", async (request: Request) => {
      const url = new URL(request.url);
      requests.push(url);
      if (url.searchParams.has("page_token")) return fail
        ? json({ error: { code: "INTERNAL_ERROR" } }, 500)
        : json({ events: [{ ...event, id: "event-2", event_type: "user.created" }] });
      return json({ events: [event], page_info: { next_page_token: "next-page" } });
    });
    mount();
    await userEvent.selectOptions(screen.getByLabelText("Event type"), "user.login.success");
    await screen.findByRole("button", { name: "Detail" });
    fail = true;
    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/could not be completed/i);
    const failedUrl = requests.at(-1)?.href;
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("user.created")).toBeInTheDocument();
    expect(requests.at(-1)?.href).toBe(failedUrl);
    expect(requests.at(-1)?.searchParams.get("page_token")).toBe("next-page");
    expect(requests.at(-1)?.searchParams.get("event_type[]") ?? requests.at(-1)?.searchParams.get("event_type"))
      .toBe("user.login.success");
    await userEvent.click(screen.getByRole("button", { name: "Previous" }));
    expect(await screen.findByText("user.login.success")).toBeInTheDocument();
    expect(requests.at(-1)?.searchParams.has("page_token")).toBe(false);
  });
});
