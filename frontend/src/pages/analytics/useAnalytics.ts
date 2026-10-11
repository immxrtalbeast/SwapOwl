import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../../api/client";
import type { BenchResult, ShortagesReport } from "../../api/types";
import { useLive } from "../../app/useLive";
import { moneyPeriod } from "../money/period";

export type AnalyticsState =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "ready"; shortages: ShortagesReport; bench: BenchResult | null };

/** Дефицит за последние `days` суток стенда и результат bench. Ошибка bench не ломает экран: цифры просто нет. */
export function useAnalytics(days: number): [AnalyticsState, () => Promise<void>] {
  const [state, setState] = useState<AnalyticsState>({ kind: "loading" });
  const seq = useRef(0);
  const load = useCallback(async () => {
    const mine = ++seq.current;
    try {
      const clock = await api.clock();
      const { from, to } = moneyPeriod(clock.now, days);
      const [shortages, bench] = await Promise.all([api.shortages(from, to), api.bench().catch(() => null)]);
      if (mine !== seq.current) return;
      setState({ kind: "ready", shortages, bench });
    } catch (e) {
      if (mine !== seq.current) return;
      setState((prev) =>
        prev.kind === "ready" ? prev : { kind: "error", message: e instanceof Error ? e.message : "Не удалось загрузить данные" },
      );
    }
  }, [days]);
  useLive(load, ["order.rejected", "stock.changed", "clock.changed", "stand.reset"]);
  const first = useRef(true);
  useEffect(() => {
    if (first.current) {
      first.current = false;
      return;
    }
    setState({ kind: "loading" });
    void load();
  }, [load]);
  return [state, load];
}
