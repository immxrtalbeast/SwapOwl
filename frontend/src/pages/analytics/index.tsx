import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import type { BenchResult, ShortagesReport } from "../../api/types";
import { useLive } from "../../app/useLive";
import { formatQty } from "../stock/format";
import { moneyPeriod } from "../money/period";
import { benchSentence } from "./benchText";

type State = { kind: "loading" } | { kind: "error"; message: string } | { kind: "ready"; shortages: ShortagesReport };

export function AnalyticsPage() {
  const [state, setState] = useState<State>({ kind: "loading" });
  const [bench, setBench] = useState<BenchResult | null>(null);

  const load = useCallback(async () => {
    try {
      const clock = await api.clock();
      const { from, to } = moneyPeriod(clock.now, 7);
      setState({ kind: "ready", shortages: await api.shortages(from, to) });
    } catch (e) {
      setState((prev) => (prev.kind === "ready" ? prev : { kind: "error", message: e instanceof Error ? e.message : "Не удалось загрузить данные" }));
    }
  }, []);
  useLive(load, ["stock.changed", "clock.changed", "stand.reset"]);

  useEffect(() => {
    api.bench().then(setBench).catch(() => setBench(null)); // нет ответа — цифры нет
  }, []);

  const sentence = benchSentence(bench);
  return (
    <section>
      <h1>Аналитика</h1>
      {sentence && <p data-testid="bench">{sentence}</p>}
      <h2>Дефицит за 7 дней</h2>
      <p>Продукты, из-за нехватки которых отклонялись заказы.</p>
      {state.kind === "loading" && <p role="status">Загрузка…</p>}
      {state.kind === "error" && (
        <div role="alert">
          <p>Ошибка: {state.message}</p>
          <button onClick={() => void load()}>Повторить</button>
        </div>
      )}
      {state.kind === "ready" &&
        (state.shortages.by_product.length === 0 ? (
          <p>За период отказов из-за нехватки не было.</p>
        ) : (
          <table style={{ width: "100%", borderCollapse: "collapse" }}>
            <thead>
              <tr>
                <th align="left">Продукт</th>
                <th align="right">Отклонённых заказов</th>
                <th align="right">Не хватало</th>
              </tr>
            </thead>
            <tbody>
              {state.shortages.by_product.map((r) => (
                <tr key={r.product_id} style={{ borderBottom: "1px solid #ddd" }}>
                  <td>{r.product_name}</td>
                  <td align="right">{r.orders}</td>
                  <td align="right">{formatQty(r.missing)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        ))}
    </section>
  );
}
