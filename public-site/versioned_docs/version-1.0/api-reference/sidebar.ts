import type { SidebarsConfig } from "@docusaurus/plugin-content-docs";

const sidebar: SidebarsConfig = {
  apisidebar: [
    {
      type: "doc",
      id: "api-reference/zed-auth-management-api",
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
  ],
};

export default sidebar.apisidebar;
