import { useCallback, useState } from "react";
import { api } from "../../api/client";
import type { SupplySuggestion } from "../../api/types";
import { useLive } from "../../app/useLive";

export type SupplyState =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "ready"; rows: SupplySuggestion[] };

/** Предложение поставок: перечитывается по изменению остатков, часов и сбросу стенда. */
export function useSupply(): [SupplyState, () => Promise<void>] {
  const [state, setState] = useState<SupplyState>({ kind: "loading" });
  const load = useCallback(async () => {
    try {
      setState({ kind: "ready", rows: await api.supplySuggestions() });
    } catch (e) {
      setState((prev) =>
        prev.kind === "ready" ? prev : { kind: "error", message: e instanceof Error ? e.message : "Не удалось загрузить предложение поставок" },
      );
    }
  }, []);
  useLive(load, ["stock.changed", "clock.changed", "stand.reset"]);
  return [state, load];
}
