import { useEffect, useState } from "react";
import { api } from "../../api/client";
import type { ForecastAccuracy } from "../../api/types";
import { accuracyDays, accuracySentence, worstDishes } from "./accuracy";
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

/** Точность прогноза спроса: ошибка с обучением против «только априор». Нет маршрута или ошибка — карточки нет. */
function AccuracyCard({ days }: { days: number }) {
  const [acc, setAcc] = useState<ForecastAccuracy | null>(null);
  useEffect(() => {
    let alive = true;
    api.forecastAccuracy(accuracyDays(days)).then((a) => { if (alive) setAcc(a); }).catch(() => { if (alive) setAcc(null); });
    return () => { alive = false; };
  }, [days]);
  if (!acc) return null;
  const rows = acc.enough_data ? worstDishes(acc) : [];
  const columns: Column<ForecastAccuracy["dishes"][number]>[] = [
    { key: "d", header: "Блюдо", cell: (r) => r.dish_name },
    { key: "a", header: "Факт, порций в сутки", numeric: true, cell: (r) => r.actual_per_day.toLocaleString("ru-RU") },
    { key: "p", header: "Прогноз", numeric: true, cell: (r) => r.predicted_per_day.toLocaleString("ru-RU") },
    { key: "w", header: "Ошибка", numeric: true, cell: (r) => `${r.wape_pct.toLocaleString("ru-RU")} %` },
  ];
  return (
    <Card title="Точность прогноза спроса" style={{ marginBottom: 16 }}>
      <p data-testid="accuracy" style={{ margin: 0, font: "var(--type-h2)" }}>{accuracySentence(acc)}</p>
      {rows.length > 0 && (
        <div style={{ marginTop: 16 }}>
          <DataTable caption="Где прогноз ошибается сильнее всего" columns={columns} rows={rows} rowKey={(r) => r.dish_id} />
        </div>
      )}
    </Card>
  );
}

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
      <AccuracyCard days={days} />
      <div role="group" aria-label="Период" style={{ display: "flex", gap: 8, marginBottom: 16 }}>
        {PERIODS.map((p) => (
          <SecondaryButton key={p.days} aria-pressed={p.days === days} onClick={() => setDays(p.days)}>
            {p.label}
          </SecondaryButton>
        ))}
      </div>
      {state.kind === "loading" && <LoadingState label="Загрузка аналитики" />}
      {state.kind === "error" && <ErrorState what="аналитику" reason={state.message} onRetry={() => void reload()} />}
      {state.kind === "ready" && (
        state.shortages.rejected_orders === 0 && state.shortages.by_product.length === 0 ? (
          // Пусто — только заголовок блока по центру и строка пояснения; заголовок слева — когда есть отказы.
          <Card>
            <div style={{ display: "grid", gap: 6, justifyItems: "center", textAlign: "center", padding: "28px 0" }}>
              <h2 style={{ margin: 0, font: "var(--type-h3)" }}>Дефицит: чего не хватило заказам</h2>
              <p data-testid="rejected-total" style={{ margin: 0, font: "var(--type-body)", color: "var(--text-muted)" }}>
                За период отказов из-за нехватки продуктов не было.
              </p>
            </div>
          </Card>
        ) : (
          <Card title="Дефицит: чего не хватило заказам">
            <p data-testid="rejected-total" style={{ margin: "0 0 8px", font: "var(--type-kpi)" }}>
              {state.shortages.rejected_orders}
            </p>
            <div style={{ marginBottom: 16 }}>
              <HandNote>отклонённых заказов за период</HandNote>
            </div>
            {state.shortages.by_product.length === 0 ? (
              <EmptyState title="Отказов не было" text="За период отказов из-за нехватки продуктов не было." />
            ) : (
              <DataTable caption="Дефицит по продуктам" columns={columns} rows={state.shortages.by_product} rowKey={(r) => r.product_id} />
            )}
          </Card>
        )
      )}
    </section>
  );
}
