import { useCallback, useState } from "react";
import { api } from "../../api/client";
import type { ReceivingBatch } from "../../api/types";
import { useLive } from "../../app/useLive";

export type ReceivingState =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "ready"; rows: ReceivingBatch[] };

/** Партии с неподтверждённым сроком: перечитываются по изменению остатков, часов и сбросу стенда. */
export function useReceiving(): [ReceivingState, () => Promise<void>] {
  const [state, setState] = useState<ReceivingState>({ kind: "loading" });
  const load = useCallback(async () => {
    try {
      setState({ kind: "ready", rows: await api.receiving() });
    } catch (e) {
      setState((prev) =>
        prev.kind === "ready" ? prev : { kind: "error", message: e instanceof Error ? e.message : "Не удалось загрузить приёмку" },
      );
    }
  }, []);
  useLive(load, ["stock.changed", "clock.changed", "stand.reset"]);
  return [state, load];
}
