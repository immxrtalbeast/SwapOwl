import { useCallback, useState } from "react";
import { api } from "../../api/client";
import { atRiskFromAlerts, todayMinor, unitPricesFromWriteoffs } from "../../api/today";
import type { AtRisk, UnitPrices } from "../../api/today";
import type { Alert } from "../../api/types";
import { useLive } from "../../app/useLive";
import { moneyPeriod } from "../money/period";

const PRICE_DAYS = 30;
const EVENTS = ["batch.expiring", "batch.expired", "stock.changed", "clock.changed", "stand.reset"];

export type RiskState =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "ready"; alerts: Alert[]; prices: UnitPrices; atRisk: AtRisk; today: number; week: number };

/** Деньги экрана «Сегодня»: предупреждения + цены из списаний за 30 суток + списано за сутки и 7 дней. */
export function useRisk(): [RiskState, () => Promise<void>] {
  const [state, setState] = useState<RiskState>({ kind: "loading" });
  const load = useCallback(async () => {
    try {
      const clock = await api.clock();
      const p = (d: number) => moneyPeriod(clock.now, d);
      const [alerts, month, week] = await Promise.all([
        api.alerts(),
        api.writeoffs(p(PRICE_DAYS).from, p(PRICE_DAYS).to),
        api.writeoffs(p(7).from, p(7).to),
      ]);
      const prices = unitPricesFromWriteoffs(month);
      setState({ kind: "ready", alerts, prices, atRisk: atRiskFromAlerts(alerts, prices), today: todayMinor(week, clock.now, clock.timezone), week: week.total_minor });
    } catch (e) {
      setState((prev) => (prev.kind === "ready" ? prev : { kind: "error", message: e instanceof Error ? e.message : "Не удалось загрузить данные" }));
    }
  }, []);
  useLive(load, EVENTS);
  return [state, load];
}
