import type { SidebarsConfig } from "@docusaurus/plugin-content-docs";

const sidebar: SidebarsConfig = {
  apisidebar: [
    {
      type: "doc",
      id: "api-reference/zed-auth-management-api",
    },
    {
      type: "category",
      label: "Discovery",
      link: {
        type: "doc",
        id: "api-reference/discovery",
      },
      items: [
        {
          type: "doc",
          id: "api-reference/get-open-id-configuration",
          label: "OpenID Provider configuration",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api-reference/get-jwks",
          label: "JSON Web Key Set",
          className: "api-method get",
        },
      ],
    },
    {
      type: "category",
      label: "Operations",
      link: {
        type: "doc",
        id: "api-reference/operations",
      },
      items: [
        {
          type: "doc",
          id: "api-reference/get-liveness",
          label: "Liveness probe",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api-reference/get-readiness",
          label: "Readiness probe",
          className: "api-method get",
        },
      ],
    },
    {
      type: "category",
      label: "OAuth",
      items: [
        {
          type: "doc",
          id: "api-reference/authorize",
          label: "Authorization endpoint (authorization code + PKCE)",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api-reference/token",
          label: "Token endpoint",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/logout",
          label: "End session (RP-Initiated Logout 1.0)",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api-reference/logout-confirm",
          label: "Confirm sign-out",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/introspect",
          label: "Token introspection (RFC 7662)",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/revoke",
          label: "Token revocation (RFC 7009)",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/userinfo",
          label: "UserInfo endpoint",
          className: "api-method get",
        },
      ],
    },
    {
      type: "category",
      label: "Organizations",
      items: [
        {
          type: "doc",
          id: "api-reference/list-organizations",
          label: "List organizations",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api-reference/create-organization",
          label: "Create an organization",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/get-organization",
          label: "Read an organization",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api-reference/update-organization",
          label: "Update an organization",
          className: "api-method patch",
        },
        {
          type: "doc",
          id: "api-reference/delete-organization",
          label: "Delete an organization",
          className: "api-method delete",
        },
      ],
    },
    {
      type: "category",
      label: "Projects",
      items: [
        {
          type: "doc",
          id: "api-reference/list-projects",
          label: "List an organization's projects",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api-reference/create-project",
          label: "Create a project",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/get-project",
          label: "Read a project",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api-reference/update-project",
          label: "Rename a project",
          className: "api-method patch",
        },
        {
          type: "doc",
          id: "api-reference/delete-project",
          label: "Delete a project",
          className: "api-method delete",
        },
      ],
    },
    {
      type: "category",
      label: "Applications",
      items: [
        {
          type: "doc",
          id: "api-reference/list-applications",
          label: "List a project's applications",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api-reference/create-application",
          label: "Register an application",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/get-application",
          label: "Read an application",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api-reference/update-application",
          label: "Update an application",
          className: "api-method patch",
        },
        {
          type: "doc",
          id: "api-reference/delete-application",
          label: "Delete an application",
          className: "api-method delete",
        },
        {
          type: "doc",
          id: "api-reference/rotate-application-secret",
          label: "Issue a new client secret",
          className: "api-method post",
        },
      ],
    },
    {
      type: "category",
      label: "Users",
      items: [
        {
          type: "doc",
          id: "api-reference/list-users",
          label: "List an organization's users",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api-reference/create-user",
          label: "Invite a user",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/get-user",
          label: "Read a user",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api-reference/update-user",
          label: "Update a user's profile",
          className: "api-method patch",
        },
        {
          type: "doc",
          id: "api-reference/deactivate-user",
          label: "Deactivate a user",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/reactivate-user",
          label: "Reactivate a user",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/reset-user-password",
          label: "Send a password-reset link",
          className: "api-method post",
        },
      ],
    },
    {
      type: "category",
      label: "Audit",
      items: [
        {
          type: "doc",
          id: "api-reference/list-events",
          label: "Read the audit log",
          className: "api-method get",
        },
      ],
    },
  ],
};

export default sidebar.apisidebar;
