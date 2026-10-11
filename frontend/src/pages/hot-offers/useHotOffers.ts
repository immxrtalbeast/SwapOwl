import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../../api/client";
import type { Dish, HotAlgo, HotOffersResponse } from "../../api/types";
import { useLive } from "../../app/useLive";

export type HotOffersState =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "ready"; data: HotOffersResponse; dishNames: Record<string, string> };

/**
 * Горячие предложения выбранного алгоритма (null — основной по настройке сервера).
 * Перечитываются по hotoffers.changed, сбросу стенда и переподключению.
 */
export function useHotOffers(algo: HotAlgo | null): [HotOffersState, () => Promise<void>] {
  const [state, setState] = useState<HotOffersState>({ kind: "loading" });
  const seq = useRef(0);
  const load = useCallback(async () => {
    const mine = ++seq.current;
    try {
      const [data, dishes] = await Promise.all([api.hotOffers(algo ?? undefined), api.dishes()]);
      if (mine !== seq.current) return; // пришёл ответ на устаревший запрос (смена алгоритма)
      setState({ kind: "ready", data, dishNames: nameMap(dishes) });
    } catch (e) {
      if (mine !== seq.current) return;
      setState((prev) =>
        prev.kind === "ready" ? prev : { kind: "error", message: e instanceof Error ? e.message : "Не удалось загрузить горячие предложения" },
      );
    }
  }, [algo]);
  useLive(load, ["hotoffers.changed", "stand.reset"]);
  // useLive сам загружает только при монтировании и по событиям: смену алгоритма перечитываем здесь.
  const first = useRef(true);
  useEffect(() => {
    if (first.current) {
      first.current = false;
      return;
    }
    setState({ kind: "loading" }); // старый список не выдаём за список нового алгоритма
    void load();
  }, [load]);
  return [state, load];
}

function nameMap(dishes: Dish[]): Record<string, string> {
  return Object.fromEntries(dishes.map((d) => [d.id, d.name]));
}
