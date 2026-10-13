import { useState } from "react";
import { Card, DataTable, EmptyState, ErrorState, LoadingState, Logo, PrimaryButton, ScreenTitle } from "../../components";
import type { Column } from "../../components";
import { formatQty } from "../stock/format";
import { formatRub, reasonLabel } from "../money/format";
import { dishLines, sumMinor, sumPortions } from "./report";
import type { DishLine } from "./report";
import { useShiftReport } from "./useShiftReport";

// Печать: одна страница A4, виден только отчёт, крупные цифры.
const PRINT_CSS = `
@page { size: A4; margin: 12mm; }
@media print {
  body * { visibility: hidden !important; }
  .shift-report, .shift-report * { visibility: visible !important; }
  .shift-report { position: absolute; left: 0; top: 0; width: 100%; font-size: 11pt; }
  .shift-report .no-print { display: none !important; }
  .shift-report .print-only { display: block !important; }
  .shift-report .big { font-size: 22pt !important; }
  .shift-report section, .shift-report table { break-inside: avoid; }
  .shift-report .grid { gap: 8px !important; }
}
.shift-report .print-only { display: none; }
`;

const big = { margin: 0, font: "var(--type-kpi)", color: "var(--text)" } as const;
const money = (m: number | null) => (m === null ? "цена блюда не отдаётся API" : formatRub(m));

function Kpi({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div>
      <div style={{ color: "var(--text-muted, inherit)" }}>{label}</div>
      <p className="big" style={big}>{value}</p>
      {hint && <div style={{ fontSize: 12 }}>{hint}</div>}
    </div>
  );
}

const lineColumns = (): Column<DishLine>[] => [
  { key: "dish", header: "Блюдо", cell: (l) => l.name },
  { key: "portions", header: "Порций", numeric: true, cell: (l) => l.portions },
  { key: "minor", header: "₽", numeric: true, cell: (l) => (l.minor === null ? "—" : formatRub(l.minor)) },
];

