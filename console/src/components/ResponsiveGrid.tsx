import type { ComponentPropsWithoutRef } from "react";

type MobileSpan = 1 | 2 | 3 | 4;
type TabletSpan = MobileSpan | 5 | 6 | 7 | 8;
type DesktopSpan = TabletSpan | 9 | 10 | 11 | 12;

export interface RegionSpans {
  mobile: MobileSpan;
  tablet: TabletSpan;
  desktop: DesktopSpan;
  wide: DesktopSpan;
}

const spans = {
  mobile: ["", "col-span-1", "col-span-2", "col-span-3", "col-span-4"],
  tablet: ["", "tablet:col-span-1", "tablet:col-span-2", "tablet:col-span-3", "tablet:col-span-4", "tablet:col-span-5", "tablet:col-span-6", "tablet:col-span-7", "tablet:col-span-8"],
  desktop: ["", "desktop:col-span-1", "desktop:col-span-2", "desktop:col-span-3", "desktop:col-span-4", "desktop:col-span-5", "desktop:col-span-6", "desktop:col-span-7", "desktop:col-span-8", "desktop:col-span-9", "desktop:col-span-10", "desktop:col-span-11", "desktop:col-span-12"],
  wide: ["", "wide:col-span-1", "wide:col-span-2", "wide:col-span-3", "wide:col-span-4", "wide:col-span-5", "wide:col-span-6", "wide:col-span-7", "wide:col-span-8", "wide:col-span-9", "wide:col-span-10", "wide:col-span-11", "wide:col-span-12"],
} as const;

/** Mobile columns are for in-scope mobile screens, outside UnsupportedWidth. */
export function ResponsiveGrid({ className = "", ...props }: ComponentPropsWithoutRef<"div">) {
  return <div {...props} className={`grid min-w-0 grid-cols-4 gap-4 tablet:grid-cols-8 desktop:grid-cols-12 wide:gap-5 ${className}`} />;
}

/** Require an occupancy decision at every breakpoint rather than infer a layout. */
export function GridRegion({ columns, className = "", ...props }: ComponentPropsWithoutRef<"div"> & { columns: RegionSpans }) {
  const classes = Object.entries(columns).map(([breakpoint, value]) => {
    const options = spans[breakpoint as keyof RegionSpans];
    if (!Number.isInteger(value) || value < 1 || value >= options.length) {
      throw new Error(`Invalid ${breakpoint} grid span: ${value}.`);
    }
    return options[value];
  });
  if (Object.keys(columns).length !== 4) throw new Error("Grid regions require mobile, tablet, desktop and wide spans.");
  return <div {...props} className={`min-w-0 break-words ${classes.join(" ")} ${className}`} />;
}
