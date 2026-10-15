import type { OrderItemInput } from "../../api/types";

/**
 * Очередь заказов без связи (FS-177). Заказ с клиентским id кладётся в очередь, когда сети нет, и уходит, как только
 * она появилась. Дублей не будет: id тот же, а API идемпотентен — повтор того же тела возвращает сохранённый ответ
 * (`replayed: true`). Хранится в localStorage; если он недоступен, очередь живёт до перезагрузки страницы.
 */
export interface Queued {
  id: string;
  items: OrderItemInput[];
  /** Когда нажали «Оформить» (для показа «ждёт N мин»). */
  at: string;
}

export const OUTBOX_KEY = "swapowl.orders.outbox.v1";
export const OUTBOX_MAX = 20;

export interface Store {
  getItem(k: string): string | null;
  setItem(k: string, v: string): void;
}

const memory = new Map<string, string>();
const memStore: Store = { getItem: (k) => memory.get(k) ?? null, setItem: (k, v) => void memory.set(k, v) };

/** localStorage, а если он недоступен (приватный режим, запрет) — память вкладки. */
export function defaultStore(): Store {
  try {
    const s = window.localStorage;
    s.setItem("swapowl.probe", "1");
    return s;
  } catch {
    return memStore;
  }
}

function valid(q: unknown): q is Queued {
  const o = q as Queued;
  return (
    !!o && typeof o.id === "string" && o.id.length > 0 && typeof o.at === "string" && Array.isArray(o.items) && o.items.length > 0 &&
    o.items.every((i) => i && typeof i.dish_id === "string" && Number.isInteger(i.portions) && i.portions > 0)
  );
}

export function loadQueue(store: Store): Queued[] {
  try {
    const raw = store.getItem(OUTBOX_KEY);
    const v: unknown = raw ? JSON.parse(raw) : [];
    return Array.isArray(v) ? v.filter(valid).slice(0, OUTBOX_MAX) : [];
  } catch {
    return [];
  }
}

function save(store: Store, q: Queued[]): void {
  try {
    store.setItem(OUTBOX_KEY, JSON.stringify(q));
  } catch {
    /* очередь живёт в памяти страницы */
  }
}

/** Добавляет заказ; тот же id второй раз не добавляется (двойное нажатие — один заказ). Возвращает очередь. */
export function enqueue(store: Store, q: Queued): Queued[] {
  const cur = loadQueue(store);
  if (cur.some((x) => x.id === q.id)) return cur;
  const next = [...cur, q].slice(-OUTBOX_MAX);
  save(store, next);
  return next;
}

export function remove(store: Store, id: string): Queued[] {
  const next = loadQueue(store).filter((x) => x.id !== id);
  save(store, next);
  return next;
}

export type FlushOutcome =
  | { kind: "sent"; id: string; accepted: boolean; replayed: boolean }
  | { kind: "dropped"; id: string; reason: string };

/**
 * Отправляет очередь по порядку. Сбой сети — стоп (остальное уйдёт позже, ничего не теряется); ошибка API
 * (валидация, конфликт тела) — заказ снимается с очереди, чтобы не повторять заведомо негодное.
 */
export async function flush(
  store: Store,
  send: (id: string, items: OrderItemInput[]) => Promise<{ status: "accepted" | "rejected"; replayed: boolean }>,
  isNetworkError: (e: unknown) => boolean,
): Promise<FlushOutcome[]> {
  const out: FlushOutcome[] = [];
  for (const q of loadQueue(store)) {
    try {
      const r = await send(q.id, q.items);
      remove(store, q.id);
      out.push({ kind: "sent", id: q.id, accepted: r.status === "accepted", replayed: r.replayed });
    } catch (e) {
      if (isNetworkError(e)) break;
      remove(store, q.id);
      out.push({ kind: "dropped", id: q.id, reason: e instanceof Error ? e.message : "заказ не принят" });
    }
  }
  return out;
}
