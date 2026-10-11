import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../../api/client";
import type { MoneyReport } from "../../api/types";
import { useLive } from "../../app/useLive";
import { moneyPeriod } from "./period";

export type MoneyState =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "ready"; writeoffs: MoneyReport; shortfalls: MoneyReport };

/** Деньги за последние `days` суток стенда; перечитывается по stock.changed (и часам/сбросу стенда) и при смене периода. */
export function useMoney(days: number): [MoneyState, () => Promise<void>] {
  const [state, setState] = useState<MoneyState>({ kind: "loading" });
  const seq = useRef(0);
  const load = useCallback(async () => {
    const mine = ++seq.current;
    try {
      const clock = await api.clock();
      const { from, to } = moneyPeriod(clock.now, days);
      const [writeoffs, shortfalls] = await Promise.all([api.writeoffs(from, to), api.shortfalls(from, to)]);
      if (mine !== seq.current) return; // ответ на устаревший запрос (смена периода)
      setState({ kind: "ready", writeoffs, shortfalls });
    } catch (e) {
      if (mine !== seq.current) return;
      setState((prev) =>
        prev.kind === "ready" ? prev : { kind: "error", message: e instanceof Error ? e.message : "Не удалось загрузить данные" },
      );
    }
  }, [days]);
  useLive(load, ["stock.changed", "clock.changed", "stand.reset"]);
  // useLive сам загружает только при монтировании и по событиям: смену периода перечитываем здесь.
  const first = useRef(true);
  useEffect(() => {
    if (first.current) {
      first.current = false;
      return;
    }
    setState({ kind: "loading" }); // суммы за старый период не выдаём за новый
    void load();
  }, [load]);
  return [state, load];
}
