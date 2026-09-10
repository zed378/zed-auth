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
          id: "api-reference/userinfo",
          label: "UserInfo endpoint",
          className: "api-method get",
        },
      ],
    },
  ],
};

export default sidebar.apisidebar;
