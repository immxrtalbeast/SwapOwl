import { useState } from "react";
import type { HubPoint, HubSummary, HubTransfer } from "../../api/network";
import { Card, DataTable, EmptyState, ErrorState, LoadingState, ScreenTitle, UrgencyBadge } from "../../components";
import type { Column } from "../../components";
import { BATCH_STATES, DISH_STATES, batchState } from "../../lib/urgency";
import type { UrgencyDef } from "../../lib/urgency";
import { formatRub, formatQty } from "../../lib/format";
import { formatTimeLeft } from "../stock/format";
import { AbCompare } from "./AbCompare";
import { Transfers } from "./Transfers";
import { abPair, buildRows, clockText, networkTotals, stopListText } from "./model";
import type { Cache, PointRow } from "./model";
import { useNetwork } from "./useNetwork";

const ONLINE: UrgencyDef = { tone: "ok", icon: "circle-check", label: "На связи" };
const OFFLINE: UrgencyDef = { tone: "neutral", icon: "circle-x", label: "Нет связи" };

const money = (v: number | null | undefined) => (v == null ? "—" : formatRub(v));

function Link({ row }: { row: PointRow }) {
  const url = row.point.url ?? row.data?.url;
  if (!url) return null;
  return (
    <a href={url} target="_blank" rel="noopener noreferrer" style={{ font: "var(--type-button)", color: "var(--brand-ink)" }}>
      Открыть точку
    </a>
  );
}

function Connection({ row }: { row: PointRow }) {
  if (row.online) return <UrgencyBadge def={ONLINE} />;
  const label = row.offlineMin == null ? OFFLINE.label : `${OFFLINE.label} ${row.offlineMin} мин`;
  return <UrgencyBadge def={OFFLINE} label={label} />;
}

/** Серая строка недоступной точки: подпись со временем последнего опроса. */
const grey = (row: PointRow) => (row.online ? undefined : { color: "var(--text-subtle)" });

function columns(onSelect: (id: string) => void, selected: string | null): Column<PointRow>[] {
  return [
    {
      key: "point",
      header: "Точка",
      cell: (r) => (
        <div data-offline={r.online ? undefined : "true"} style={grey(r)}>
          <button
            type="button"
            onClick={(e) => { e.stopPropagation(); onSelect(r.point.id); }}
            aria-pressed={selected === r.point.id}
            style={{ all: "unset", cursor: "pointer", font: "var(--type-body-strong)", color: r.online ? "var(--brand-ink)" : "var(--text-subtle)", textDecoration: "underline" }}
          >
            {r.point.name}
          </button>
          {!r.online && (
            <div style={{ font: "var(--type-small)" }}>
              {r.lastSuccessAt ? `данные последнего опроса в ${clockText(r.lastSuccessAt)}` : "опросов ещё не было"}
            </div>
          )}
        </div>
      ),
    },
    { key: "link", header: "Связь", cell: (r) => <Connection row={r} /> },
    {
      key: "expiring",
      header: "Истекает, ₽",
      numeric: true,
      cell: (r) => <span style={grey(r)}>{money(r.data?.expiring_sum_minor)}</span>,
    },
    {
      key: "stop",
      header: "Стоп-лист",
      cell: (r) => (
        <span style={grey(r)}>
          {r.data && r.data.stop_list_count > 0 && <UrgencyBadge def={DISH_STATES.stopped} label={String(r.data.stop_list_count)} />}{" "}
          {stopListText(r.data)}
        </span>
      ),
    },
    { key: "today", header: "Списания сегодня", numeric: true, cell: (r) => <span style={grey(r)}>{money(r.data?.writeoffs_today_minor)}</span> },
    { key: "week", header: "За 7 дней", numeric: true, cell: (r) => <span style={grey(r)}>{money(r.data?.writeoffs_7d_minor)}</span> },
    { key: "hot", header: "Горячее предложение", cell: (r) => <span style={grey(r)}>{r.data?.top_hot_offer?.dish_name ?? "—"}</span> },
  ];
}

