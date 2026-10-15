import { ApiError } from "../../api/types";
import type { DeliveryInput, SupplySuggestion } from "../../api/types";

/**
 * Тело поставки для строки предложения: количество и единица из строки, срок — всегда явно (поставка без срока не
 * принимается, FS-160): введённый вручную или обычный срок поставщика из строки предложения.
 */
export function buildAcceptBody(row: SupplySuggestion, id: string, createdAt: string, shelfLifeHours?: number): DeliveryInput {
  const hours = shelfLifeHours ?? row.shelf_life_hours ?? undefined;
  return {
    id,
    number: `supply-${id.slice(0, 8)}`,
    created_at: createdAt,
    items: [
      {
        product_id: row.product_id,
        quantity: String(row.quantity.qty),
        unit: row.quantity.unit,
        ...(hours !== undefined ? { shelf_life_hours: hours } : {}),
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
  return e instanceof ApiError && e.status === 400 && /shelf_life_hours|expires_on/.test(`${e.message} ${JSON.stringify(e.details)}`);
}

export function outcomeFromResult(replayed: boolean): AcceptOutcome {
  return replayed
    ? { kind: "replayed", message: "Поставка уже принята" }
    : { kind: "accepted", message: "Поставка принята" };
}

export function outcomeFromError(e: unknown): AcceptOutcome {
  if (isNoShelfLife(e)) {
    return { kind: "need-shelf-life", message: "Без срока поставку не принять: укажите срок хранения в часах и нажмите «Принять» ещё раз" };
  }
  return { kind: "error", message: e instanceof Error ? e.message : "Не удалось принять поставку" };
}
