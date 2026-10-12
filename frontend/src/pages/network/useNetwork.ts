import { useCallback, useEffect, useRef, useState } from "react";
import { network } from "../../api/network";
import type { HubPoint, HubSummary } from "../../api/network";
import { POLL_MS, updateCache } from "./model";
import type { Cache } from "./model";

export type NetworkState =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "ready"; summary: HubSummary; cache: Cache; detail: HubPoint | null; at: number };

/**
 * Сводка по сети: опрос раз в 60 с без перезагрузки страницы. Данные последнего успешного опроса точки
 * держим в памяти, чтобы недоступную точку показывать ими. Ошибка hub после первого успеха не стирает экран.
 */
export function useNetwork(selected: string | null): [NetworkState, () => Promise<void>] {
  const [state, setState] = useState<NetworkState>({ kind: "loading" });
  const cache = useRef<Cache>({});
  const detailCache = useRef<Record<string, HubPoint>>({});
  const sel = useRef(selected);
  sel.current = selected;

  const load = useCallback(async () => {
    try {
      const summary = await network.summary();
      cache.current = updateCache(cache.current, summary.points);
      const id = sel.current;
      if (id) {
        try {
          const d = await network.point(id);
          if (d.status === "ok") detailCache.current[id] = d;
        } catch {
          /* подробности необязательны: остаётся прошлая лента */
        }
      }
      setState({
        kind: "ready", summary, cache: cache.current,
        detail: id ? detailCache.current[id] ?? null : null, at: Date.now(),
      });
    } catch (e) {
      setState((prev) => (prev.kind === "ready" ? prev : { kind: "error", message: e instanceof Error ? e.message : "Hub не отвечает." }));
    }
  }, []);

  useEffect(() => {
    void load();
    const t = setInterval(() => void load(), POLL_MS);
    return () => clearInterval(t);
  }, [load]);

  // Смена выбранной точки — сразу перечитываем подробности.
  const first = useRef(true);
  useEffect(() => {
    if (first.current) {
      first.current = false;
      return;
    }
    void load();
  }, [selected, load]);

  return [state, load];
}
