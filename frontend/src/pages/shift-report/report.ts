import type { Dish, Order } from "../../api/types";

export interface DishLine {
  dish_id: string;
  name: string;
  portions: number;
  /** Копейки; null — цена блюда API не отдаёт (в GET /api/dishes нет price_minor). */
  minor: number | null;
}

/** Календарная дата YYYY-MM-DD момента `iso` в часовом поясе стенда. */
export function dayOf(iso: string, timeZone: string): string {
  return new Intl.DateTimeFormat("sv-SE", { timeZone, year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date(iso));
}

/** Порции по блюдам за `date`; ₽ = порции × цена блюда, если цена известна. */
export function dishLines(orders: Order[], dishes: (Pick<Dish, "id" | "name"> & { price_minor?: number })[], date: string, timeZone: string): DishLine[] {
  const byId = new Map(dishes.map((d) => [d.id, d]));
  const acc = new Map<string, number>();
  for (const o of orders) {
    if (dayOf(o.at, timeZone) !== date) continue;
    for (const it of o.items) acc.set(it.dish_id, (acc.get(it.dish_id) ?? 0) + it.portions);
  }
  return [...acc.entries()]
    .map(([id, portions]) => {
      const d = byId.get(id);
      const price = d?.price_minor;
      return { dish_id: id, name: d?.name ?? id, portions, minor: typeof price === "number" ? price * portions : null };
    })
    .sort((a, b) => (b.minor ?? 0) - (a.minor ?? 0) || b.portions - a.portions);
}

/** Сумма в копейках; null, если ни у одной строки нет цены. */
export function sumMinor(lines: DishLine[]): number | null {
  const priced = lines.filter((l) => l.minor !== null);
  return priced.length === 0 ? null : priced.reduce((s, l) => s + (l.minor ?? 0), 0);
}

export function sumPortions(lines: DishLine[]): number {
  return lines.reduce((s, l) => s + l.portions, 0);
}

/** Соседние сутки для «‹ / ›» у выбора даты: YYYY-MM-DD ± n дней (календарно, без часовых поясов). */
export function shiftDay(day: string, n: number): string {
  const d = new Date(`${day}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + n);
  return d.toISOString().slice(0, 10);
}
