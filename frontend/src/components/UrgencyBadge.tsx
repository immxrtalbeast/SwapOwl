import type { UrgencyDef } from "../lib/urgency";
import { toneVars } from "../lib/urgency";
import { URGENCY_ICONS } from "./urgencyIcons";

/** Бейдж срочности: высота 22, радиус 4, --type-caption, иконка 14. Иконка и метка — всегда. */
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
        height: 22,
        padding: "0 8px",
        borderRadius: 4,
        background: v.bg,
        color: v.fg,
        font: "var(--type-caption)",
        textTransform: "uppercase",
        letterSpacing: ".04em",
        whiteSpace: "nowrap",
      }}
    >
      <Icon size={14} aria-hidden="true" />
      {label ?? def.label}
    </span>
  );
}
