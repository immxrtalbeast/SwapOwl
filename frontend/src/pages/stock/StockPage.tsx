import { useCallback, useMemo, useRef, useState } from "react";
import { IconChevronDown, IconChevronRight } from "@tabler/icons-react";
import { RoleGate } from "../../auth/AuthContext";
import { api } from "../../api/client";
import { useLive } from "../../app/useLive";
import { useConnection } from "../../app/ConnectionContext";
import { useStandFormat } from "../../app/useStandFormat";
import { ApiError } from "../../api/types";
import type { Alert, Batch, ProductStock } from "../../api/types";
import { Card, DataTable, ScreenStates, ScreenTitle, UrgencyBadge } from "../../components";
import type { Column } from "../../components";
import { formatQty } from "../../lib/format";
import { BATCH_STATES } from "../../lib/urgency";
import { DeliveryForm } from "./DeliveryForm";
import { buildRows, changedKeys, signature } from "./view";
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

  useLive(load, ["stock.changed", "clock.changed", "stand.reset"]);

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
            style={{ display: "inline-flex", alignItems: "center", gap: 8, background: "none", border: 0, padding: 0, cursor: "pointer", font: "var(--type-body-strong)", color: "var(--text)" }}
          >
            {r.expanded ? <IconChevronDown size={16} aria-hidden="true" /> : <IconChevronRight size={16} aria-hidden="true" />}
            {r.product.name}
          </button>
        ) : (
          <span className="so-num" style={{ paddingLeft: 24 }}>партия №{r.batch.id}</span>
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
      cell: (r) => (r.kind === "batch" ? <span title={f.exact(r.batch.received_at)}>{f.dateTime(r.batch.received_at)}</span> : <span style={{ color: "var(--text-muted)" }}>партий: {r.product.batches_count}</span>),
    },
    {
      key: "expires",
      header: "Годна до",
      cell: (r) => {
        const at = r.kind === "batch" ? r.batch.expires_at : r.product.nearest_expires_at;
        return at ? <span title={f.exact(at)}>{f.dateTime(at)}</span> : "—";
      },
    },
    {
      key: "left",
      header: "Осталось",
      cell: (r) => {
        const at = r.kind === "batch" ? r.batch.expires_at : r.product.nearest_expires_at;
        if (!at) return "—";
        return <span title={f.exact(at)}>{f.relative(at, { expired: true })}</span>;
      },
    },
    { key: "state", header: "Состояние", cell: (r) => <UrgencyBadge def={BATCH_STATES[r.state]} /> },
  ];

  const stale = conn === "offline" && updatedAt ? f.time(updatedAt) : null;

  return (
    <section>
      <ScreenTitle stale={stale}>Склад</ScreenTitle>
      <div style={{ display: "flex", flexWrap: "wrap", gap: 24, alignItems: "flex-start" }}>
        <div style={{ flex: "1 1 560px", minWidth: 0 }}>
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
              <DataTable
                caption="Продукты склада с раскрытием до партий"
                columns={columns}
                rows={rows}
                rowKey={(r) => r.key}
                rowTone={(r) => BATCH_STATES[r.state].tone}
                changed={changed}
              />
            )}
          </Card>
        </div>
        <RoleGate min="operator">
          <div style={{ flex: "0 1 380px", minWidth: "min(100%, 300px)" }}>
            <DeliveryForm products={state.kind === "ready" ? state.products : []} onReceived={load} />
          </div>
        </RoleGate>
      </div>
    </section>
  );
}
