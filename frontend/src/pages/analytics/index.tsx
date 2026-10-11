import { useState } from "react";
import { Card, DataTable, EmptyState, ErrorState, HandNote, LoadingState, ScreenTitle, SecondaryButton } from "../../components";
import type { Column } from "../../components";
import type { ShortageRow } from "../../api/types";
import { formatQty } from "../stock/format";
import { benchSentence } from "./benchText";
import { useAnalytics } from "./useAnalytics";

const PERIODS = [
  { days: 1, label: "Сутки" },
  { days: 7, label: "7 дней" },
  { days: 30, label: "30 дней" },
];

export function AnalyticsPage() {
  const [days, setDays] = useState(7);
  const [state, reload] = useAnalytics(days);
  const bench = state.kind === "ready" ? benchSentence(state.bench) : null;
  const columns: Column<ShortageRow>[] = [
    { key: "product", header: "Продукт", cell: (r) => r.product_name },
    { key: "rejected", header: "Отклонённых заказов", numeric: true, cell: (r) => r.rejected_orders },
    { key: "missing", header: "Не хватило", numeric: true, cell: (r) => formatQty(r.missing) },
  ];
  return (
    <section>
      <ScreenTitle>Аналитика</ScreenTitle>
      {bench && (
        <Card style={{ marginBottom: 16 }}>
          <p data-testid="bench" style={{ margin: 0, font: "var(--type-h2)" }}>
            {bench}
          </p>
        </Card>
      )}
      <div role="group" aria-label="Период" style={{ display: "flex", gap: 8, marginBottom: 16 }}>
        {PERIODS.map((p) => (
          <SecondaryButton key={p.days} aria-pressed={p.days === days} onClick={() => setDays(p.days)} style={p.days === days ? { background: "var(--brand-tint)" } : undefined}>
            {p.label}
          </SecondaryButton>
        ))}
      </div>
      {state.kind === "loading" && <LoadingState label="Загрузка аналитики" />}
      {state.kind === "error" && <ErrorState what="аналитику" reason={state.message} onRetry={() => void reload()} />}
      {state.kind === "ready" && (
        <Card title="Дефицит: чего не хватило заказам">
          <p data-testid="rejected-total" style={{ margin: "0 0 8px", font: "var(--type-kpi)" }}>
            {state.shortages.rejected_orders}
          </p>
          <div style={{ marginBottom: 16 }}>
            <HandNote arrow="down">отклонённых заказов за период</HandNote>
          </div>
          {state.shortages.by_product.length === 0 ? (
            <EmptyState title="Отказов не было" text="За период отказов из-за нехватки продуктов не было." />
          ) : (
            <DataTable caption="Дефицит по продуктам" columns={columns} rows={state.shortages.by_product} rowKey={(r) => r.product_id} />
          )}
        </Card>
      )}
    </section>
  );
}
