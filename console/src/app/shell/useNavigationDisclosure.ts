import { useMemo } from "react";
import { useAdministeredOrganizations, useOrgId, useReceivedGrants } from "../../lib/api/queries";
import { ADVANCED_NAV_DISCLOSURE, type DisclosureContext } from "./navigationDisclosure";

/** Server-backed relevance inputs for the active organization's navigation. */
export function useNavigationDisclosure(): DisclosureContext {
  const orgId = useOrgId();
  const organizations = useAdministeredOrganizations(orgId !== null);
  const current = organizations.data?.find((organization) => organization.id === orgId);
  const grantRule = ADVANCED_NAV_DISCLOSURE.find((rule) => rule.feature === "project-grants");
  const canManageGrants = current?.roles.some((role) => grantRule?.managerRoles.includes(role)) ?? false;
  const grants = useReceivedGrants(canManageGrants ? orgId : null);

  return useMemo(() => ({
    // The receiver endpoint is the existence signal available to a tenant-wide
    // list. Outgoing grants remain reachable through each project to its
    // authorized managers; this sidebar item describes the received list.
    existingFeatures: new Set(grants.data && grants.data.length > 0 ? ["project-grants"] : []),
    managerRoles: new Set(current?.roles ?? []),
  }), [grants.data, current?.roles]);
}
