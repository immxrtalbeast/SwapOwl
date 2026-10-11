import { useCallback, useState } from "react";
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
  const load = useCallback(async () => {
    try {
      const [data, dishes] = await Promise.all([api.hotOffers(algo ?? undefined), api.dishes()]);
      setState({ kind: "ready", data, dishNames: nameMap(dishes) });
    } catch (e) {
      setState((prev) =>
        prev.kind === "ready" ? prev : { kind: "error", message: e instanceof Error ? e.message : "Не удалось загрузить горячие предложения" },
      );
    }
  }, [algo]);
  useLive(load, ["hotoffers.changed", "stand.reset"]);
  return [state, load];
}

function nameMap(dishes: Dish[]): Record<string, string> {
  return Object.fromEntries(dishes.map((d) => [d.id, d.name]));
}
