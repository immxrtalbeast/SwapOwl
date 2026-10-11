import { useCallback, useState } from "react";
import { api } from "../../api/client";
import type { MoneyReport } from "../../api/types";
import { useLive } from "../../app/useLive";

export type MoneyState =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "ready"; writeoffs: MoneyReport; shortfalls: MoneyReport };

const DAY_MS = 86_400_000;

/** Деньги за последние `days` суток стенда; перечитывается по stock.changed (и часам/сбросу стенда). */
export function useMoney(days: number): [MoneyState, () => Promise<void>] {
  const [state, setState] = useState<MoneyState>({ kind: "loading" });
  const load = useCallback(async () => {
    try {
      const clock = await api.clock();
      const nowMs = Date.parse(clock.now);
      const to = new Date(nowMs + 1000).toISOString();
      const from = new Date(nowMs - days * DAY_MS).toISOString();
      const [writeoffs, shortfalls] = await Promise.all([api.writeoffs(from, to), api.shortfalls(from, to)]);
      setState({ kind: "ready", writeoffs, shortfalls });
    } catch (e) {
      setState((prev) =>
        prev.kind === "ready" ? prev : { kind: "error", message: e instanceof Error ? e.message : "Не удалось загрузить данные" },
      );
    }
  }, [days]);
  useLive(load, ["stock.changed", "clock.changed", "stand.reset"]);
  return [state, load];
}
