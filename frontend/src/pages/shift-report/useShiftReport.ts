import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../../api/client";
import type { Dish, Order, MoneyReport, ShortagesReport, SupplySuggestion, ForecastReport } from "../../api/types";
import { useLive } from "../../app/useLive";
import { dayOf } from "./report";

export interface ShiftData {
  date: string;
  timeZone: string;
  dishes: (Dish & { price_minor?: number })[];
  accepted: Order[];
  rejected: Order[];
  /** Заказы за сутки прочитаны не до конца: упёрлись в MAX_PAGES страниц. */
  incomplete: boolean;
  writeoffs: MoneyReport;
  shortages: ShortagesReport;
  supply: SupplySuggestion[];
  forecast: ForecastReport;
}

export type ShiftState = { kind: "loading" } | { kind: "error"; message: string } | { kind: "ready"; data: ShiftData };

const MAX_PAGES = 10;

/** Заказы статуса за сутки `date`: страницами от новых к старым, пока не дошли до более ранних суток.
 *  complete — false, если лимит страниц кончился раньше, чем дошли до более ранних суток. */
async function ordersOfDay(status: Order["status"], date: string, tz: string): Promise<{ orders: Order[]; complete: boolean }> {
  const out: Order[] = [];
  let before: string | undefined;
  for (let i = 0; i < MAX_PAGES; i++) {
    const { orders } = await api.ordersPage(status, { limit: 500, before });
    if (orders.length === 0) return { orders: out, complete: true };
    out.push(...orders.filter((o) => dayOf(o.at, tz) === date));
    const last = orders[orders.length - 1];
    if (orders.length < 500 || dayOf(last.at, tz) < date) return { orders: out, complete: true };
    before = last.id;
  }
  return { orders: out, complete: false };
}

/** Данные отчёта за сутки `date` (по умолчанию — текущие сутки стенда). */
export function useShiftReport(date: string | null): [ShiftState, string | null, () => Promise<void>] {
  const [state, setState] = useState<ShiftState>({ kind: "loading" });
  const [today, setToday] = useState<string | null>(null);
  const seq = useRef(0);
  const load = useCallback(async () => {
    const mine = ++seq.current;
    try {
      const clock = await api.clock();
      const tz = clock.timezone || "UTC";
      const day = date ?? dayOf(clock.now, tz);
      setToday(dayOf(clock.now, tz));
      const [dishes, accepted, rejected, writeoffs, shortages, supply, forecast] = await Promise.all([
        api.dishes() as Promise<ShiftData["dishes"]>,
        ordersOfDay("accepted", day, tz),
        ordersOfDay("rejected", day, tz),
        api.writeoffs(day, day),
        api.shortages(day, day),
        api.supplySuggestions(),
        api.forecastWriteoffs(),
      ]);
      if (mine !== seq.current) return;
      // Отменённый заказ вернул продукты на склад — в «Продано» он не входит.
      const sold = accepted.orders.filter((o) => !o.cancelled_at);
      setState({
        kind: "ready",
        data: { date: day, timeZone: tz, dishes, accepted: sold, rejected: rejected.orders, incomplete: !accepted.complete || !rejected.complete, writeoffs, shortages, supply, forecast },
      });
    } catch (e) {
      if (mine !== seq.current) return;
      setState((p) => (p.kind === "ready" ? p : { kind: "error", message: e instanceof Error ? e.message : "Не удалось загрузить данные" }));
    }
  }, [date]);
  useLive(load, ["stock.changed", "clock.changed", "stand.reset"]);
  const first = useRef(true);
  useEffect(() => {
    if (first.current) { first.current = false; return; }
    setState({ kind: "loading" });
    void load();
  }, [load]);
  return [state, today, load];
}
