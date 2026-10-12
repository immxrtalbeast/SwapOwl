import type { Alert, MoneyReport, Qty } from "./types";

/** Цена за базовую единицу (копеек за г/мл/шт) по продуктам. */
export type UnitPrices = Record<string, number>;

/**
 * В API нет прайса: цену единицы выводим из отчёта о списаниях (стоимость ÷ количество по продукту).
 * Продукты, которые ни разу не списывали, цены не получают — экран показывает их как «без оценки».
 */
export function unitPricesFromWriteoffs(report: MoneyReport): UnitPrices {
  const qty: Record<string, number> = {};
  const cost: Record<string, number> = {};
  for (const r of report.by_product) {
    qty[r.product_id] = (qty[r.product_id] ?? 0) + r.quantity.qty;
    cost[r.product_id] = (cost[r.product_id] ?? 0) + r.cost_minor;
  }
  const out: UnitPrices = {};
  for (const id of Object.keys(qty)) if (qty[id] > 0 && cost[id] > 0) out[id] = cost[id] / qty[id];
  return out;
}

/** Стоимость количества в копейках; null — цены продукта нет. */
export function qtyCostMinor(productId: string, q: Qty, prices: UnitPrices): number | null {
  const p = prices[productId];
  return p === undefined ? null : Math.round(p * q.qty);
}

export interface AtRisk {
  totalMinor: number;
  /** Партии «истекает», для которых нет цены: в сумму не вошли. */
  unpriced: number;
}

/** Под риском: остатки партий со статусом «истекает» (просроченные уже потеряны и сюда не входят). */
export function atRiskFromAlerts(alerts: readonly Alert[], prices: UnitPrices): AtRisk {
  let totalMinor = 0;
  let unpriced = 0;
  for (const a of alerts) {
    if (a.status !== "expiring") continue;
    const c = qtyCostMinor(a.product_id, a.remaining, prices);
    if (c === null) unpriced++;
    else totalMinor += c;
  }
  return { totalMinor, unpriced };
}

/** Дата YYYY-MM-DD момента `nowIso` в часовом поясе ресторана (формат `by_day` из отчётов). */
export function restaurantDay(nowIso: string, timezone: string): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone: timezone, year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date(nowIso));
}

/** Сумма за сегодняшний день ресторана из отчёта по дням. */
export function todayMinor(report: MoneyReport, nowIso: string, timezone: string): number {
  const key = restaurantDay(nowIso, timezone);
  return report.by_day.find((d) => d.date === key)?.cost_minor ?? 0;
}
