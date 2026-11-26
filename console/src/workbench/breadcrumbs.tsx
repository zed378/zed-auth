import { MemoryRouter } from "react-router-dom";
import { ConsoleBreadcrumb } from "../components/Breadcrumb";
import type { ComponentEntry } from "./catalog";

export const breadcrumbEntry: ComponentEntry = {
  name: "Breadcrumb",
  rule: "Reflect the actual data hierarchy. Every segment navigates. Keep Projects distinct from Granted Projects and preserve full names for assistive technology.",
  requiredStates: ["owned", "delegated", "long-name"],
  examples: [
    { name: "owned", render: () => <MemoryRouter initialEntries={["/projects/pos/roles"]}><ConsoleBreadcrumb organizationName="Acme Org" projectName="POS Project" /></MemoryRouter> },
    { name: "delegated", render: () => <MemoryRouter initialEntries={["/granted-projects"]}><ConsoleBreadcrumb organizationName="Vendor Org" /></MemoryRouter> },
    { name: "long-name", render: () => <MemoryRouter initialEntries={["/projects/pos/authorizations"]}><ConsoleBreadcrumb organizationName="Acme Org" projectName="Point of sale application for the international retail organization" /></MemoryRouter> },
  ],
};
