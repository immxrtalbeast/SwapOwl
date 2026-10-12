import { useCallback, useEffect, useRef, useState } from "react";
import { network } from "../../api/network";
import type { HubPoint, HubSummary } from "../../api/network";
import { AB_POLL_MS, POLL_MS, abRunning, updateCache } from "./model";
import type { Cache } from "./model";

export type NetworkState =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "ready"; summary: HubSummary; cache: Cache; detail: HubPoint | null; at: number };

/**
 * Сводка по сети: опрос раз в 60 с (раз в 2,5 с, пока идёт A/B-прогон) без перезагрузки страницы. Данные последнего успешного опроса точки
 * держим в памяти, чтобы недоступную точку показывать ими. Ошибка hub после первого успеха не стирает экран.
 */
export function useNetwork(selected: string | null): [NetworkState, () => Promise<void>] {
  const [state, setState] = useState<NetworkState>({ kind: "loading" });
  const cache = useRef<Cache>({});
  const detailCache = useRef<Record<string, HubPoint>>({});
  const fast = useRef(false); // идёт A/B-прогон: опрос чаще
  const sel = useRef(selected);
  sel.current = selected;

  const load = useCallback(async () => {
    try {
      const summary = await network.summary();
      cache.current = updateCache(cache.current, summary.points);
      fast.current = abRunning(summary.points);
      const id = sel.current;
      if (id) {
        try {
          const d = await network.point(id);
          if (d.status === "ok") detailCache.current[id] = d;
        } catch {
          /* подробности необязательны: остаётся прошлая лента */
        }
      }
      // Выбор мог смениться, пока шёл опрос: подробности берём для текущего выбора, а не для начатого.
      const cur = sel.current;
      setState({
        kind: "ready", summary, cache: cache.current,
        detail: cur ? detailCache.current[cur] ?? null : null, at: Date.now(),
      });
    } catch (e) {
      setState((prev) => (prev.kind === "ready" ? prev : { kind: "error", message: e instanceof Error ? e.message : "Hub не отвечает." }));
    }
  }, []);

  useEffect(() => {
    let stopped = false;
    let t: ReturnType<typeof setTimeout>;
    const tick = async () => {
      await load();
      if (!stopped) t = setTimeout(() => void tick(), fast.current ? AB_POLL_MS : POLL_MS);
    };
    void tick();
    return () => {
      stopped = true;
      clearTimeout(t);
    };
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
