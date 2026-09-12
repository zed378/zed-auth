import { useEffect, useMemo, useRef, useState } from "react";

import { Skeleton } from "../../components/states";
import { useAdministeredOrganizations, useOrganization } from "../../lib/api/queries";
import { useOrgContext } from "../../lib/org/OrgProvider";

type Administered = { id: string; name: string; roles: string[] };

/**
 * The organization switcher (P2-13, `docs/UI-UX/08` § Organization switcher).
 *
 * **It is present as a label before it is present as a control.** `docs/UI-UX/08`
 * says the switcher itself is "relevant only once multi-org is active", and
 * `P2-13` step 1 asks for it to appear only when the caller actually
 * administers more than one organization. But step 4 asks for the active
 * organization to be unmistakable **at all times** — and that is the more
 * important half.
 *
 * The failure it guards against is specific and severe: an administrator
 * performing a destructive action believing they are somewhere else. So the
 * name is always in the chrome. Whether it is a button depends on whether
 * there is anywhere to go.
 *
 * The list comes from `GET /v1/me/organizations`, which is server-side truth.
 * Deriving it from the token's manager-role claim would be wrong twice over:
 * the claim carries role names without their scopes, and it is a snapshot up
 * to ten minutes stale.
 */
export function OrgSwitcher() {
  const { orgId, homeOrgId, switched, switchTo } = useOrgContext();

  // Only asked for once signed in. Before that there is no caller to describe.
  const administered = useAdministeredOrganizations(homeOrgId !== null);
  const current = useOrganization(orgId);

  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const panel = useRef<HTMLDivElement>(null);
  const search = useRef<HTMLInputElement>(null);

  // Memoised because `?? []` is a fresh array every render, which would make
  // the filter below recompute whether or not anything changed.
  const options = useMemo(() => (administered.data ?? []) as Administered[], [administered.data]);
  const switchable = options.length > 1;

  const matches = useMemo(() => {
    const needle = filter.trim().toLowerCase();
    if (needle === "") return options;
    return options.filter((option) => option.name.toLowerCase().includes(needle));
  }, [options, filter]);

  // The name from the switcher's own list where it has one, so the label does
  // not sit blank while `GET /v1/organizations/{org_id}` is in flight — and so
  // it still reads correctly for a caller whose organization read is refused.
  const activeName =
    options.find((option) => option.id === orgId)?.name ??
    (current.data?.name as string | undefined);

  useEffect(() => {
    if (!open) return;

    // Focus moves into the popup when it opens, which is what a listbox is
    // supposed to do — done with a ref rather than `autoFocus`, because that
    // prop fires on mount regardless of why the element mounted and is a
    // genuine accessibility hazard on anything that is not a popup.
    search.current?.focus();

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    const onClickAway = (event: MouseEvent) => {
      if (panel.current !== null && !panel.current.contains(event.target as Node)) {
        setOpen(false);
      }
    };

    document.addEventListener("keydown", onKeyDown);
    document.addEventListener("mousedown", onClickAway);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      document.removeEventListener("mousedown", onClickAway);
    };
  }, [open]);

  // The active organization is not among the ones the server says the caller
  // administers. Only meaningful once the list has actually arrived — and only
  // when it arrived successfully, since a failed read names nothing.
  const unauthorized =
    orgId !== null &&
    administered.isSuccess &&
    options.length > 0 &&
    !options.some((option) => option.id === orgId);

  if (homeOrgId === null) return null;

  return (
    <div ref={panel} className="relative px-3 pb-3">
      <p className="text-small text-text-secondary" id="org-switcher-label">
        Organization
      </p>

      {!switchable ? (
        // One organization: a label, not a disabled control. A dropdown with a
        // single entry is a control that teaches people to ignore it.
        <p className="mt-1 flex flex-wrap items-baseline gap-2 text-body font-medium text-text-primary">
          {administered.isPending && activeName === undefined ? (
            <Skeleton className="h-5 w-32" />
          ) : (
            (activeName ?? "Your organization")
          )}
        </p>
      ) : (
        <button
          type="button"
          aria-haspopup="true"
          aria-expanded={open}
          aria-labelledby="org-switcher-label org-switcher-value"
          onClick={() => {
            setOpen((was) => !was);
            setFilter("");
          }}
          className="mt-1 flex w-full items-center justify-between gap-2 rounded border border-border bg-bg-surface px-3 py-2 text-left focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        >
          <span id="org-switcher-value" className="truncate text-body font-medium text-text-primary">
            {activeName ?? "…"}
          </span>
          <span aria-hidden="true" className="text-text-secondary">
            ⌄
          </span>
        </button>
      )}

      {/*
        Step 4: unmistakable when it is not the usual one. Text, not a colour
        — and not `color-danger`, which is reserved for destructive actions
        (`docs/UI-UX/06`). Acting elsewhere is unusual, not dangerous; the
        danger is not noticing, which a sentence fixes and a red border does
        not.

        Two different sentences, because they are two different situations. A
        deliberate switch is normal and wants a reminder. A context the caller
        does NOT administer — which `?org=` in a pasted URL can produce — means
        every request on the page is being refused, and "another organization"
        would leave somebody reading error states wondering what broke.

        Only once the list has loaded: saying "you do not administer this"
        while still finding out would be wrong more often than right.
      */}
      {unauthorized ? (
        <p role="status" className="mt-1 text-small text-warning">
          You do not administer this organization, so this console cannot read it. Everything on
          this page will be refused.
        </p>
      ) : switched ? (
        <p role="status" className="mt-1 text-small text-warning">
          You are acting in another organization, not your own.
        </p>
      ) : null}

      {open ? (
        <div className="absolute inset-x-3 z-40 mt-1 rounded border border-border bg-bg-surface p-2 shadow-overlay">
          <label htmlFor="org-filter" className="sr-only">
            Filter organizations
          </label>
          <input
            id="org-filter"
            ref={search}
            type="search"
            value={filter}
            onChange={(event) => setFilter(event.currentTarget.value)}
            placeholder="Filter"
            className="w-full rounded border border-border bg-bg-base px-2 py-1 text-small text-text-primary"
          />

          {/*
            A list of buttons, NOT `role="listbox"` with `role="option"`.
            That pattern promises arrow-key navigation, a single tab stop and
            typeahead, and this implements none of them — axe caught the
            structural half (a listbox's children must be options), but the
            real defect is the promise. A screen-reader user told they are in
            a listbox will press Down and nothing will happen.

            The same call `ProjectNav` makes: mark it up as what it is.
          */}
          <ul aria-labelledby="org-switcher-label" className="mt-2 max-h-64 overflow-y-auto">
            {matches.length === 0 ? (
              <li className="px-2 py-2 text-small text-text-secondary">
                None of your organizations match “{filter}”.
              </li>
            ) : (
              matches.map((option) => (
                <li key={option.id}>
                  <button
                    type="button"
                    // `aria-current`, not `aria-selected`: selection belongs to
                    // a listbox, and this is a set of links-like actions where
                    // one is the page you are on.
                    aria-current={option.id === orgId ? "true" : undefined}
                    onClick={() => {
                      switchTo(option.id);
                      setOpen(false);
                    }}
                    className={`flex w-full flex-col items-start rounded px-2 py-2 text-left hover:bg-bg-base focus-visible:outline focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent ${
                      option.id === orgId ? "bg-bg-base" : ""
                    }`}
                  >
                    <span className="text-body text-text-primary">
                      {option.name}
                      {option.id === orgId ? (
                        // Text, not a tick alone: the current entry has to be
                        // identifiable without colour or an icon.
                        <span className="ml-2 text-small text-text-secondary">(current)</span>
                      ) : null}
                    </span>
                    {/*
                      What the caller is there. An administrator switching
                      between an organization they own and one they merely
                      administer should be able to see which is which before
                      they arrive.
                    */}
                    <span className="text-small text-text-secondary">
                      {option.roles.join(", ").toLowerCase().replace(/_/g, " ")}
                    </span>
                  </button>
                </li>
              ))
            )}
          </ul>
        </div>
      ) : null}
    </div>
  );
}
