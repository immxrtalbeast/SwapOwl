import type { DishServings, Order, OrderMissing, OrderShortfall, OrderWriteoff } from "../../api/types";
import { NBSP, formatNumber, formatQty } from "../../lib/format";
import type { UrgencyDef } from "../../lib/urgency";

/** Статус заказа на шкале срочности: иконка и метка рядом с цветом. */
export const ORDER_STATES: Record<Order["status"], UrgencyDef> = {
  accepted: { tone: "ok", icon: "circle-check", label: "Принят" },
  rejected: { tone: "critical", icon: "ban", label: "Отклонён" },
};

const lower = (s: string) => (s ? s.charAt(0).toLowerCase() + s.slice(1) : s);

const upper = (s: string) => (s ? s.charAt(0).toUpperCase() + s.slice(1) : s);

/** Строка списания: «Лосось 500 г — партия №17». */
export function writeoffText(w: OrderWriteoff): string {
  return `${upper(w.product_name ?? w.product_id)} ${formatQty(w.qty)} — партия${NBSP}№${w.batch_id}`;
}

/** Списания по партиям; одна и та же пара «продукт — партия» не повторяется. */
export function writeoffLines(order: Pick<Order, "writeoffs">): string[] {
  const merged = new Map<string, OrderWriteoff>();
  for (const w of order.writeoffs) {
    const key = `${w.product_id}:${w.batch_id}`;
    const prev = merged.get(key);
    merged.set(key, prev ? { ...prev, qty: { ...prev.qty, qty: prev.qty.qty + w.qty.qty } } : w);
  }
  return [...merged.values()].map(writeoffText);
}

/** Причина отказа человеческим языком: «Не хватает: сливки 200 мл на заказ». */
export function rejectionReason(missing: readonly OrderMissing[]): string {
  if (missing.length === 0) return "Не хватает продуктов на заказ";
  return `Не хватает: ${missing.map((m) => `${lower(m.product_name)} ${formatQty(m.missing)}`).join(", ")} на заказ`;
}

/** Недостача заказа из кассы: касса уже продала блюдо, склад не покрыл. */
export function shortfallText(shortfalls: readonly OrderShortfall[] | undefined): string | null {
  if (!shortfalls || shortfalls.length === 0) return null;
  return `Не хватило на складе: ${shortfalls.map((s) => `${lower(s.product_name)} ${formatQty(s.quantity)}`).join(", ")}`;
}

/** «Принятые · 1 284» — total с сервера, а не размер загруженной страницы. */
export function sectionTitle(name: string, total: number): string {
  return `${name} · ${formatNumber(total)}`;
}

/** Блюда заказа: «Паста с лососем × 2». */
export function itemsText(order: Pick<Order, "items">, dishName: (id: string) => string): string {
  return order.items.map((it) => `${dishName(it.dish_id)} × ${it.portions}`).join(", ");
}

/** Добавляет страницу, не дублируя уже загруженные заказы. */
export function appendPage(rows: readonly Order[], next: readonly Order[]): Order[] {
  const seen = new Set(rows.map((o) => o.id));
  return [...rows, ...next.filter((o) => !seen.has(o.id))];
}

/** Подпись блюда в выборе заказа: доступные — с числом порций, из стоп-листа — с причиной (выбрать нельзя). */
export function dishOptionLabel(d: Pick<DishServings, "name" | "available" | "servings" | "missing">): string {
  if (!d.available) {
    const what = d.missing.map((m) => m.product_name.toLowerCase()).join(", ");
    return `${d.name} — стоп-лист${what ? ` (нет: ${what})` : ""}`;
  }
  return `${d.name} — можно ${d.servings}`;
}

/** Строки заказа с блюдом, ушедшим в стоп-лист после выбора: их нужно убрать, иначе заказ не оформить. */
export function stoppedLines(lines: readonly { dish_id: string }[], dishes: readonly Pick<DishServings, "id" | "name" | "available">[]): string[] {
  return lines.flatMap((l) => {
    const d = dishes.find((x) => x.id === l.dish_id);
    return d && !d.available ? [d.name] : [];
  });
}
