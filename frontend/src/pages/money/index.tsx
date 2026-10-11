import { useState } from "react";
import type { MoneyReport } from "../../api/types";
import { formatQty } from "../stock/format";
import { formatRub, reasonLabel } from "./format";
import { useMoney } from "./useMoney";

const PERIODS = [
  { days: 1, label: "Сутки" },
  { days: 7, label: "7 дней" },
  { days: 30, label: "30 дней" },
];

function Section({ title, report, withReason }: { title: string; report: MoneyReport; withReason: boolean }) {
  return (
    <section aria-label={title}>
      <h2>{title}</h2>
      <p data-testid={`total-${withReason ? "writeoffs" : "shortfalls"}`} style={{ fontSize: "1.6em", fontWeight: 700 }}>
        {formatRub(report.total_minor)}
      </p>
      {report.by_product.length === 0 ? (
        <p>За период ничего нет.</p>
      ) : (
        <>
          <table style={{ width: "100%", borderCollapse: "collapse" }}>
            <thead>
              <tr>
                <th align="left">Продукт</th>
                {withReason && <th align="left">Причина</th>}
                <th align="right">Количество</th>
                <th align="right">Стоимость</th>
              </tr>
            </thead>
            <tbody>
              {report.by_product.map((r) => (
                <tr key={`${r.product_id}/${r.reason ?? ""}`} style={{ borderBottom: "1px solid #ddd" }}>
                  <td>{r.product_name}</td>
                  {withReason && <td>{reasonLabel(r.reason)}</td>}
                  <td align="right">{formatQty(r.quantity)}</td>
                  <td align="right">{formatRub(r.cost_minor)}</td>
                </tr>
              ))}
            </tbody>
          </table>
          <h3>По дням</h3>
          <table style={{ width: "100%", borderCollapse: "collapse" }}>
            <tbody>
              {report.by_day.map((d) => (
                <tr key={d.date} style={{ borderBottom: "1px solid #ddd" }}>
                  <td>{new Date(`${d.date}T00:00:00`).toLocaleDateString("ru-RU", { day: "numeric", month: "long" })}</td>
                  <td align="right">{formatRub(d.cost_minor)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}
    </section>
  );
}

export function MoneyPage() {
  const [days, setDays] = useState(7);
  const [state, reload] = useMoney(days);
  return (
    <section>
      <h1>Деньги</h1>
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
        <>
          <Section title="Списано за период" report={state.writeoffs} withReason />
          <Section title="Расхождения кассы" report={state.shortfalls} withReason={false} />
        </>
      )}
    </section>
  );
}
