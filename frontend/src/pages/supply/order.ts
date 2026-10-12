import type { SupplyOrderInput, SupplyOrderStatus, SupplySuggestion } from "../../api/types";
import { offerKey } from "./accept";

/** Тело заказа поставщику: выбранные строки предложения, количество в базовых единицах продукта. */
export function buildOrderBody(rows: SupplySuggestion[], id: string): SupplyOrderInput {
  return { id, items: rows.map((r) => ({ product_id: r.product_id, qty: r.quantity.qty })) };
}

/** Ключ выбора: тот же набор строк с теми же количествами — тот же id, повтор не создаёт второй заказ. */
export function selectionKey(rows: SupplySuggestion[]): string {
  return rows.map(offerKey).sort().join(";");
}

export const ORDER_STATUS_TEXT: Record<SupplyOrderStatus, string> = {
  sent: "отправлен",
  delivered: "доставлен в iiko",
  error: "ошибка доставки",
};

/** Опрашиваем, пока адаптер не подтвердил доставку; ошибка — адаптер повторяет сам, поэтому тоже ждём. */
export function needsPolling(status: SupplyOrderStatus): boolean {
  return status !== "delivered";
}
