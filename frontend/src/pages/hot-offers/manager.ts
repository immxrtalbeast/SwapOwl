import type { DishServings, HotOffersResponse } from "../../api/types";

/** Блюда, которые менеджер может добавить: доступны (не в стоп-листе) и ещё не в предложениях, не добавлены и не скрыты. */
export function pickableDishes(menu: readonly DishServings[], data: HotOffersResponse): DishServings[] {
  const taken = new Set([
    ...data.offers.map((o) => o.dish_id),
    ...(data.manager_picks ?? []).map((p) => p.dish_id),
    ...(data.hidden ?? []).map((h) => h.dish_id),
  ]);
  return menu.filter((d) => d.available && !taken.has(d.id)).sort((a, b) => a.name.localeCompare(b.name, "ru"));
}

/** Карточка для зала: что предложить гостю и одна короткая причина без цифр партий. */
export interface HallCard {
  dishId: string;
  dish: string;
  why: string;
  byManager: boolean;
}

export function hallCards(data: HotOffersResponse): HallCard[] {
  const out: HallCard[] = data.offers.map((o) => {
    const r = o.reasons[0];
    // «Булочка — продать до завтра»: название в именительном падеже без склонения (FS-161).
    const why = o.manager_note || (r ? `${r.product_name} — продать ${untilWord(r.expires_at)}` : "");
    return { dishId: o.dish_id, dish: o.dish_name, why, byManager: !!o.pinned };
  });
  for (const p of data.manager_picks ?? []) out.push({ dishId: p.dish_id, dish: p.dish_name, why: p.note || "советует менеджер", byManager: true });
  return out;
}

function untilWord(iso: string, now: Date = new Date()): string {
  const h = (Date.parse(iso) - now.getTime()) / 3_600_000;
  if (h <= 12) return "сегодня";
  if (h <= 36) return "до завтра";
  return "в ближайшие дни";
}
