import { GridRegion, ResponsiveGrid } from "../components/ResponsiveGrid";
import { UnsupportedWidth } from "../app/shell/UnsupportedWidth";
import type { ComponentEntry } from "./catalog";

export const responsiveEntry: ComponentEntry = {
  name: "Responsive Grid",
  rule: "Declare each region's mobile/tablet/desktop/wide column span. Use mobile columns only for mobile-supported surfaces; admin screens retain the 768px boundary. These foundation fixtures do not prescribe page layouts.",
  requiredStates: ["explicit-occupancy", "admin-width-boundary"],
  examples: [
    { name: "explicit-occupancy", render: () => <ResponsiveGrid aria-label="Responsive grid example">
      <GridRegion columns={{ mobile: 4, tablet: 8, desktop: 8, wide: 9 }} className="rounded border border-border p-3">
        <h3 className="text-small font-medium">Primary region</h3>
        <p className="text-small text-text-secondary">Columns: 4 / 8 / 8 / 9</p>
      </GridRegion>
      <GridRegion columns={{ mobile: 4, tablet: 8, desktop: 4, wide: 3 }} className="rounded border border-border p-3">
        <h3 className="text-small font-medium">Supporting region</h3>
        <p className="text-small text-text-secondary">Columns: 4 / 8 / 4 / 3</p>
      </GridRegion>
    </ResponsiveGrid> },
    { name: "admin-width-boundary", render: () => <UnsupportedWidth><p className="text-body">Admin fixture is supported at tablet width and above.</p></UnsupportedWidth> },
  ],
};
