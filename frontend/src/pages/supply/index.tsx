import { useRef, useState } from "react";
import { api } from "../../api/client";
import type { DeliveryInput, SupplySuggestion } from "../../api/types";
import { Card, DataTable, EmptyState, ErrorState, LoadingState, PrimaryButton, ScreenTitle, SecondaryButton, UrgencyBadge } from "../../components";
import type { Column } from "../../components";
import { SUPPLY_STATES } from "../../lib/urgency";
import { formatQty } from "../stock/format";
import { buildAcceptBody, offerKey, outcomeFromError, outcomeFromResult } from "./accept";
import type { AcceptOutcome } from "./accept";
import { formatStockout } from "./urgency";
import { useSupply } from "./useSupply";

interface Pending {
  id: string;
  createdAt: string;
}

interface Sent {
  productName: string;
  body: DeliveryInput;
  outcome: AcceptOutcome;
}

function AcceptCell({ row, pending, onSent }: { row: SupplySuggestion; pending: () => Pending; onSent: (s: Sent) => void }) {
  const [busy, setBusy] = useState(false);
  const [outcome, setOutcome] = useState<AcceptOutcome | null>(null);
  const [hours, setHours] = useState("");
  const needHours = outcome?.kind === "need-shelf-life";

  async function accept() {
    const h = needHours && hours.trim() !== "" ? Number(hours) : undefined;
    const p = pending();
    setBusy(true);
    try {
      const body = buildAcceptBody(row, p.id, p.createdAt, h);
      const r = await api.receiveDelivery(body);
      setOutcome(null);
      onSent({ productName: row.product_name, body, outcome: outcomeFromResult(r.replayed) });
    } catch (e) {
      setOutcome(outcomeFromError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div>
      {needHours && (
        <label style={{ display: "block", font: "var(--type-small)", marginBottom: 4 }}>
          Срок хранения, ч{" "}
          <input type="number" min={1} max={87600} step={1} required value={hours} onChange={(e) => setHours(e.target.value)} style={{ width: 80, height: 30, border: "1px solid var(--border-strong)", borderRadius: 6, padding: "0 8px" }} />
        </label>
      )}
      <PrimaryButton size="compact-md" onClick={() => void accept()} disabled={busy || (needHours && hours.trim() === "")} loading={busy}>
        Принять предложенное
      </PrimaryButton>
      {outcome && <div role={outcome.kind === "error" || needHours ? "alert" : "status"} style={{ font: "var(--type-small)", marginTop: 4 }}>{outcome.message}</div>}
    </div>
  );
}

export function SupplyPage() {
  const [state, reload] = useSupply();
  const nowMs = Date.now();
  // Отправленное тело хранится на уровне страницы: повтор не зависит от пересчёта строк после SSE.
  const [sent, setSent] = useState<Sent | null>(null);
  const [resending, setResending] = useState(false);
  async function resend() {
    if (!sent) return;
    setResending(true);
    try {
      const r = await api.receiveDelivery(sent.body);
      setSent({ ...sent, outcome: outcomeFromResult(r.replayed) });
    } catch (e) {
      setSent({ ...sent, outcome: outcomeFromError(e) });
    } finally {
      setResending(false);
    }
  }
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
  const columns: Column<SupplySuggestion>[] = [
    {
      key: "product",
      header: "Продукт",
      cell: (r) => (
        <>
          <span style={{ font: "var(--type-body-strong)" }}>{r.product_name}</span>
          {r.blocks_dishes > 0 && <div style={{ font: "var(--type-small)", color: "var(--text-muted)" }}>Блокирует блюд: {r.blocks_dishes}</div>}
        </>
      ),
    },
    { key: "qty", header: "К заказу", numeric: true, cell: (r) => <strong>{formatQty(r.quantity)}</strong> },
    { key: "stock", header: "Запас", numeric: true, cell: (r) => formatQty(r.usable_stock) },
    {
      key: "stockout",
      header: "Закончится",
      cell: (r) => <span title={r.stockout_at ? new Date(r.stockout_at).toLocaleString("ru-RU") : undefined}>{formatStockout(r.stockout_at, nowMs)}</span>,
    },
    { key: "urgency", header: "Срочность", cell: (r) => <UrgencyBadge def={SUPPLY_STATES[r.urgency]} /> },
    { key: "explanation", header: "Объяснение", cell: (r) => <span style={{ font: "var(--type-small)" }}>{r.explanation}</span> },
    { key: "action", header: "Действие", cell: (r) => <AcceptCell row={r} pending={pendingFor(r)} onSent={setSent} /> },
  ];
  return (
    <section>
      <ScreenTitle>Поставки</ScreenTitle>
      {sent && (
        <div role={sent.outcome.kind === "error" ? "alert" : "status"} style={{ margin: "0 0 16px", display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap", font: "var(--type-body)" }}>
          <span>{sent.productName}: {sent.outcome.message}</span>
          <SecondaryButton size="compact-sm" onClick={() => void resend()} disabled={resending}>Отправить ещё раз</SecondaryButton>
        </div>
      )}
      <Card flush>
        {state.kind === "loading" && <div style={{ padding: 16 }}><LoadingState label="Загрузка поставок" /></div>}
        {state.kind === "error" && <ErrorState what="поставки" reason={state.message} onRetry={() => void reload()} />}
        {state.kind === "ready" && state.rows.length === 0 && (
          <EmptyState title="Закупать ничего не нужно" text="Запаса хватает на срок доставки и дни покрытия." />
        )}
        {state.kind === "ready" && state.rows.length > 0 && (
          <DataTable
            caption="Предложение поставок"
            columns={columns}
            rows={state.rows}
            rowKey={(r) => r.product_id}
            rowTone={(r) => (r.urgency === "soon" ? "warn" : undefined)}
          />
        )}
      </Card>
    </section>
  );
}
