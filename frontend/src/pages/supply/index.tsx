import { formatQty } from "../stock/format";
import { formatStockout, supplyUrgencyView } from "./urgency";
import { useSupply } from "./useSupply";

export function SupplyPage() {
  const [state, reload] = useSupply();
  const nowMs = Date.now();
  return (
    <section>
      <h1>Поставки</h1>
      {state.kind === "loading" && <p role="status">Загрузка…</p>}
      {state.kind === "error" && (
        <div role="alert">
          <p>Ошибка: {state.message}</p>
          <button onClick={() => void reload()}>Повторить</button>
        </div>
      )}
      {state.kind === "ready" && state.rows.length === 0 && <p>Закупать ничего не нужно: запаса хватает на срок доставки и дни покрытия.</p>}
      {state.kind === "ready" && state.rows.length > 0 && (
        <table style={{ width: "100%", borderCollapse: "collapse" }}>
          <thead>
            <tr>
              <th align="left">Продукт</th>
              <th align="right">К заказу</th>
              <th align="right">Запас</th>
              <th align="left">Закончится</th>
              <th align="left">Срочность</th>
              <th align="left">Объяснение</th>
            </tr>
          </thead>
          <tbody>
            {state.rows.map((r) => {
              const v = supplyUrgencyView(r.urgency);
              return (
                <tr key={r.product_id} data-urgency={r.urgency} style={{ background: v.background, borderBottom: "1px solid #ddd" }}>
                  <td>
                    {r.product_name}
                    {r.blocks_dishes > 0 && <div style={{ fontSize: "0.85em" }}>Блокирует блюд: {r.blocks_dishes}</div>}
                  </td>
                  <td align="right" style={{ fontWeight: 700 }}>{formatQty(r.quantity)}</td>
                  <td align="right">{formatQty(r.usable_stock)}</td>
                  <td title={r.stockout_at ? new Date(r.stockout_at).toLocaleString("ru-RU") : undefined}>{formatStockout(r.stockout_at, nowMs)}</td>
                  <td style={{ color: v.color }}>
                    <span aria-hidden="true">{v.icon} </span>
                    {v.label}
                  </td>
                  <td style={{ fontSize: "0.9em" }}>{r.explanation}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </section>
  );
}
