import { useCallback, useMemo, useRef, useState } from "react";
import { useMediaQuery } from "@mantine/hooks";
import { IconChevronDown, IconChevronRight } from "@tabler/icons-react";
import { RoleGate } from "../../auth/AuthContext";
import { api } from "../../api/client";
import { useLive } from "../../app/useLive";
import { useConnection } from "../../app/ConnectionContext";
import { useStandFormat } from "../../app/useStandFormat";
import { ApiError } from "../../api/types";
import type { Alert, Batch, ProductStock } from "../../api/types";
import { Card, DataTable, ScreenStates, ScreenTitle, SecondaryButton, UrgencyBadge } from "../../components";
import type { Column } from "../../components";
import { formatQty } from "../../lib/format";
import { BATCH_STATES } from "../../lib/urgency";
import { DeliveryForm } from "./DeliveryForm";
import { BatchOpen } from "./BatchOpen";
import { openedMark } from "./opened";
import { needsOpenFirst } from "../label/labelData";
import { StockCards } from "./StockCards";
import { buildRows, changedKeys, productStateLabel, signature } from "./view";
import type { StockRow } from "./view";

type State =
  | { kind: "loading" }
  | { kind: "error"; message: string; requestId: string | null }
  | { kind: "ready"; products: ProductStock[]; batches: Batch[]; alerts: Alert[] };

const CHANGED_MS = 1600;

function requestIdOf(e: unknown): string | null {
  const id = e instanceof ApiError ? e.details.request_id : null;
  return typeof id === "string" ? id : null;
}

