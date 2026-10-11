import { useState } from "react";
import { formatQty } from "../stock/format";
import { benchSentence } from "./benchText";
import { useAnalytics } from "./useAnalytics";

const PERIODS = [
  { days: 1, label: "Сутки" },
  { days: 7, label: "7 дней" },
  { days: 30, label: "30 дней" },
];

export function AnalyticsPage() {
  const [days, setDays] = useState(7);
  const [state, reload] = useAnalytics(days);
  const bench = state.kind === "ready" ? benchSentence(state.bench) : null;
  return (
    <section>
      <h1>Аналитика</h1>
      {bench && (
        <p data-testid="bench" style={{ fontSize: "1.2em", fontWeight: 600 }}>
          {bench}
        </p>
      )}
      <div role="group" aria-label="Период">
        {PERIODS.map((p) => (
          <button key={p.days} aria-pressed={p.days === days} onClick={() => setDays(p.days)}>
            {p.label}
          </button>
        ))}
      </div>
      {state.kind === "loading" && <p role="status">Загрузка…</p>}
      {state.kind === "error" && (
        <div role="alert">
          <p>Ошибка: {state.message}</p>
          <button onClick={() => void reload()}>Повторить</button>
        </div>
      )}
      {state.kind === "ready" && (
        <section aria-label="Дефицит">
          <h2>Дефицит: чего не хватило заказам</h2>
          <p data-testid="rejected-total">Отклонённых заказов за период: {state.shortages.rejected_orders}</p>
          {state.shortages.by_product.length === 0 ? (
            <p>За период отказов из-за нехватки продуктов не было.</p>
          ) : (
            <table style={{ width: "100%", borderCollapse: "collapse" }}>
              <thead>
                <tr>
                  <th align="left">Продукт</th>
                  <th align="right">Отклонённых заказов</th>
                  <th align="right">Не хватило</th>
                </tr>
              </thead>
              <tbody>
                {state.shortages.by_product.map((r) => (
                  <tr key={r.product_id} style={{ borderBottom: "1px solid #ddd" }}>
                    <td>{r.product_name}</td>
                    <td align="right">{r.rejected_orders}</td>
                    <td align="right">{formatQty(r.missing)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </section>
      )}
    </section>
  );
}
