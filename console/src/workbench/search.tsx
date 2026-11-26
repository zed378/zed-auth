import { useState } from "react";
import { MemoryRouter } from "react-router-dom";
import { SearchInput, ScopedSearchInput } from "../components/SearchInput";
import type { ComponentEntry } from "./catalog";

function GlobalExample({ loading = false, count = 3, global = false }: { loading?: boolean; count?: number; global?: boolean }) {
  const [query, setQuery] = useState("");
  const [searched, setSearched] = useState("");
  return <SearchInput label={global ? "Search console" : "Search projects"} variant={global ? "global" : "scoped"} query={query} onQueryChange={setQuery}
    onSearch={setSearched} loading={loading} results={{ query: searched, count }} />;
}

export const searchEntry: ComponentEntry = {
  name: "Search Input",
  rule: "Debounce queries, delay loading feedback, and announce only counts for the current query. Ctrl+K / Command+K focuses the mounted global input. Scoped queries persist in the URL.",
  requiredStates: ["global", "scoped", "loading", "no-results"],
  examples: [
    { name: "global", render: () => <GlobalExample global /> },
    { name: "scoped", render: () => <MemoryRouter initialEntries={["/?q=Acme"]}><ScopedSearchInput label="Search projects" onSearch={() => undefined} results={{ query: "Acme", count: 1 }} /></MemoryRouter> },
    { name: "loading", render: () => <GlobalExample loading /> },
    { name: "no-results", render: () => <GlobalExample count={0} /> },
  ],
};