export function StockPage() {
  const [state, setState] = useState<State>({ kind: "loading" });
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(new Set());
  const [changed, setChanged] = useState<ReadonlySet<string>>(new Set());
  const [updatedAt, setUpdatedAt] = useState<Date | null>(null);
  const f = useStandFormat();
  // < 640 px: карточки вместо таблицы, форма приёма поставки — под списком и свёрнута.
  const narrow = useMediaQuery("(max-width: 639px)") ?? false;
  const [formOpen, setFormOpen] = useState(false);
  const conn = useConnection();
  const prevSig = useRef<Map<string, string> | null>(null);
  const expandedRef = useRef(expanded);
  expandedRef.current = expanded;
  const clearTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const nowRef = useRef(f.now);
  nowRef.current = f.now;

  const load = useCallback(async () => {
    try {
      const [products, batches, alerts] = await Promise.all([api.products(), api.batches(), api.alerts()]);
      // Подсветка считается по всем партиям (в том числе в свёрнутых продуктах): раскрытие потом покажет, что изменилось.
      const all = new Set(products.map((p) => p.id));
      const sig = signature(buildRows(products, batches, alerts, all));
      const diff = changedKeys(prevSig.current, sig);
      prevSig.current = sig;
      setState({ kind: "ready", products, batches, alerts });
      setUpdatedAt(nowRef.current);
      if (diff.size > 0) {
        setChanged(diff);
        if (clearTimer.current) clearTimeout(clearTimer.current);
        clearTimer.current = setTimeout(() => setChanged(new Set()), CHANGED_MS);
      }
    } catch (e) {
      setState((prev) =>
        prev.kind === "ready"
          ? prev
          : { kind: "error", message: e instanceof Error ? e.message : "Сервер не ответил.", requestId: requestIdOf(e) },
      );
    }
  }, []);

  // Как в ленте предупреждений: окно истечения открывает «дворник» событием batch.expiring без stock.changed.
  useLive(load, ["batch.expiring", "batch.expired", "stock.changed", "clock.changed", "stand.reset"]);

  const rows = useMemo(
    () => (state.kind === "ready" ? buildRows(state.products, state.batches, state.alerts, expanded) : []),
    [state, expanded],
  );

  const toggle = (id: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (!next.delete(id)) next.add(id);
      return next;
    });

  // Кнопка «Вскрыл упаковку» — у непросроченной партии продукта со сроком после вскрытия (operator).
  const productsById = useMemo(() => new Map((state.kind === "ready" ? state.products : []).map((p) => [p.id, p])), [state]);
  const openAction = (r: Extract<StockRow, { kind: "batch" }>) => {
    const product = productsById.get(r.batch.product_id);
    const hours = product?.shelf_after_open_hours;
    if (!product || !hours || r.batch.opened_at || r.batch.status === "expired" || r.batch.remaining.qty <= 0) return null;
    return (
      <RoleGate min="operator">
        <BatchOpen batchId={r.batch.id} productName={product.name} hours={hours} batchExpiresAt={r.batch.expires_at} nowMs={f.now.getTime()} timeZone={f.timezone} onOpened={load} />
      </RoleGate>
    );
  };

  const columns: Column<StockRow>[] = [
    {
      key: "name",
      header: "Продукт · партия",
      cell: (r) =>
        r.kind === "product" ? (
          <button
            type="button"
            onClick={() => toggle(r.product.id)}
            aria-expanded={r.expanded}
            aria-label={`${r.expanded ? "Свернуть" : "Раскрыть"} партии: ${r.product.name}`}
            style={{ display: "inline-flex", alignItems: "center", gap: 8, background: "none", border: 0, padding: 0, cursor: "pointer", font: "var(--type-body-strong)", color: "var(--text)", textAlign: "left", whiteSpace: "nowrap" }}
          >
            {r.expanded ? <IconChevronDown size={16} aria-hidden="true" /> : <IconChevronRight size={16} aria-hidden="true" />}
            {r.product.name}
          </button>
        ) : (
          <span style={{ paddingLeft: 24, display: "inline-grid" }}>
            <span className="so-num">партия №{r.batch.id}{!needsOpenFirst(r.batch.opened_at, productsById.get(r.batch.product_id)?.shelf_after_open_hours) && <> · <a href={`#/label/${r.batch.id}`}>Печать этикетки</a></>}</span>
            {r.batch.opened_at && (
              <span title={`вскрыта из партии №${r.batch.parent_batch_id ?? "?"}`} style={{ color: "var(--text-muted)" }}>
                {openedMark(r.batch.opened_at, f.timezone)}
              </span>
            )}
          </span>
        ),
    },
    {
      key: "qty",
      header: "Остаток",
      numeric: true,
      cell: (r) =>
        r.kind === "product" ? (
          formatQty(r.product.available)
        ) : (
          <span style={{ fontWeight: r.state === "doomed" ? 700 : undefined, textDecoration: r.state === "expired" ? "line-through" : undefined, color: r.state === "spent" || r.state === "expired" ? "var(--text-subtle)" : undefined }}>
            {formatQty(r.batch.remaining)}
          </span>
        ),
    },
    {
      key: "received",
      header: "Поступила",
      cell: (r) => (r.kind === "batch" ? <span title={f.exact(r.batch.received_at)} style={{ whiteSpace: "nowrap" }}>{f.dateTime(r.batch.received_at)}</span> : <span style={{ color: "var(--text-muted)", whiteSpace: "nowrap" }}>партий: {r.product.batches_count}</span>),
    },
    {
      key: "expires",
      header: "Годна до",
      cell: (r) => {
        const at = r.kind === "batch" ? r.batch.expires_at : r.product.nearest_expires_at;
        return at ? <span title={f.exact(at)} style={{ whiteSpace: "nowrap" }}>{f.dateTime(at)}</span> : "—";
      },
    },
    {
      key: "left",
      header: "Осталось",
      cell: (r) => {
        const at = r.kind === "batch" ? r.batch.expires_at : r.product.nearest_expires_at;
        if (!at) return "—";
        return <span title={f.exact(at)} style={{ whiteSpace: "nowrap" }}>{f.relative(at, { expired: true })}</span>;
      },
    },
    // У строки продукта «Годна до» — ближайшая живая партия, а состояние — худшая: «есть просрочка», а не «просрочена».
    { key: "state", header: "Состояние", cell: (r) => <UrgencyBadge def={BATCH_STATES[r.state]} label={r.kind === "product" ? productStateLabel(r.state) : undefined} /> },
    { key: "open", header: "Действие", cell: (r) => (r.kind === "batch" ? openAction(r) : null) },
  ].filter((c) => c.key !== "open" || rows.some((r) => r.kind === "batch")); // «Действие» — только у раскрытых партий

  const stale = conn === "offline" && updatedAt ? f.time(updatedAt) : null;

  return (
    <section>
      <ScreenTitle stale={stale}>Склад</ScreenTitle>
      <div style={{ display: "flex", flexWrap: "wrap", gap: 24, alignItems: "flex-start" }}>
        <div style={{ flex: "1 1 640px", minWidth: 0 }}>
          <Card flush>
            {state.kind === "loading" && (
              <div style={{ padding: "var(--card-pad)" }}>
                <ScreenStates.Loading label="Загрузка склада" />
              </div>
            )}
            {state.kind === "error" && (
              <ScreenStates.Error what="склад" reason={state.message} requestId={state.requestId} onRetry={() => { setState({ kind: "loading" }); void load(); }} />
            )}
            {state.kind === "ready" && state.products.length === 0 && (
              <ScreenStates.Empty title="Продуктов пока нет" text="Примите первую поставку — партии появятся на складе." />
            )}
            {state.kind === "ready" && state.products.length > 0 && (
              narrow ? (
                <StockCards rows={rows} changed={changed} expiresText={f.dateTime} exactText={f.exact} onToggle={toggle} timeZone={f.timezone} batchAction={openAction} />
              ) : (
              <DataTable
                caption="Продукты склада с раскрытием до партий"
                columns={columns}
                rows={rows}
                rowKey={(r) => r.key}
                rowTone={(r) => BATCH_STATES[r.state].tone}
                changed={changed}
              />
              )
            )}
          </Card>
        </div>
        <RoleGate min="operator">
          <div style={{ flex: "0 1 340px", minWidth: "min(100%, 300px)" }}>
            {narrow && (
              <SecondaryButton aria-expanded={formOpen} aria-controls="delivery-form" onClick={() => setFormOpen((o) => !o)} style={{ marginBottom: 12 }}>
                {formOpen ? "Скрыть приём поставки" : "Приём поставки"}
              </SecondaryButton>
            )}
            {(!narrow || formOpen) && (
              <div id="delivery-form">
                <DeliveryForm products={state.kind === "ready" ? state.products : []} onReceived={load} />
              </div>
            )}
          </div>
        </RoleGate>
      </div>
    </section>
  );
}
