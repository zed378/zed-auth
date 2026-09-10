import { Route, Routes } from "react-router-dom";

import { RequireAuth } from "./RequireAuth";
import { CallbackPage } from "../pages/CallbackPage";
import { NotFoundPage } from "../pages/NotFoundPage";
import { OverviewPage } from "../pages/OverviewPage";
import { ApplicationsPage } from "../pages/ApplicationsPage";
import { AuditLogPage } from "../pages/AuditLogPage";
import { PlaceholderPage } from "../pages/PlaceholderPage";
import { ProjectsPage } from "../pages/ProjectsPage";
import { UserDetailPage } from "../pages/UserDetailPage";
import { UsersPage } from "../pages/UsersPage";
import { SilentCallbackPage } from "../pages/SilentCallbackPage";

/**
 * Routes, matching docs/PLAN/06-FRONTEND-ARCHITECTURE.md § Information Architecture.
 *
 * Every application route is wrapped in RequireAuth, so a route the user's
 * claims do not permit is genuinely unreachable by typing its URL rather than
 * merely absent from the navigation (docs/UI-UX/08 § Cross-Screen
 * Requirements). That is a user-experience property: the API enforces every
 * permission independently regardless (docs/PLAN/08, docs/SECURITY/02 §2-§3),
 * and a route guard is never the control.
 *
 * The two /auth routes are deliberately OUTSIDE the guard. One of them is how
 * a session is established in the first place, and guarding it would be a
 * redirect loop; the other renders nothing and exists only inside a hidden
 * iframe.
 */
export function AppRoutes() {
  return (
    <Routes>
      <Route path="/auth/callback" element={<CallbackPage />} />
      <Route path="/auth/silent" element={<SilentCallbackPage />} />

      <Route
        path="/"
        element={
          <RequireAuth>
            <OverviewPage />
          </RequireAuth>
        }
      />
      <Route
        path="/projects"
        element={
          <RequireAuth roles={["ORG_ADMIN", "ORG_OWNER", "INSTANCE_OWNER"]}>
            <ProjectsPage />
          </RequireAuth>
        }
      />
      <Route
        path="/projects/:projectId"
        element={
          <RequireAuth roles={["ORG_ADMIN", "ORG_OWNER", "INSTANCE_OWNER"]}>
            <ApplicationsPage />
          </RequireAuth>
        }
      />
      <Route
        path="/users"
        element={
          <RequireAuth roles={["ORG_ADMIN", "ORG_OWNER", "INSTANCE_OWNER"]}>
            <UsersPage />
          </RequireAuth>
        }
      />
      <Route
        path="/users/:userId"
        element={
          <RequireAuth roles={["ORG_ADMIN", "ORG_OWNER", "INSTANCE_OWNER"]}>
            <UserDetailPage />
          </RequireAuth>
        }
      />
      <Route
        path="/granted-projects"
        element={
          <RequireAuth>
            <PlaceholderPage title="Granted Projects" phase="4" />
          </RequireAuth>
        }
      />
      <Route
        path="/policies"
        element={
          <RequireAuth roles={["ORG_OWNER", "INSTANCE_OWNER"]}>
            <PlaceholderPage title="Policies" phase="1" />
          </RequireAuth>
        }
      />
      <Route
        path="/audit-log"
        element={
          <RequireAuth roles={["ORG_ADMIN", "ORG_OWNER", "INSTANCE_OWNER"]}>
            <AuditLogPage />
          </RequireAuth>
        }
      />
      <Route
        path="/settings"
        element={
          <RequireAuth>
            <PlaceholderPage title="Settings" phase="1" />
          </RequireAuth>
        }
      />
      <Route path="*" element={<NotFoundPage />} />
    </Routes>
  );
}
