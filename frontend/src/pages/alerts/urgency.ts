import type { Alert } from "../../api/types";

// Шкала срочности партии (docs/design/design-system.md): doomed → risk, expiring → warn, expired → critical.
// Статус и doomed приходят с бэкенда; здесь только выбор тона. Цвета — токены дизайн-системы с запасными значениями.
export type Urgency = "doomed" | "expiring" | "expired";

export interface UrgencyView {
  urgency: Urgency;
  label: string;
  icon: string;
  color: string;
  background: string;
  solid: string;
}

const VIEWS: Record<Urgency, UrgencyView> = {
  expired: { urgency: "expired", label: "Просрочена", icon: "✕", color: "var(--urgency-critical-fg, #b0152c)", background: "var(--urgency-critical-bg, #fde8eb)", solid: "var(--urgency-critical-solid, #b8172f)" },
  doomed: { urgency: "doomed", label: "Спишется", icon: "⌫", color: "var(--urgency-risk-fg, #a3330a)", background: "var(--urgency-risk-bg, #fde8de)", solid: "var(--urgency-risk-solid, #c8440e)" },
  expiring: { urgency: "expiring", label: "Истекает", icon: "⧗", color: "var(--urgency-warn-fg, #7f4600)", background: "var(--urgency-warn-bg, #fff1d6)", solid: "var(--urgency-warn-solid, #f2a516)" },
};

export function alertUrgency(a: Pick<Alert, "status" | "doomed">): Urgency {
  if (a.status === "expired") return "expired";
  return a.doomed ? "doomed" : "expiring";
}

export function urgencyView(u: Urgency): UrgencyView {
  return VIEWS[u];
}

export interface AlertCounts {
  expiring: number;
  doomed: number;
  expired: number;
}

/** Счётчики шапки: «N истекают» (warn) и «N спишутся» (risk); просроченные считаются отдельно. */
export function countAlerts(alerts: readonly Alert[]): AlertCounts {
  const c: AlertCounts = { expiring: 0, doomed: 0, expired: 0 };
  for (const a of alerts) c[alertUrgency(a)]++;
  return c;
}
