import type { DishServings } from "../../api/types";
import { formatQty } from "../stock/format";

// Текст причины для блюда в стоп-листе; только форматирование того, что прислал бэкенд.
export function reasonText(d: DishServings): string {
  if (d.missing.length === 0) return "Нет продуктов для порции";
  return d.missing
    .map((r) =>
      r.available.qty === 0
        ? `${r.product_name}: закончился или просрочен`
        : `${r.product_name}: не хватает ${formatQty(r.missing)} на порцию`,
    )
    .join("; ");
}

export function servingsLabel(d: DishServings): string {
  return d.available ? `${d.servings} порц.` : "нет в наличии";
}
