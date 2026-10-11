import { useCallback, useState } from "react";
import { api } from "../../api/client";
import type { Alert } from "../../api/types";
import { useLive } from "../../app/useLive";

export type AlertsState =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "ready"; alerts: Alert[] };

/** Лента предупреждений: перечитывается по событиям срока, остатков и часов стенда. */
export function useAlerts(): [AlertsState, () => Promise<void>] {
  const [state, setState] = useState<AlertsState>({ kind: "loading" });
  const load = useCallback(async () => {
    try {
      setState({ kind: "ready", alerts: await api.alerts() });
    } catch (e) {
      setState((prev) =>
        prev.kind === "ready" ? prev : { kind: "error", message: e instanceof Error ? e.message : "Не удалось загрузить предупреждения" },
      );
    }
  }, []);
  useLive(load, ["batch.expiring", "batch.expired", "stock.changed", "clock.changed", "stand.reset"]);
  return [state, load];
}
