import type { Qty } from "../../api/types";
import { formatQty } from "../../lib/format";

/** Количество для экрана «Поставки»: в кг/л не больше двух знаков («4,24 кг», а не «4,238 кг»). Остальное — общий форматтер. */
export function formatSupplyQty(q: Qty): string {
  if ((q.unit === "g" || q.unit === "ml") && Math.abs(q.qty) >= 1000) {
    return formatQty({ ...q, qty: Math.round(q.qty / 10) * 10 });
  }
  return formatQty(q);
}
