import { ApiError } from "../../api/types";
import type { DeliveryInput, SupplySuggestion } from "../../api/types";

/**
 * Тело поставки для строки предложения: количество и единица из строки, срок — по умолчанию у продукта
 * (shelf_life_hours не передаётся) или введённый вручную, если у продукта срока нет.
 */
export function buildAcceptBody(row: SupplySuggestion, id: string, createdAt: string, shelfLifeHours?: number): DeliveryInput {
  return {
    id,
    number: `supply-${id.slice(0, 8)}`,
    created_at: createdAt,
    items: [
      {
        product_id: row.product_id,
        quantity: String(row.quantity.qty),
        unit: row.quantity.unit,
        ...(shelfLifeHours !== undefined ? { shelf_life_hours: shelfLifeHours } : {}),
      },
    ],
  };
}

/** Ключ предложения: новая строка (другое количество) — новая поставка, та же строка — тот же id. */
export function offerKey(row: SupplySuggestion): string {
  return `${row.product_id}|${row.quantity.qty}|${row.quantity.unit}`;
}

export type AcceptOutcome =
  | { kind: "accepted"; message: string }
  | { kind: "replayed"; message: string }
  | { kind: "need-shelf-life"; message: string }
  | { kind: "error"; message: string };

export function isNoShelfLife(e: unknown): boolean {
  return e instanceof ApiError && e.status === 400 && /shelf_life_hours/.test(`${e.message} ${JSON.stringify(e.details)}`);
}

export function outcomeFromResult(replayed: boolean): AcceptOutcome {
  return replayed
    ? { kind: "replayed", message: "Поставка уже принята" }
    : { kind: "accepted", message: "Поставка принята" };
}

export function outcomeFromError(e: unknown): AcceptOutcome {
  if (isNoShelfLife(e)) {
    return { kind: "need-shelf-life", message: "У продукта не задан срок хранения: укажите его в часах и нажмите «Принять» ещё раз" };
  }
  return { kind: "error", message: e instanceof Error ? e.message : "Не удалось принять поставку" };
}
