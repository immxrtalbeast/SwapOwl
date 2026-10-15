import type { UnitPrices } from "../../api/today";
import { qtyCostMinor } from "../../api/today";
import type { HotOffer, Qty } from "../../api/types";

/** Во сколько раз можно продать сверх обычного темпа. Допущение, уточнить у ресторана. */
export const MAX_OVERSELL = 2;

/** Доля стоимости остатка, которую спасает действие. Допущение, уточнить у ресторана. */
export const SAVE_SHARE = { discount: 0.8, staff: 1, prep: 0.7 } as const;
export type LeftoverAction = keyof typeof SAVE_SHARE;

export interface Realistic {
  /** Цель сервера на сегодня (или на ближайшую смену). */
  target: number;
  /** «Реально продать». */
  real: number;
  leftoverPortions: number;
  /** Остаток главного продукта, не попавший в реальную цель; null — пропорцию не посчитать. */
  leftover: { name: string; productId: string; qty: Qty } | null;
}

/** Цель больше 2× обычного → реально продать 2× (не меньше обычного + 1), остальное — действие. Иначе null. */
export function realisticTarget(o: HotOffer): Realistic | null {
  const target = o.target_today > 0 ? o.target_today : o.target_portions;
  if (target <= 0 || o.usual_portions < 0 || target <= MAX_OVERSELL * o.usual_portions) return null;
  const real = Math.max(MAX_OVERSELL * o.usual_portions, o.usual_portions + 1);
  if (real >= target) return null;
  const leftoverPortions = target - real;
  const first = o.reasons[0];
  const leftover = first
    ? {
        name: first.product_name,
        productId: first.product_id,
        qty: { qty: Math.round((first.at_risk.qty * leftoverPortions) / target), unit: first.at_risk.unit },
      }
    : null;
  return { target, real, leftoverPortions, leftover };
}

/** «не спишем ≈ X ₽» копейками по действию; null — нет цены продукта. */
export function actionSavedMinor(r: Realistic, action: LeftoverAction, prices: UnitPrices): number | null {
  if (!r.leftover) return null;
  const c = qtyCostMinor(r.leftover.productId, r.leftover.qty, prices);
  return c === null ? null : Math.round(c * SAVE_SHARE[action]);
}

export const ACTION_LABELS: Record<LeftoverAction, string> = {
  discount: "скидка 20 % на блюдо",
  staff: "в стафф-питание",
  prep: "в заготовку (заморозка/полуфабрикат)",
};
