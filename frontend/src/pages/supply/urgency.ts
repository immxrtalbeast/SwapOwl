import type { SupplySuggestion } from "../../api/types";

// Общая шкала срочности (docs/design/design-system.md): now → risk, soon → warn, planned → нейтральный тон.
export type SupplyUrgency = SupplySuggestion["urgency"];

export interface SupplyUrgencyView {
  label: string;
  icon: string;
  color: string;
  background: string;
}

const VIEWS: Record<SupplyUrgency, SupplyUrgencyView> = {
  now: { label: "Заказать сейчас", icon: "⌫", color: "var(--urgency-risk-fg, #a3330a)", background: "var(--urgency-risk-bg, #fde8de)" },
  soon: { label: "Скоро", icon: "⧗", color: "var(--urgency-warn-fg, #7f4600)", background: "var(--urgency-warn-bg, #fff1d6)" },
  planned: { label: "По плану", icon: "○", color: "var(--urgency-ok-fg, #1f5f3a)", background: "var(--urgency-ok-bg, #e8f4ec)" },
};

export function supplyUrgencyView(u: SupplyUrgency): SupplyUrgencyView {
  return VIEWS[u];
}

/** При нулевом пригодном запасе — «закончилось», а не время. «через 5 ч» / «через 2 сут.» до обнуления запаса; null — расхода нет. */
export function formatStockout(iso: string | null, nowMs: number, usableQty?: number): string {
  if (usableQty !== undefined && usableQty <= 0) return "закончилось";
  if (!iso) return "расхода нет";
  const ms = new Date(iso).getTime() - nowMs;
  if (ms <= 0) return "уже закончился";
  // Одна шкала без «сут.»: до суток — часы, дальше — целые дни словом («через 2 дня»), точное время — строкой ниже.
  const hours = ms / 3_600_000;
  if (hours < 24) return `через ${Math.max(1, Math.round(hours))} ч`;
  const days = Math.round(hours / 24);
  return `через ${days} ${daysWord(days)}`;
}

/** 1 день, 2 дня, 5 дней. */
export function daysWord(n: number): string {
  const m10 = n % 10, m100 = n % 100;
  if (m10 === 1 && m100 !== 11) return "день";
  if (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14)) return "дня";
  return "дней";
}
