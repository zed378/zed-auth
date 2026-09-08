import { Route, Routes } from "react-router-dom";

import { NotFoundPage } from "../pages/NotFoundPage";
import { OverviewPage } from "../pages/OverviewPage";
import { PlaceholderPage } from "../pages/PlaceholderPage";

/**
 * Routes, matching PLAN/06-FRONTEND-ARCHITECTURE.md § Information Architecture.
 *
 * The organization-scoped screens are all present as placeholders. Two things
 * that must be true of them once they carry real content, stated here because
 * this is where someone will add the first one:
 *
 *   Permission-gated routes are genuinely unreachable, not hidden
 *   (console/README.md). Hiding a nav item leaves the route reachable by
 *   typing the URL.
 *
 *   The API enforces every check independently regardless (PLAN/08,
 *   SECURITY/02 §2-§3). A route guard is a user-experience feature; it is
 *   never the control.
 *
 * The Instance group (PLAN/06's INSTANCE_OWNER section) is absent until the
 * console can read a role claim from an access token, which is P1-03.
 */
export function AppRoutes() {
  return (
    <Routes>
      <Route path="/" element={<OverviewPage />} />
      <Route path="/projects/*" element={<PlaceholderPage title="Projects" phase="1" />} />
      <Route path="/users/*" element={<PlaceholderPage title="Users" phase="1" />} />
      <Route
        path="/granted-projects"
        element={<PlaceholderPage title="Granted Projects" phase="4" />}
      />
      <Route path="/policies" element={<PlaceholderPage title="Policies" phase="1" />} />
      <Route path="/audit-log" element={<PlaceholderPage title="Audit Log" phase="1" />} />
      <Route path="/settings" element={<PlaceholderPage title="Settings" phase="1" />} />
      <Route path="*" element={<NotFoundPage />} />
    </Routes>
  );
}
