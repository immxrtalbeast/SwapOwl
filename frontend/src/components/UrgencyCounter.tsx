import type { UrgencyDef } from "../lib/urgency";
import { toneVars } from "../lib/urgency";
import { URGENCY_ICONS } from "./urgencyIcons";

interface Props {
  def: UrgencyDef;
  n: number;
  /** Подпись после числа: «истекают», «спишутся». */
  text: string;
  href?: string;
}

/** Счётчик в шапке и на меню: заливка -solid, число и подпись, иконка. */
export function UrgencyCounter({ def, n, text, href }: Props) {
  const v = toneVars(def.tone);
  const Icon = URGENCY_ICONS[def.icon];
  const style = {
    display: "inline-flex",
    alignItems: "center",
    gap: 6,
    padding: "2px 10px",
    borderRadius: "var(--radius-pill)",
    background: v.solid,
    color: v.onSolid,
    font: "var(--type-label)",
    whiteSpace: "nowrap",
    textDecoration: "none",
  } as const;
  const body = (
    <>
      <Icon size={14} aria-hidden="true" />
      <span className="so-num" style={{ color: "inherit" }}>{n}</span> {text}
    </>
  );
  return href ? (
    <a href={href} data-tone={def.tone} style={style}>{body}</a>
  ) : (
    <span data-tone={def.tone} style={style}>{body}</span>
  );
}
