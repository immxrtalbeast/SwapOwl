import { useEffect, useRef, useState } from "react";
import { api } from "../../api/client";
import type { DeliveryInput, SupplyOrder, SupplySuggestion } from "../../api/types";
import { RoleGate } from "../../auth/AuthContext";
import { Card, DataTable, EmptyState, ErrorState, LoadingState, PrimaryButton, ScreenTitle, SecondaryButton, UrgencyBadge } from "../../components";
import type { Column } from "../../components";
import { SUPPLY_STATES } from "../../lib/urgency";
import { formatSupplyQty as formatQty } from "./qty";
import { explainSupply } from "./explain";
import { buildAcceptBody, offerKey, outcomeFromError, outcomeFromResult } from "./accept";
import type { AcceptOutcome } from "./accept";
import { ORDER_STATUS_TEXT, buildOrderBody, buildOrderCsv, needsPolling, selectionKey } from "./order";
import { formatStockout } from "./urgency";
import { useSupply } from "./useSupply";
import { useStandFormat } from "../../app/useStandFormat";

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
  // Без срока поставщика в строке срок вводит оператор сразу: поставка без срока не принимается (FS-160).
  const needHours = outcome?.kind === "need-shelf-life" || row.shelf_life_hours === null;
  const AcceptButton = row.urgency === "now" ? PrimaryButton : SecondaryButton;

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
      {/* Залитая кнопка — только у «Заказать сейчас»: восемь одинаковых зелёных кнопок забивали таблицу (FS-161). */}
      <AcceptButton size="compact-md" onClick={() => void accept()} disabled={busy || (needHours && hours.trim() === "")} loading={busy}>
        Принять предложенное
      </AcceptButton>
      {outcome && <div role={outcome.kind === "error" || needHours ? "alert" : "status"} style={{ font: "var(--type-small)", marginTop: 4 }}>{outcome.message}</div>}
    </div>
  );
}

const ORDER_POLL_MS = 3000;

interface SentOrder {
  order?: SupplyOrder;
  /** Ошибка запроса статуса или отправки — не путать с «ошибкой доставки» от адаптера. */
  failure?: string;
}

export function SupplyPage() {
  const [state, reload] = useSupply();
  const fmt = useStandFormat();
  const nowMs = fmt.now.getTime(); // время стенда, не часы браузера
  // Заказ поставщику (через подключённую учётную систему): выбранные строки → POST /api/supply-orders → статус по подтверждению outbox.
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [ordering, setOrdering] = useState(false);
  const [placed, setPlaced] = useState<SentOrder | null>(null);
  // id заказа живёт, пока выбор тот же: повтор отправки — тот же заказ, без дубля в iiko.
  const orderIds = useRef(new Map<string, string>());
  const rows = state.kind === "ready" ? state.rows : [];
  const chosen = rows.filter((r) => selected.has(offerKey(r)));
  async function placeOrder() {
    if (chosen.length === 0) return;
    const key = selectionKey(chosen);
    let id = orderIds.current.get(key);
    if (!id) {
      id = crypto.randomUUID();
      orderIds.current.set(key, id);
    }
    setOrdering(true);
    try {
      const order = await api.createSupplyOrder(buildOrderBody(chosen, id));
      setPlaced({ order });
    } catch (e) {
      setPlaced((prev) => ({ order: prev?.order, failure: e instanceof Error ? e.message : "Не удалось отправить заказ" }));
    } finally {
      setOrdering(false);
    }
  }
  function downloadCsv() {
    const url = URL.createObjectURL(new Blob([buildOrderCsv(chosen)], { type: "text/csv;charset=utf-8" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = "order.csv";
    a.click();
    URL.revokeObjectURL(url);
  }
  const placedId = placed?.order?.id;
  const placedStatus = placed?.order?.status;
  useEffect(() => {
    if (!placedId || !placedStatus || !needsPolling(placedStatus)) return;
    const t = setInterval(() => {
      api.supplyOrder(placedId).then(
        (order) => setPlaced({ order }),
        () => undefined, // следующий опрос повторит
      );
    }, ORDER_POLL_MS);
    return () => clearInterval(t);
  }, [placedId, placedStatus]);
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
      key: "select",
      header: "Выбор",
      cell: (r) => (
        <input
          type="checkbox"
          aria-label={`Выбрать ${r.product_name} для заказа`}
          checked={selected.has(offerKey(r))}
          onChange={(e) =>
            setSelected((prev) => {
              const next = new Set(prev);
              if (e.target.checked) next.add(offerKey(r));
              else next.delete(offerKey(r));
              return next;
            })
          }
        />
      ),
    },
    {
      key: "product",
      header: "Продукт",
      cell: (r) => (
        <>
          <span style={{ font: "var(--type-body-strong)", whiteSpace: "nowrap" }}>{r.product_name}</span>
          {r.blocks_dishes > 0 && <div style={{ font: "var(--type-small)", color: "var(--text-muted)" }}>Блокирует блюд: {r.blocks_dishes}</div>}
        </>
      ),
    },
    { key: "qty", header: "К заказу", numeric: true, cell: (r) => <strong>{formatQty(r.quantity)}</strong> },
    { key: "stock", header: "Запас", numeric: true, cell: (r) => formatQty(r.usable_stock) },
    {
      key: "stockout",
      header: "Закончится",
      cell: (r) => <span style={r.usable_stock.qty <= 0 ? { color: "var(--urgency-risk-fg, #a3330a)" } : undefined} title={r.stockout_at ? fmt.exact(r.stockout_at) : undefined}>{formatStockout(r.stockout_at, nowMs, r.usable_stock.qty)}</span>,
    },
    { key: "urgency", header: "Срочность", cell: (r) => <UrgencyBadge def={SUPPLY_STATES[r.urgency]} /> },
    { key: "explanation", header: "Объяснение", cell: (r) => <span style={{ font: "var(--type-small)" }}>{explainSupply(r.explanation, r.quantity.unit)}</span> },
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
      <RoleGate min="operator">
        <div style={{ margin: "0 0 16px", display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap", font: "var(--type-body)" }}>
          <PrimaryButton size="compact-md" onClick={() => void placeOrder()} disabled={ordering || chosen.length === 0} loading={ordering}>
            Отправить заказ поставщику
          </PrimaryButton>
          <SecondaryButton size="compact-md" onClick={downloadCsv} disabled={chosen.length === 0}>Скачать заказ (CSV)</SecondaryButton>
          {/* В API нет признака подключённого адаптера — показываем обе кнопки. */}
          <span style={{ font: "var(--type-small)", color: "var(--text-muted)" }}>Отправка — через подключённую учётную систему</span>
          <span style={{ font: "var(--type-small)", color: "var(--text-muted)" }}>Выбрано строк: {chosen.length}</span>
          {placed?.order && (
            <span role={placed.order.status === "error" ? "alert" : "status"}>
              Заказ поставщику: {ORDER_STATUS_TEXT[placed.order.status]}
              {placed.order.status === "error" && placed.order.last_error ? ` (${placed.order.last_error}); адаптер повторит` : ""}
            </span>
          )}
          {placed?.failure && <span role="alert">{placed.failure}</span>}
        </div>
      </RoleGate>
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
