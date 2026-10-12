import { useState } from "react";
import type { MoneyReport } from "../../api/types";
import { Card, DataTable, EmptyState, ErrorState, HandNote, LoadingState, ScreenTitle, SecondaryButton } from "../../components";
import type { Column } from "../../components";
import { formatQty } from "../stock/format";
import { formatRub, reasonLabel } from "./format";
import { useMoney } from "./useMoney";

const PERIODS = [
  { days: 1, label: "Сутки" },
  { days: 7, label: "7 дней" },
  { days: 30, label: "30 дней" },
];

type ProductRow = MoneyReport["by_product"][number];
type DayRow = MoneyReport["by_day"][number];

function Section({ title, report, withReason, note }: { title: string; report: MoneyReport; withReason: boolean; note?: string }) {
  const productColumns: Column<ProductRow>[] = [
    { key: "product", header: "Продукт", cell: (r) => r.product_name },
    ...(withReason ? [{ key: "reason", header: "Причина", cell: (r: ProductRow) => reasonLabel(r.reason) }] : []),
    { key: "qty", header: "Количество", numeric: true, cell: (r) => formatQty(r.quantity) },
    { key: "cost", header: "Стоимость", numeric: true, cell: (r) => formatRub(r.cost_minor) },
  ];
  const dayColumns: Column<DayRow>[] = [
    { key: "date", header: "День", cell: (d) => new Date(`${d.date}T00:00:00`).toLocaleDateString("ru-RU", { day: "numeric", month: "long" }) },
    { key: "cost", header: "Стоимость", numeric: true, cell: (d) => formatRub(d.cost_minor) },
  ];
  return (
    <Card title={title}>
      <p data-testid={`total-${withReason ? "writeoffs" : "shortfalls"}`} style={{ margin: "0 0 8px", font: "var(--type-kpi)", color: "var(--text)" }}>
        {formatRub(report.total_minor)}
      </p>
      {note && <div style={{ marginBottom: 16 }}><HandNote arrow="down">{note}</HandNote></div>}
      {report.by_product.length === 0 ? (
        <EmptyState title="Ничего нет" text="За период ничего нет." />
      ) : (
        <div style={{ display: "grid", gap: 16 }}>
          <DataTable caption={`${title}: по продуктам`} columns={productColumns} rows={report.by_product} rowKey={(r) => `${r.product_id}/${r.reason ?? ""}`} />
          <h3 style={{ margin: 0, font: "var(--type-h3)" }}>По дням</h3>
          <DataTable caption={`${title}: по дням`} columns={dayColumns} rows={report.by_day} rowKey={(d) => d.date} />
        </div>
      )}
    </Card>
  );
}

const PERIOD_NOTE: Record<number, string> = { 1: "за сутки", 7: "за неделю", 30: "за 30 дней" };

export function MoneyPage() {
  const [days, setDays] = useState(7);
  const [state, reload] = useMoney(days);
  return (
    <section>
      <ScreenTitle>Деньги</ScreenTitle>
      <div role="group" aria-label="Период" style={{ display: "flex", gap: 8, marginBottom: 16 }}>
        {PERIODS.map((p) => (
          <SecondaryButton key={p.days} aria-pressed={p.days === days} onClick={() => setDays(p.days)}>
            {p.label}
          </SecondaryButton>
        ))}
      </div>
      {state.kind === "loading" && <LoadingState label="Загрузка денег" />}
      {state.kind === "error" && <ErrorState what="отчёт о деньгах" reason={state.message} onRetry={() => void reload()} />}
      {state.kind === "ready" && (
        <div style={{ display: "grid", gap: 24 }}>
          <Section title="Списано за период" report={state.writeoffs} withReason note={`списано ${PERIOD_NOTE[days] ?? "за период"} — это можно было продать`} />
          <Section title="Расхождения кассы" report={state.shortfalls} withReason={false} />
        </div>
      )}
    </section>
  );
}
