import type { OrderItemInput } from "../../api/types";

/**
 * Очередь заказов без связи (FS-177). Заказ с клиентским id кладётся в очередь, когда сети нет, и уходит, как только
 * она появилась. Дублей не будет: id тот же, а API идемпотентен — повтор того же тела возвращает сохранённый ответ
 * (`replayed: true`). Хранится в localStorage; если он недоступен, очередь живёт до перезагрузки страницы.
 * У каждого заказа есть владелец (логин): чужой заказ с общего устройства уходит только под своей сессией.
 */
export interface Queued {
  id: string;
  items: OrderItemInput[];
  /** Когда нажали «Оформить» (для показа «ждёт N мин»). */
  at: string;
  /** Кто оформил: логин или «guest». Отправляется только под этим владельцем. */
  owner: string;
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
    !!o && typeof o.id === "string" && o.id.length > 0 && typeof o.at === "string" && typeof o.owner === "string" &&
    Array.isArray(o.items) && o.items.length > 0 &&
    o.items.every((i) => i && typeof i.dish_id === "string" && Number.isInteger(i.portions) && i.portions > 0)
  );
}

/** Вся очередь устройства. */
export function loadAll(store: Store): Queued[] {
  try {
    const raw = store.getItem(OUTBOX_KEY);
    const v: unknown = raw ? JSON.parse(raw) : [];
    return Array.isArray(v) ? v.filter(valid) : [];
  } catch {
    return [];
  }
}

/** Заказы одного владельца. */
export function loadQueue(store: Store, owner: string): Queued[] {
  return loadAll(store).filter((q) => q.owner === owner);
}

function save(store: Store, q: Queued[]): void {
  try {
    store.setItem(OUTBOX_KEY, JSON.stringify(q));
  } catch {
    /* очередь живёт в памяти страницы */
  }
}

export type EnqueueResult = { ok: true; queue: Queued[] } | { ok: false; reason: "full"; queue: Queued[] };

/**
 * Добавляет заказ; тот же id второй раз не добавляется (двойное нажатие — один заказ). Полная очередь заказ не
 * принимает и не вытесняет старые: человек должен узнать, что заказ не сохранён.
 */
export function enqueue(store: Store, q: Queued): EnqueueResult {
  const all = loadAll(store);
  if (all.some((x) => x.id === q.id)) return { ok: true, queue: loadQueue(store, q.owner) };
  if (all.length >= OUTBOX_MAX) return { ok: false, reason: "full", queue: loadQueue(store, q.owner) };
  save(store, [...all, q]);
  return { ok: true, queue: loadQueue(store, q.owner) };
}

export function remove(store: Store, id: string): void {
  save(store, loadAll(store).filter((x) => x.id !== id));
}

export type FlushOutcome =
  | { kind: "sent"; id: string; accepted: boolean; replayed: boolean }
  | { kind: "dropped"; id: string; reason: string };

/**
 * Отправляет заказы владельца по порядку. Временный сбой (нет сети, 5xx, 429, 401 — сессия вернётся) — стоп:
 * остальное уйдёт позже, ничего не теряется. Окончательная ошибка API (валидация, конфликт тела) снимает негодный
 * заказ с очереди, чтобы не повторять его вечно.
 */
export async function flush(
  store: Store,
  owner: string,
  send: (id: string, items: OrderItemInput[]) => Promise<{ status: "accepted" | "rejected"; replayed: boolean }>,
  isTemporary: (e: unknown) => boolean,
): Promise<FlushOutcome[]> {
  const out: FlushOutcome[] = [];
  for (const q of loadQueue(store, owner)) {
    try {
      const r = await send(q.id, q.items);
      remove(store, q.id);
      out.push({ kind: "sent", id: q.id, accepted: r.status === "accepted", replayed: r.replayed });
    } catch (e) {
      if (isTemporary(e)) break;
      remove(store, q.id);
      out.push({ kind: "dropped", id: q.id, reason: e instanceof Error ? e.message : "заказ не принят" });
    }
  }
  return out;
}
