import { Badge, StatusBadge, type BadgeStatus } from "../components/Badge";
import { RoleSourceBadge } from "../components/RoleSourceBadge";
import { Tag } from "../components/Tag";
import type { ComponentEntry } from "./catalog";

const statuses: BadgeStatus[] = ["active", "invited", "deactivated", "locked", "revoked", "draft"];

export const badgeEntry: ComponentEntry = {
  name: "Badge / Tag",
  rule: "Include visible text alongside status marks. Direct roles are plain; delegated roles disclose the source organization on hover, tap or keyboard activation. Tags label metadata without implying status.",
  requiredStates: [...statuses, "neutral", "positive", "attention", "muted", "direct", "delegated", "tag"],
  examples: [
    ...statuses.map((status) => ({ name: status, render: () => <StatusBadge status={status} /> })),
    ...(["neutral", "positive", "attention", "muted"] as const).map((tone) => ({ name: tone, render: () => <Badge tone={tone}>{tone}</Badge> })),
    { name: "direct", render: () => <RoleSourceBadge roleKey="project.viewer" /> },
    { name: "delegated", render: () => <RoleSourceBadge roleKey="project.viewer" delegatedFrom="Example organization" /> },
    { name: "tag", render: () => <Tag>Production</Tag> },
  ],
};
