import { formatQty, formatTimeLeft } from "../stock/format";
import { useAlerts } from "./useAlerts";
import { hotOffersUrl } from "../hot-offers/query";
import { alertUrgency, urgencyView } from "./urgency";

export function AlertsPage() {
  const [state, reload] = useAlerts();
  return (
    <section>
      <h1>Предупреждения</h1>
      {state.kind === "loading" && <p role="status">Загрузка…</p>}
      {state.kind === "error" && (
        <div role="alert">
          <p>Ошибка: {state.message}</p>
          <button onClick={() => void reload()}>Повторить</button>
        </div>
      )}
      {state.kind === "ready" && state.alerts.length === 0 && <p>Предупреждений нет: истекающих и недавно просроченных партий нет.</p>}
      {state.kind === "ready" && state.alerts.length > 0 && (
        <table style={{ width: "100%", borderCollapse: "collapse" }}>
          <thead>
            <tr>
              <th align="left">Продукт</th>
              <th align="right">Количество</th>
              <th align="left">Срок</th>
              <th align="left">Состояние</th>
              <th align="left">Что делать</th>
            </tr>
          </thead>
          <tbody>
            {state.alerts.map((a) => {
              const u = alertUrgency(a);
              const v = urgencyView(u);
              return (
                <tr key={a.batch_id} data-urgency={u} style={{ background: v.background, borderBottom: "1px solid #ddd" }}>
                  <td>{a.product_name}</td>
                  <td align="right" style={{ fontWeight: u === "doomed" ? 700 : 400, textDecoration: u === "expired" ? "line-through" : "none" }}>
                    {formatQty(a.remaining)}
                  </td>
                  <td title={new Date(a.expires_at).toLocaleString("ru-RU")}>{formatTimeLeft(a.time_left_seconds)}</td>
                  <td style={{ color: v.color }}>
                    <span aria-hidden="true">{v.icon} </span>
                    {v.label}
                  </td>
                  <td>{a.status === "expiring" && <a href={hotOffersUrl(a.product_id)}>Блюда, которые спасают</a>}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </section>
  );
}
