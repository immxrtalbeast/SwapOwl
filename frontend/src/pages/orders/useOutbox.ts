import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../../api/client";
import { NetworkError } from "../../api/types";
import type { OrderItemInput } from "../../api/types";
import { defaultStore, enqueue, flush, loadQueue } from "./outbox";
import type { FlushOutcome, Queued } from "./outbox";

const RETRY_MS = 5000;

export interface OutboxApi {
  queue: Queued[];
  /** Итог последней отправки из очереди — показываем одной строкой, пока человек не закроет. */
  lastSent: FlushOutcome[];
  flushing: boolean;
  /** Поставить заказ в очередь (id тот же, что был у попытки). */
  put: (id: string, items: OrderItemInput[]) => void;
  flushNow: () => Promise<void>;
  dismiss: () => void;
}

/** Очередь заказов без связи: отправляется сама, когда браузер снова онлайн, и раз в 5 секунд, пока очередь не пуста. */
export function useOutbox(onSent?: () => void | Promise<void>): OutboxApi {
  const store = useRef(defaultStore());
  const [queue, setQueue] = useState<Queued[]>(() => loadQueue(store.current));
  const [lastSent, setLastSent] = useState<FlushOutcome[]>([]);
  const [flushing, setFlushing] = useState(false);
  const busy = useRef(false);
  const cb = useRef(onSent);
  cb.current = onSent;

  const flushNow = useCallback(async () => {
    if (busy.current || loadQueue(store.current).length === 0) return;
    busy.current = true;
    setFlushing(true);
    try {
      const out = await flush(store.current, (id, items) => api.placeOrder(id, items), (e) => e instanceof NetworkError);
      setQueue(loadQueue(store.current));
      if (out.length > 0) {
        setLastSent((prev) => [...prev, ...out]);
        await cb.current?.();
      }
    } finally {
      busy.current = false;
      setFlushing(false);
    }
  }, []);

  const put = useCallback((id: string, items: OrderItemInput[]) => {
    setQueue(enqueue(store.current, { id, items, at: new Date().toISOString() }));
  }, []);

  useEffect(() => {
    const on = () => void flushNow();
    window.addEventListener("online", on);
    void flushNow(); // очередь с прошлого сеанса
    return () => window.removeEventListener("online", on);
  }, [flushNow]);

  useEffect(() => {
    if (queue.length === 0) return;
    const t = window.setInterval(() => void flushNow(), RETRY_MS);
    return () => window.clearInterval(t);
  }, [queue.length, flushNow]);

  return { queue, lastSent, flushing, put, flushNow, dismiss: () => setLastSent([]) };
}
