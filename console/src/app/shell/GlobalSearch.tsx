import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { useAuth } from "../../lib/auth/AuthProvider";
import { api } from "../../lib/api/client";
import { asFailure, collectPages, PAGE_SIZE, useAdministeredOrganizations, useOrgId } from "../../lib/api/queries";
import { Modal } from "../../components/Modal";
import { buildSearchResults, isResourceIdQuery, type GlobalSearchResult } from "./searchResults";

/** Keyboard-reachable, permission-scoped console directory lookup (PF-14). */
export function GlobalSearch() {
  const { hasRole, status } = useAuth();
  const orgId = useOrgId();
  const navigate = useNavigate();
  const input = useRef<HTMLInputElement>(null);
  const [open, setOpen] = useState(false);
  const [rawQuery, setRawQuery] = useState("");
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const [slowQuery, setSlowQuery] = useState<string | null>(null);
  const canSearch = hasRole("ORG_ADMIN", "ORG_OWNER", "INSTANCE_OWNER");
  const organizations = useAdministeredOrganizations(open && canSearch);

  useEffect(() => {
    const timer = window.setTimeout(() => setQuery(rawQuery.trim()), 250);
    return () => window.clearTimeout(timer);
  }, [rawQuery]);

  useEffect(() => {
    function onShortcut(event: globalThis.KeyboardEvent) {
      if (canSearch && (event.ctrlKey || event.metaKey) && !event.altKey && !event.shiftKey && event.key.toLowerCase() === "k" && !event.isComposing) {
        event.preventDefault();
        setActive(0);
        setOpen(true);
      }
    }
    window.addEventListener("keydown", onShortcut);
    return () => window.removeEventListener("keydown", onShortcut);
  }, [canSearch]);

  useEffect(() => {
    if (open) input.current?.focus();
  }, [open]);

  const projects = useQuery({
    // Project lists have no server-side search parameter. Fetch the scoped
    // directory once and filter locally rather than refetching it per keypress.
    queryKey: ["global-search", "projects", orgId],
    enabled: open && canSearch && orgId !== null && query.length >= 2,
    staleTime: 15_000,
    queryFn: async () => {
      const all = await collectPages(async (token) => {
        const { data, error, response } = await api.GET("/v1/organizations/{org_id}/projects", {
          params: {
            path: { org_id: orgId as string },
            query: { page_size: PAGE_SIZE, ...(token === undefined ? {} : { page_token: token }) },
          },
        });
        if (error !== undefined) throw asFailure(error, response.status);
        return { items: data.projects, next: data.page_info?.next_page_token };
      });
      return all.items;
    },
  });

  const projectById = useQuery({
    queryKey: ["global-search", "project-id", orgId, query],
    enabled: open && canSearch && orgId !== null && isResourceIdQuery(query),
    staleTime: 15_000,
    queryFn: async () => {
      const { data, error, response } = await api.GET("/v1/organizations/{org_id}/projects/{project_id}", {
        params: { path: { org_id: orgId as string, project_id: query } },
      });
      if (error === undefined) return data;
      // Both absent and inaccessible resources look absent in search.
      if (response.status === 403 || response.status === 404) return null;
      throw asFailure(error, response.status);
    },
  });

  const users = useQuery({
    queryKey: ["global-search", "users", orgId, query],
    enabled: open && canSearch && orgId !== null && query.length >= 2,
    staleTime: 15_000,
    queryFn: async () => {
      const all = await collectPages(async (token) => {
        const { data, error, response } = await api.GET("/v1/organizations/{org_id}/users", {
          params: {
            path: { org_id: orgId as string },
            query: {
              page_size: PAGE_SIZE,
              search: query,
              ...(token === undefined ? {} : { page_token: token }),
            },
          },
        });
        if (error !== undefined) throw asFailure(error, response.status);
        return { items: data.users, next: data.page_info?.next_page_token };
      });
      const found = [...all.items];

      // The list endpoint searches names and email on the server. For an
      // identifier-shaped query, an exact detail read adds ID lookup while the
      // API's normal authorization remains the disclosure boundary.
      if (isResourceIdQuery(query)) {
        const { data, error, response } = await api.GET("/v1/organizations/{org_id}/users/{user_id}", {
          params: { path: { org_id: orgId as string, user_id: query } },
        });
        if (error === undefined && !found.some((candidate) => candidate.id === data.id)) found.push(data);
        // 403 and 404 intentionally collapse to absence; distinguishing them
        // would turn the search box into an account-enumeration oracle.
        if (error !== undefined && response.status !== 403 && response.status !== 404) throw asFailure(error, response.status);
      }
      return found;
    },
  });

  const results = useMemo(() => buildSearchResults({
    query,
    activeOrgId: orgId ?? "",
    organizations: organizations.data ?? [],
    projects: projectById.data === undefined || projectById.data === null
      ? projects.data ?? []
      : [...(projects.data ?? []), ...(projects.data?.some((project) => project.id === projectById.data?.id) ? [] : [projectById.data])],
    users: users.data ?? [],
  }), [query, orgId, organizations.data, projects.data, projectById.data, users.data]);
  const loading = (rawQuery.trim().length >= 2 && rawQuery.trim() !== query)
    || projects.isFetching || projectById.isFetching || users.isFetching || (organizations.isFetching && canSearch);
  const failure = projects.error ?? projectById.error ?? users.error ?? (organizations.error && canSearch ? organizations.error : null);
  const showLoading = loading && slowQuery === query;

  useEffect(() => {
    if (!loading || query.length < 2) return;
    const timer = window.setTimeout(() => setSlowQuery(query), 200);
    return () => { window.clearTimeout(timer); setSlowQuery(null); };
  }, [loading, query]);

  // Only results for the text now in the box may be shown or chosen.
  const current = !loading && !failure && query.length >= 2 && rawQuery.trim() === query;
  const selectable = current ? results : [];
  const selectedIndex = selectable.length === 0 ? 0 : active % selectable.length;

  function close() {
    setOpen(false);
    setRawQuery("");
    setQuery("");
  }

  function openSearch() {
    setActive(0);
    setOpen(true);
  }

  function choose(result: GlobalSearchResult) {
    navigate(result.to);
    close();
  }

  function onInputKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "ArrowDown" && selectable.length > 0) {
      event.preventDefault();
      setActive((index) => (index + 1) % selectable.length);
    } else if (event.key === "ArrowUp" && selectable.length > 0) {
      event.preventDefault();
      setActive((index) => (index + selectable.length - 1) % selectable.length);
    } else if (event.key === "Enter" && selectable[selectedIndex]) {
      event.preventDefault();
      choose(selectable[selectedIndex]);
    }
  }

  if (status !== "authenticated" || !canSearch) return null;

  return <>
    <button type="button" onClick={openSearch} aria-keyshortcuts="Control+k Meta+k"
      className="min-h-9 rounded border border-border bg-bg-surface px-3 text-small text-text-primary hover:bg-bg-base">
      Search <span aria-hidden="true" className="ml-2 text-text-secondary">Ctrl / ⌘ K</span>
    </button>
    <Modal open={open} title="Search the console" onClose={close}>
      <label htmlFor="global-search-input" className="sr-only">Search organizations, projects, and users</label>
      <input ref={input} id="global-search-input" type="search" value={rawQuery} onChange={(event) => { setActive(0); setRawQuery(event.target.value); }}
        onKeyDown={onInputKeyDown} role="combobox" aria-expanded={query.length >= 2}
        aria-controls="global-search-results" aria-activedescendant={selectable[selectedIndex] ? `global-search-option-${selectedIndex}` : undefined}
        aria-autocomplete="list" placeholder="Name, email, or ID" autoComplete="off"
        className="w-full rounded-md border border-border bg-bg-surface px-3 py-2 text-body text-text-primary focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent" />
      <div id="global-search-results" role="listbox" aria-label="Search results" className="mt-3 max-h-96 overflow-y-auto">
        {rawQuery.trim().length < 2 ? <p className="py-4 text-small text-text-secondary">Type at least two characters to search.</p> : null}
        {showLoading ? <p role="status" aria-live="polite" className="py-4 text-small text-text-secondary">Searching…</p> : null}
        {!loading && failure ? <p role="alert" className="py-4 text-small text-danger">Search failed. Try again in a moment.</p> : null}
        {current && results.length === 0 ? <p role="status" className="py-4 text-small text-text-secondary">No matching resources.</p> : null}
        {selectable.length > 0 ? <GroupedResults results={selectable} active={selectedIndex} onChoose={choose} onActiveChange={setActive} /> : null}
      </div>
    </Modal>
  </>;
}

