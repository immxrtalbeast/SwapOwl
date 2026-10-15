import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../../api/client";
import { ApiError, NetworkError } from "../../api/types";
import type { OrderItemInput } from "../../api/types";
import { useAuth } from "../../auth/AuthContext";
import { defaultStore, enqueue, flush, loadQueue } from "./outbox";
import type { FlushOutcome, Queued } from "./outbox";

const RETRY_MS = 5000;

/** Временное: нет сети, сервер перезапускается или перегружен, сессия истекла (вернётся после входа). Заказ остаётся. */
export function isTemporary(e: unknown): boolean {
  if (e instanceof NetworkError) return true;
  return e instanceof ApiError && (e.status === 401 || e.status === 408 || e.status === 429 || e.status >= 500);
}

export interface OutboxApi {
  queue: Queued[];
  /** Итог последней отправки из очереди — показываем одной строкой, пока человек не закроет. */
  lastSent: FlushOutcome[];
  flushing: boolean;
  /** Поставить заказ в очередь (id тот же, что был у попытки). false — очередь полна, заказ не сохранён. */
  put: (id: string, items: OrderItemInput[]) => boolean;
  flushNow: () => Promise<void>;
  dismiss: () => void;
}

/** Очередь заказов без связи: отправляется сама, когда браузер снова онлайн, и раз в 5 секунд, пока очередь не пуста. */
export function useOutbox(onSent?: () => void | Promise<void>): OutboxApi {
  const { state } = useAuth();
  const owner = state.kind === "user" ? (state.login ?? "guest") : "guest";
  const store = useRef(defaultStore());
  const [queue, setQueue] = useState<Queued[]>(() => loadQueue(store.current, owner));
  const [lastSent, setLastSent] = useState<FlushOutcome[]>([]);
  const [flushing, setFlushing] = useState(false);
  const busy = useRef(false);
  const cb = useRef(onSent);
  cb.current = onSent;

  const flushNow = useCallback(async () => {
    if (busy.current || loadQueue(store.current, owner).length === 0) return;
    busy.current = true;
    setFlushing(true);
    try {
      const out = await flush(store.current, owner, (id, items) => api.placeOrder(id, items), isTemporary);
      setQueue(loadQueue(store.current, owner));
      if (out.length > 0) {
        setLastSent((prev) => [...prev, ...out]);
        await cb.current?.();
      }
    } finally {
      busy.current = false;
      setFlushing(false);
    }
  }, [owner]);

  const put = useCallback((id: string, items: OrderItemInput[]) => {
    const r = enqueue(store.current, { id, items, at: new Date().toISOString(), owner });
    setQueue(r.queue);
    return r.ok;
  }, [owner]);

  useEffect(() => {
    setQueue(loadQueue(store.current, owner));
    const on = () => void flushNow();
    window.addEventListener("online", on);
    void flushNow(); // очередь с прошлого сеанса
    return () => window.removeEventListener("online", on);
  }, [flushNow, owner]);

  useEffect(() => {
    if (queue.length === 0) return;
    const t = window.setInterval(() => void flushNow(), RETRY_MS);
    return () => window.clearInterval(t);
  }, [queue.length, flushNow]);

  return { queue, lastSent, flushing, put, flushNow, dismiss: () => setLastSent([]) };
}
