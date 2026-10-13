import type { SupplySuggestion } from "../../api/types";

// Общая шкала срочности (.ai/frontend-prompt.md): now → risk, soon → warn, planned → нейтральный тон.
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
  const hours = ms / 3_600_000;
  if (hours < 48) return `через ${Math.max(1, Math.round(hours))} ч`;
  return `через ${Math.round(hours / 24)} сут.`;
}