export function ShiftReportPage() {
  const [picked, setPicked] = useState<string | null>(null);
  const [state, today, reload] = useShiftReport(picked);
  return (
    <section className="shift-report">
      <style>{PRINT_CSS}</style>
      <div className="print-only" style={{ marginBottom: 8 }}><Logo withWord size={36} /></div>
      <ScreenTitle>Отчёт смены</ScreenTitle>
      <div className="no-print" style={{ display: "flex", gap: 12, alignItems: "center", marginBottom: 16 }}>
        <label>
          Дата{" "}
          <input type="date" value={picked ?? today ?? ""} onChange={(e) => setPicked(e.target.value || null)} />
        </label>
        <PrimaryButton onClick={() => window.print()}>Печать</PrimaryButton>
      </div>
      {state.kind === "loading" && <LoadingState label="Загрузка отчёта смены" />}
      {state.kind === "error" && <ErrorState what="отчёт смены" reason={state.message} onRetry={() => void reload()} />}
      {state.kind === "ready" && (() => {
        const d = state.data;
        const sold = dishLines(d.accepted, d.dishes, d.date, d.timeZone);
        const lost = dishLines(d.rejected, d.dishes, d.date, d.timeZone);
        const soldMinor = sumMinor(sold);
        const lostMinor = sumMinor(lost);
        const now = d.supply.filter((s) => s.urgency === "now");
        return (
          <div className="grid" style={{ display: "grid", gap: 20 }}>
            <p style={{ margin: 0 }}>Сутки стенда: {new Date(`${d.date}T00:00:00`).toLocaleDateString("ru-RU", { day: "numeric", month: "long", year: "numeric" })}</p>
            <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(180px, 1fr))", gap: 16 }}>
              <Kpi label="Продано" value={soldMinor === null ? `${sumPortions(sold)} порц.` : formatRub(soldMinor)} hint={`${sumPortions(sold)} порций`} />
              <Kpi label="Списано" value={formatRub(d.writeoffs.total_minor)} />
              <Kpi label="Стоп стоил" value={lostMinor === null ? `${sumPortions(lost)} порц.` : `≈ ${formatRub(lostMinor)}`} hint={`${d.shortages.rejected_orders} отказов`} />
            </div>
            <Card title="Продано">
              {sold.length === 0 ? <EmptyState title="Ничего нет" text="За сутки заказов не было." /> : <DataTable caption="Продано по блюдам" columns={lineColumns()} rows={sold} rowKey={(l) => l.dish_id} />}
              {soldMinor === null && sold.length > 0 && <p style={{ fontSize: 12 }}>Рубли не посчитаны: {money(null)}.</p>}
            </Card>
            <Card title="Списано по причинам">
              {d.writeoffs.by_product.length === 0 ? <EmptyState title="Ничего нет" text="Списаний нет." /> : (
                <DataTable caption="Списано по причинам" columns={[
                  { key: "p", header: "Продукт", cell: (r) => r.product_name },
                  { key: "r", header: "Причина", cell: (r) => reasonLabel(r.reason) },
                  { key: "q", header: "Количество", numeric: true, cell: (r) => formatQty(r.quantity) },
                  { key: "c", header: "₽", numeric: true, cell: (r) => formatRub(r.cost_minor) },
                ]} rows={d.writeoffs.by_product} rowKey={(r) => `${r.product_id}/${r.reason ?? ""}`} />
              )}
            </Card>
            <Card title="Цена стопа">
              <p data-testid="stop-cost" className="big" style={big}>
                {lostMinor === null ? "недополучено: цены блюд нет в API" : `недополучено ≈ ${formatRub(lostMinor)}`}
              </p>
              <p style={{ fontSize: 12 }}>
                Истории стоп-листа API не хранит, поэтому считаем по отказам: порции отклонённых заказов × цена блюда.
              </p>
              {lost.length > 0 && <DataTable caption="Недополучено по блюдам" columns={lineColumns()} rows={lost} rowKey={(l) => l.dish_id} />}
              {d.shortages.by_product.length > 0 && (
                <DataTable caption="Чего не хватило" columns={[
                  { key: "p", header: "Продукт", cell: (r) => r.product_name },
                  { key: "o", header: "Отказов", numeric: true, cell: (r) => r.rejected_orders },
                  { key: "m", header: "Не хватило", numeric: true, cell: (r) => formatQty(r.missing) },
                ]} rows={d.shortages.by_product} rowKey={(r) => r.product_id} />
              )}
            </Card>
            <Card title="Что заказать завтра">
              {now.length === 0 ? <EmptyState title="Ничего срочного" text="Срочных закупок нет." /> : (
                <DataTable caption="Заказать сейчас" columns={[
                  { key: "p", header: "Продукт", cell: (s) => s.product_name },
                  { key: "q", header: "Заказать", numeric: true, cell: (s) => formatQty(s.quantity) },
                  { key: "b", header: "Блюд под угрозой", numeric: true, cell: (s) => s.blocks_dishes },
                ]} rows={now} rowKey={(s) => s.product_id} />
              )}
            </Card>
            <Card title="Прогноз под угрозой на неделю">
              <p className="big" style={big}>{formatRub(d.forecast.total_minor)}</p>
              {d.forecast.products.length > 0 && (
                <DataTable caption="Под угрозой по продуктам" columns={[
                  { key: "p", header: "Продукт", cell: (r) => r.product_name },
                  { key: "q", header: "Объём", numeric: true, cell: (r) => formatQty(r.at_risk) },
                  { key: "c", header: "₽", numeric: true, cell: (r) => formatRub(r.total_minor) },
                ]} rows={d.forecast.products} rowKey={(r) => r.product_id} />
              )}
            </Card>
          </div>
        );
      })()}
    </section>
  );
}
