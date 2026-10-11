import type { HotOffer } from "../../api/types";
import { formatQty } from "../stock/format";

/** «до 18:30 · 12.10» по записи времени ресторана из ответа (пояс из строки, не браузера). */
export function formatUntil(iso: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}:\d{2})/.exec(iso);
  return m ? `до ${m[4]} · ${m[3]}.${m[2]}` : "";
}

/** Блюда, у которых среди причин есть продукт; без продукта — все. Только отбор, расчёта нет. */
export function filterByProduct(offers: HotOffer[], productId: string | null): HotOffer[] {
  return productId ? offers.filter((o) => o.reasons.some((r) => r.product_id === productId)) : offers;
}

export function portionsWord(n: number): string {
  const m10 = n % 10;
  const m100 = n % 100;
  if (m10 === 1 && m100 !== 11) return "порция";
  if (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14)) return "порции";
  return "порций";
}

/** Подпись-пометка первой карточки: «−1,4 кг в мусор» — сумма риска по причинам в единице первой причины. */
export function handNote(offer: HotOffer): string | undefined {
  const first = offer.reasons[0];
  if (!first) return undefined;
  const qty = offer.reasons.filter((r) => r.at_risk.unit === first.at_risk.unit).reduce((s, r) => s + r.at_risk.qty, 0);
  return qty > 0 ? `−${formatQty({ qty, unit: first.at_risk.unit })} в мусор` : undefined;
}
