import { lazy, Suspense } from "react";
import { Route, Routes } from "react-router-dom";

import { RequireAuth } from "./RequireAuth";
import { NotFoundPage } from "../pages/NotFoundPage";

const CallbackPage = lazy(() => import("../pages/CallbackPage").then(({ CallbackPage }) => ({ default: CallbackPage })));
const SilentCallbackPage = lazy(() => import("../pages/SilentCallbackPage").then(({ SilentCallbackPage }) => ({ default: SilentCallbackPage })));
const OverviewPage = lazy(() => import("../pages/OverviewPage").then(({ OverviewPage }) => ({ default: OverviewPage })));
const ApplicationsPage = lazy(() => import("../pages/ApplicationsPage").then(({ ApplicationsPage }) => ({ default: ApplicationsPage })));
const AuditLogPage = lazy(() => import("../pages/AuditLogPage").then(({ AuditLogPage }) => ({ default: AuditLogPage })));
const AuthorizationsPage = lazy(() => import("../pages/AuthorizationsPage").then(({ AuthorizationsPage }) => ({ default: AuthorizationsPage })));
const GrantedProjectsPage = lazy(() => import("../pages/GrantedProjectsPage").then(({ GrantedProjectsPage }) => ({ default: GrantedProjectsPage })));
const PlaceholderPage = lazy(() => import("../pages/PlaceholderPage").then(({ PlaceholderPage }) => ({ default: PlaceholderPage })));
const PoliciesPage = lazy(() => import("../pages/PoliciesPage").then(({ PoliciesPage }) => ({ default: PoliciesPage })));
const ProjectGrantsPage = lazy(() => import("../pages/ProjectGrantsPage").then(({ ProjectGrantsPage }) => ({ default: ProjectGrantsPage })));
const ProjectsPage = lazy(() => import("../pages/ProjectsPage").then(({ ProjectsPage }) => ({ default: ProjectsPage })));
const RolesPage = lazy(() => import("../pages/RolesPage").then(({ RolesPage }) => ({ default: RolesPage })));
const UserDetailPage = lazy(() => import("../pages/UserDetailPage").then(({ UserDetailPage }) => ({ default: UserDetailPage })));
const UsersPage = lazy(() => import("../pages/UsersPage").then(({ UsersPage }) => ({ default: UsersPage })));

function RouteLoading() {
  return (
    <main className="card" role="status" aria-live="polite">
      <h1>Loading screen</h1>
      <p>The requested screen is loading.</p>
    </main>
  );
}

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
    <Suspense fallback={<RouteLoading />}>
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
        path="/projects/:projectId/roles"
        element={
          <RequireAuth roles={["ORG_ADMIN", "ORG_OWNER", "INSTANCE_OWNER"]}>
            <RolesPage />
          </RequireAuth>
        }
      />
      <Route
        path="/projects/:projectId/authorizations"
        element={
          <RequireAuth roles={["ORG_ADMIN", "ORG_OWNER", "INSTANCE_OWNER"]}>
            <AuthorizationsPage />
          </RequireAuth>
        }
      />
      <Route
        path="/projects/:projectId/grants"
        element={
          <RequireAuth roles={["ORG_ADMIN", "ORG_OWNER", "INSTANCE_OWNER"]}>
            <ProjectGrantsPage />
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
          <RequireAuth roles={["ORG_ADMIN", "ORG_OWNER", "INSTANCE_OWNER"]}>
            <GrantedProjectsPage />
          </RequireAuth>
        }
      />
      <Route
        path="/policies"
        element={
          <RequireAuth roles={["ORG_OWNER", "INSTANCE_OWNER"]}>
            <PoliciesPage />
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
            <PlaceholderPage title="Settings" phase={null} />
          </RequireAuth>
        }
      />
      <Route path="*" element={<NotFoundPage />} />
      </Routes>
    </Suspense>
  );
}
