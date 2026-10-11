import { useRef, useState } from "react";
import { api } from "../../api/client";
import type { SupplySuggestion } from "../../api/types";
import { formatQty } from "../stock/format";
import { buildAcceptBody, offerKey, outcomeFromError, outcomeFromResult } from "./accept";
import type { AcceptOutcome } from "./accept";
import { formatStockout, supplyUrgencyView } from "./urgency";
import { useSupply } from "./useSupply";

interface Pending {
  id: string;
  createdAt: string;
}

function AcceptCell({ row, pending }: { row: SupplySuggestion; pending: () => Pending }) {
  const [busy, setBusy] = useState(false);
  const [outcome, setOutcome] = useState<AcceptOutcome | null>(null);
  const [hours, setHours] = useState("");
  const needHours = outcome?.kind === "need-shelf-life";

  async function accept() {
    const h = needHours && hours.trim() !== "" ? Number(hours) : undefined;
    const p = pending();
    setBusy(true);
    try {
      const r = await api.receiveDelivery(buildAcceptBody(row, p.id, p.createdAt, h));
      setOutcome(outcomeFromResult(r.replayed));
    } catch (e) {
      setOutcome(outcomeFromError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div>
      {needHours && (
        <label style={{ display: "block", fontSize: "0.85em" }}>
          Срок хранения, ч{" "}
          <input type="number" min={1} max={87600} step={1} required value={hours} onChange={(e) => setHours(e.target.value)} style={{ width: 80 }} />
        </label>
      )}
      <button onClick={() => void accept()} disabled={busy || (needHours && hours.trim() === "")}>
        Принять предложенное
      </button>
      {outcome && <div role={outcome.kind === "error" || needHours ? "alert" : "status"} style={{ fontSize: "0.85em" }}>{outcome.message}</div>}
    </div>
  );
}

export function SupplyPage() {
  const [state, reload] = useSupply();
  const nowMs = Date.now();
  // id поставки создаётся при показе предложения и живёт, пока строка видна.
  const offers = useRef(new Map<string, Pending>());
  if (state.kind === "ready") {
    const live = new Set(state.rows.map(offerKey));
    for (const k of offers.current.keys()) if (!live.has(k)) offers.current.delete(k);
  }
  const pendingFor = (row: SupplySuggestion) => () => {
    const k = offerKey(row);
    let p = offers.current.get(k);
    if (!p) {
      p = { id: crypto.randomUUID(), createdAt: new Date().toISOString() };
      offers.current.set(k, p);
    }
    return p;
  };
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
              <th align="left">Действие</th>
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
                  <td>
                    <AcceptCell row={r} pending={pendingFor(r)} />
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </section>
  );
}
