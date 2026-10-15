import type { UrgencyDef } from "../lib/urgency";
import { toneVars } from "../lib/urgency";
import { URGENCY_ICONS } from "./urgencyIcons";

/** Бейдж срочности: высота 24, капсула, 12 px полужирный без капса, иконка 14. Иконка и метка — всегда. */
export function UrgencyBadge({ def, label }: { def: UrgencyDef; label?: string }) {
  const v = toneVars(def.tone);
  const Icon = URGENCY_ICONS[def.icon];
  return (
    <span
      data-tone={def.tone}
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: 4,
        height: 24,
        padding: "0 10px 0 8px",
        borderRadius: "var(--radius-pill)",
        background: v.bg,
        color: v.fg,
        font: "600 12px/16px var(--font-body)",
        whiteSpace: "nowrap",
      }}
    >
      <Icon size={14} aria-hidden="true" />
      {label ?? def.label}
    </span>
  );
}
