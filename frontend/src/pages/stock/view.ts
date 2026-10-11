import type { Alert, Batch, ProductStock } from "../../api/types";
import { batchState, worstBatchState } from "../../lib/urgency";
import type { BatchState } from "../../lib/urgency";

// Только сборка строк для таблицы: статусы приходят с бэкенда, фронт ничего не пересчитывает.
// /api/batches отдаёт status ok|expired; «истекает» и «спишется» — из /api/alerts (по номеру партии).

export type StockRow =
  | { kind: "product"; key: string; product: ProductStock; batches: Batch[]; state: BatchState; expanded: boolean }
  | { kind: "batch"; key: string; batch: Batch; state: BatchState };

export const productKey = (id: string) => `p:${id}`;
export const batchKey = (id: number) => `b:${id}`;

/** Состояние партии по шкале срочности: статус партии дополняется данными ленты предупреждений. */
export function batchStateOf(b: Batch, alerts: ReadonlyMap<number, Alert>): BatchState {
  const a = alerts.get(b.id);
  if (a && b.status !== "expired") return batchState({ remaining: b.remaining.qty, status: a.status, doomed: a.doomed });
  return batchState({ remaining: b.remaining.qty, status: b.status });
}

/** Плоский список: строка продукта и, если раскрыта, строки его партий (ближайший срок — первым). */
export function buildRows(
  products: readonly ProductStock[],
  batches: readonly Batch[],
  alerts: readonly Alert[],
  expanded: ReadonlySet<string>,
): StockRow[] {
  const byAlert = new Map(alerts.map((a) => [a.batch_id, a]));
  const rows: StockRow[] = [];
  for (const p of products) {
    const own = batches
      .filter((b) => b.product_id === p.id)
      .sort((a, b) => Date.parse(a.expires_at) - Date.parse(b.expires_at) || a.id - b.id);
    const states = own.map((b) => batchStateOf(b, byAlert));
    const open = expanded.has(p.id);
    rows.push({ kind: "product", key: productKey(p.id), product: p, batches: own, state: worstBatchState(states), expanded: open });
    if (open) own.forEach((b, i) => rows.push({ kind: "batch", key: batchKey(b.id), batch: b, state: states[i] }));
  }
  return rows;
}

/** Отпечаток строки для подсветки: меняется, когда меняется остаток, срок или состояние. */
export function signature(rows: readonly StockRow[]): Map<string, string> {
  const m = new Map<string, string>();
  for (const r of rows) {
    m.set(
      r.key,
      r.kind === "product"
        ? `${r.product.available.qty}|${r.product.batches_count}|${r.product.nearest_expires_at}|${r.state}`
        : `${r.batch.remaining.qty}|${r.batch.expires_at}|${r.state}`,
    );
  }
  return m;
}

/** Ключи строк, изменившихся с прошлой загрузки; при первой загрузке (prev = null) подсвечивать нечего. */
export function changedKeys(prev: ReadonlyMap<string, string> | null, next: ReadonlyMap<string, string>): Set<string> {
  const out = new Set<string>();
  if (!prev) return out;
  for (const [k, v] of next) if (prev.get(k) !== v) out.add(k);
  return out;
}