function GroupedResults({ results, active, onChoose, onActiveChange }: {
  results: GlobalSearchResult[];
  active: number;
  onChoose: (result: GlobalSearchResult) => void;
  onActiveChange: (index: number) => void;
}) {
  const groups = ["Organization", "Project", "User"] as const;
  let index = 0;
  return <>
    {groups.map((kind) => {
      const items = results.filter((result) => result.kind === kind);
      if (items.length === 0) return null;
      const heading = kind === "Organization" ? "Organizations" : `${kind}s`;
      return <div key={kind} role="group" aria-label={`${kind} results`} className="mb-3">
        <h3 className="mb-1 text-small font-medium text-text-secondary">{heading}</h3>
        <ul className="grid gap-1">
          {items.map((result) => {
            const itemIndex = index++;
            return <li key={`${kind}:${result.id}`}>
              <button type="button" role="option" id={`global-search-option-${itemIndex}`} aria-selected={active === itemIndex}
                onMouseMove={() => onActiveChange(itemIndex)}
                onClick={() => onChoose(result)}
                className={`w-full rounded px-3 py-2 text-left ${active === itemIndex ? "bg-accent/10" : "hover:bg-bg-base"}`}>
                <span className="block text-body text-text-primary">{result.title}</span>
                <span className="block text-small text-text-secondary">{result.detail}</span>
                <span className="sr-only">{result.kind}</span>
              </button>
            </li>;
          })}
        </ul>
      </div>;
    })}
  </>;
}