function Detail({ row, detail }: { row: PointRow; detail: HubPoint | null }) {
  const alerts = detail?.alerts ?? [];
  const stop = detail?.stop_list ?? [];
  const missing = detail === null;
  return (
    <Card title={`Подробности: ${row.point.name}`} actions={<Link row={row} />}>
      {!row.online && (
        <p style={{ margin: "0 0 12px", color: "var(--text-muted)" }}>
          Точка недоступна — ленты (если есть) показаны по данным последнего опроса{row.lastSuccessAt ? ` в ${clockText(row.lastSuccessAt)}` : ""}.
        </p>
      )}
      <div style={{ display: "grid", gap: 16, gridTemplateColumns: "repeat(auto-fit, minmax(280px, 1fr))" }}>
        <section aria-label="Предупреждения">
          <h3 style={{ margin: "0 0 8px", font: "var(--type-h3)" }}>Предупреждения</h3>
          {missing ? (
            <p style={{ margin: 0, color: "var(--text-muted)" }}>Ленты недоступны — нет данных последнего опроса.</p>
          ) : alerts.length === 0 ? (
            <p style={{ margin: 0, color: "var(--text-muted)" }}>Нет предупреждений.</p>
          ) : (
            <ul style={{ listStyle: "none", margin: 0, padding: 0, display: "grid", gap: 8 }}>
              {alerts.map((a) => (
                <li key={a.batch_id} style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
                  <UrgencyBadge def={BATCH_STATES[batchState({ remaining: a.remaining.qty, status: a.status, doomed: a.doomed })]} />
                  <span style={{ font: "var(--type-body-strong)" }}>{a.product_name}</span>
                  <span className="so-num">{formatQty(a.remaining)}</span>
                  <span style={{ color: "var(--text-muted)" }}>{a.status === "expired" ? "просрочена" : formatTimeLeft(a.time_left_seconds)}</span>
                </li>
              ))}
            </ul>
          )}
        </section>
        <section aria-label="Стоп-лист">
          <h3 style={{ margin: "0 0 8px", font: "var(--type-h3)" }}>Стоп-лист</h3>
          {missing ? (
            <p style={{ margin: 0, color: "var(--text-muted)" }}>Ленты недоступны — нет данных последнего опроса.</p>
          ) : stop.length === 0 ? (
            <p style={{ margin: 0, color: "var(--text-muted)" }}>Стоп-лист пуст.</p>
          ) : (
            <ul style={{ listStyle: "none", margin: 0, padding: 0, display: "grid", gap: 8 }}>
              {stop.map((d) => (
                <li key={d.id} style={{ display: "flex", gap: 8, alignItems: "center" }}>
                  <UrgencyBadge def={DISH_STATES.stopped} />
                  <span>{d.name}</span>
                </li>
              ))}
            </ul>
          )}
        </section>
      </div>
    </Card>
  );
}

interface ViewProps {
  summary: HubSummary;
  cache: Cache;
  /** Время сравнения для «нет связи N мин», мс. */
  nowMs: number;
  selected: string | null;
  onSelect: (id: string) => void;
  detail: HubPoint | null;
  /** Подсказки о перемещении партий между точками; без них блок «Перемещения» не показывается. */
  transfers?: HubTransfer[];
}

/** Таблица точек, итог по сети и подробности выбранной точки. Без запросов — их делает NetworkPage. */
export function NetworkView({ summary, cache, nowMs, selected, onSelect, detail, transfers }: ViewProps) {
  const rows = buildRows(summary.points, cache, nowMs);
  const totals = networkTotals(summary);
  const chosen = rows.find((r) => r.point.id === selected) ?? null;
  const cols = columns(onSelect, selected);
  const ab = abPair(summary.points);
  return (
    <div style={{ display: "grid", gap: 24 }}>
      {ab && <AbCompare pair={ab} />}
      <Card flush title="Точки сети">
        <DataTable
          caption="Точки сети по риску"
          columns={cols}
          rows={rows}
          rowKey={(r) => r.point.id}
          rowTone={(r) => (!r.data ? undefined : r.data.expired_batches > 0 ? "risk" : r.data.expiring_batches > 0 ? "warn" : undefined)}
          onRowClick={(r) => onSelect(r.point.id)}
        />
        <div
          data-testid="network-totals"
          style={{ display: "flex", gap: 24, flexWrap: "wrap", padding: "12px var(--card-pad)", background: "var(--surface-sunken)", font: "var(--type-body-strong)" }}
        >
          <span>Итого по сети · на связи {totals.online} из {totals.points}</span>
          <span>Истекает: {money(totals.expiringSumMinor)}</span>
          <span>Стоп-лист: {totals.stopListCount}</span>
          <span>Списания сегодня: {formatRub(totals.writeoffsTodayMinor)}</span>
          <span>За 7 дней: {formatRub(totals.writeoffs7dMinor)}</span>
        </div>
      </Card>
      {transfers && <Transfers transfers={transfers} />}
      {chosen ? <Detail row={chosen} detail={detail} /> : <p style={{ margin: 0, color: "var(--text-muted)" }}>Выберите точку в таблице — откроются ленты предупреждений и стоп-листа.</p>}
    </div>
  );
}

export function NetworkPage() {
  const [selected, setSelected] = useState<string | null>(null);
  const [state, reload] = useNetwork(selected);
  return (
    <section>
      <ScreenTitle>Сеть</ScreenTitle>
      {state.kind === "loading" && <LoadingState label="Загрузка сети" />}
      {state.kind === "error" && <ErrorState what="сводку по сети" reason={state.message} onRetry={() => void reload()} />}
      {state.kind === "ready" && state.summary.points.length === 0 && (
        <EmptyState title="Точек нет" text="В конфиге hub не задано ни одной точки." />
      )}
      {state.kind === "ready" && state.summary.points.length > 0 && (
        <NetworkView
          summary={state.summary}
          cache={state.cache}
          nowMs={state.at}
          selected={selected}
          onSelect={setSelected}
          detail={state.detail}
          transfers={state.transfers}
        />
      )}
    </section>
  );
}
