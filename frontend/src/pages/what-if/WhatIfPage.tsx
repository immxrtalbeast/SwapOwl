import { useEffect, useRef, useState } from "react";
import { NativeSelect, TextInput } from "@mantine/core";
import { IconPlus, IconTrash } from "@tabler/icons-react";
import { api } from "../../api/client";
import type { Dish, ProductStock, WhatIfResponse } from "../../api/types";
import { Card, DataTable, ErrorState, LoadingState, PrimaryButton, ScreenTitle, SecondaryButton } from "../../components";
import type { Column } from "../../components";
import { formatRub } from "../money/format";
import { HORIZONS, MAX_SCENARIOS, SPECS, TYPE_ORDER, buildRequest, changedProducts, newDraft, scenarioTitle, verdict } from "./model";
import type { Draft } from "./model";

const TONE: Record<string, string> = {
  good: "var(--urgency-ok-fg)",
  bad: "var(--urgency-critical-fg)",
  same: "var(--text-muted)",
};

type Catalog = { kind: "loading" } | { kind: "error"; message: string } | { kind: "ready"; dishes: Dish[]; products: ProductStock[] };

function useCatalog() {
  const [state, setState] = useState<Catalog>({ kind: "loading" });
  const load = () => {
    setState({ kind: "loading" });
    Promise.all([api.dishes(), api.products()])
      .then(([dishes, products]) => setState({ kind: "ready", dishes, products }))
      .catch((e: unknown) => setState({ kind: "error", message: e instanceof Error ? e.message : "Нет связи с сервером" }));
  };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(load, []);
  return [state, load] as const;
}

const horizonLabel = (h: number) => `${h} ${h === 1 ? "день" : h < 5 ? "дня" : "дней"}`;

const productColumns: Column<{ product_id: string; product_name: string; base_minor: number; scenario_minor: number; delta_minor: number }>[] = [
  { key: "p", header: "Продукт", cell: (r) => r.product_name },
  { key: "b", header: "Если ничего не делать", numeric: true, cell: (r) => formatRub(r.base_minor) },
  { key: "s", header: "По сценарию", numeric: true, cell: (r) => formatRub(r.scenario_minor) },
  { key: "d", header: "Разница", numeric: true, cell: (r) => (r.delta_minor > 0 ? "+" : "") + formatRub(r.delta_minor) },
];

function DraftRow({ d, dishes, products, error, canRemove, onChange, onRemove }: {
  d: Draft; dishes: readonly Dish[]; products: readonly ProductStock[]; error?: string; canRemove: boolean;
  onChange: (next: Draft) => void; onRemove: () => void;
}) {
  const spec = SPECS[d.type];
  const options = spec.target === "dish" ? dishes.map((x) => ({ value: x.id, label: x.name })) : products.map((x) => ({ value: x.id, label: x.name }));
  return (
    <li data-testid="whatif-row" style={{ display: "grid", gap: 6, listStyle: "none" }}>
      <div style={{ display: "flex", gap: 12, flexWrap: "wrap", alignItems: "flex-end" }}>
        <NativeSelect
          label="Сценарий"
          data={TYPE_ORDER.map((t) => ({ value: t, label: SPECS[t].label }))}
          value={d.type}
          onChange={(e) => {
            const t = e.currentTarget.value as Draft["type"];
            const s = SPECS[t];
            const keep = s.target === SPECS[d.type].target ? d.targetId : ((s.target === "dish" ? dishes[0]?.id : products[0]?.id) ?? "");
            onChange({ ...d, type: t, targetId: keep, value: s.start });
          }}
          style={{ minWidth: 190 }}
        />
        <NativeSelect
          label={spec.target === "dish" ? "Блюдо" : "Продукт"}
          data={options}
          value={d.targetId}
          onChange={(e) => onChange({ ...d, targetId: e.currentTarget.value })}
          style={{ minWidth: 220, flex: 1 }}
        />
        <TextInput
          label={`Значение, ${spec.unit}`}
          description={spec.hint}
          inputMode="decimal"
          value={d.value}
          onChange={(e) => onChange({ ...d, value: e.currentTarget.value })}
          error={error ? true : undefined}
          style={{ width: 170 }}
        />
        {canRemove && (
          <SecondaryButton size="sm" aria-label="Убрать сценарий" leftSection={<IconTrash size={16} aria-hidden="true" />} onClick={onRemove}>Убрать</SecondaryButton>
        )}
      </div>
      {error && <span role="alert" style={{ color: "var(--urgency-critical-fg)", font: "var(--type-small)" }}>{error}</span>}
    </li>
  );
}

function Results({ res, dishes, products }: { res: WhatIfResponse; dishes: readonly Dish[]; products: readonly ProductStock[] }) {
  return (
    <div data-testid="whatif-results" style={{ display: "grid", gap: 16 }}>
      <Card title={`Если ничего не делать — ${horizonLabel(res.horizon_days)}`}>
        <p style={{ margin: 0, font: "var(--type-kpi)" }} data-testid="whatif-base">≈ {formatRub(res.base.total_minor)}</p>
        <p style={{ margin: "4px 0 0", font: "var(--type-small)", color: "var(--text-muted)" }}>
          Прогноз списаний по текущему складу и обычному спросу — так же считает экран «Деньги».
        </p>
      </Card>
      {res.scenarios.map((s, i) => {
        const v = verdict(s);
        const rows = changedProducts(s.products);
        const title = scenarioTitle(s, dishes, products);
        return (
          <Card key={i} title={title}>
            <p data-testid="whatif-verdict" style={{ margin: 0, font: "var(--type-h2)", color: TONE[v.tone] }}>{v.headline}</p>
            <p style={{ margin: "2px 0 12px", font: "var(--type-small)", color: "var(--text-muted)" }}>
              {v.detail} · списания по сценарию ≈ {formatRub(s.total_minor)}
            </p>
            {rows.length > 0 && <DataTable caption={`Что меняется: ${title}`} columns={productColumns} rows={rows} rowKey={(r) => r.product_id} />}
            {s.assumptions.length > 0 && (
              <ul aria-label="Допущения" style={{ margin: "12px 0 0", paddingLeft: 18, font: "var(--type-small)", color: "var(--text-muted)" }}>
                {s.assumptions.map((a, j) => <li key={j}>{a}</li>)}
              </ul>
            )}
          </Card>
        );
      })}
    </div>
  );
}

/** «Что если»: прикинуть эффект скидки, меньшего заказа или другого спроса на списания. Ничего не записывает. */
export function WhatIfPage() {
  const [cat, reload] = useCatalog();
  const keys = useRef(1);
  const [horizon, setHorizon] = useState(3);
  const [drafts, setDrafts] = useState<Draft[]>([]);
  const [errors, setErrors] = useState<Record<number, string>>({});
  const [busy, setBusy] = useState(false);
  const [fail, setFail] = useState<string | null>(null);
  const [res, setRes] = useState<WhatIfResponse | null>(null);

  // Первая строка — скидка на первое блюдо: экран сразу готов посчитать.
  useEffect(() => {
    if (cat.kind === "ready" && drafts.length === 0) setDrafts([newDraft(keys.current++, "discount", cat.dishes, cat.products)]);
  }, [cat, drafts.length]);

  if (cat.kind === "loading") return (<section><ScreenTitle>Что если</ScreenTitle><LoadingState label="Загрузка блюд и продуктов" /></section>);
  if (cat.kind === "error") return (<section><ScreenTitle>Что если</ScreenTitle><ErrorState what="блюда и продукты" reason={cat.message} onRetry={reload} /></section>);

  const run = async () => {
    const built = buildRequest(horizon, drafts);
    setErrors(built.errors);
    setFail(null);
    if (!built.request) return;
    setBusy(true);
    try {
      setRes(await api.whatIf(built.request));
    } catch (e) {
      setFail(e instanceof Error ? e.message : "Не удалось посчитать");
    } finally {
      setBusy(false);
    }
  };

  return (
    <section>
      <ScreenTitle>Что если</ScreenTitle>
      <p style={{ margin: "0 0 16px", color: "var(--text-muted)", maxWidth: 720 }}>
        Прикиньте, как скидка, меньший заказ или другой спрос повлияют на списания. Склад и стоп-лист не меняются — это только расчёт.
      </p>
      <Card title="Сценарии">
        <form onSubmit={(e) => { e.preventDefault(); void run(); }} style={{ display: "grid", gap: 16 }}>
          <NativeSelect
            label="Горизонт"
            data={HORIZONS.map((h) => ({ value: String(h), label: horizonLabel(h) }))}
            value={String(horizon)}
            onChange={(e) => setHorizon(Number(e.currentTarget.value))}
            style={{ width: 160 }}
          />
          <ul aria-label="Сценарии" style={{ margin: 0, padding: 0, display: "grid", gap: 16 }}>
            {drafts.map((d) => (
              <DraftRow
                key={d.key} d={d} dishes={cat.dishes} products={cat.products} error={errors[d.key]} canRemove={drafts.length > 1}
                onChange={(next) => setDrafts((all) => all.map((x) => (x.key === d.key ? next : x)))}
                onRemove={() => setDrafts((all) => all.filter((x) => x.key !== d.key))}
              />
            ))}
          </ul>
          {errors[-1] && <span role="alert" style={{ color: "var(--urgency-critical-fg)" }}>{errors[-1]}</span>}
          <div style={{ display: "flex", gap: 12, flexWrap: "wrap" }}>
            <SecondaryButton
              disabled={drafts.length >= MAX_SCENARIOS}
              leftSection={<IconPlus size={16} aria-hidden="true" />}
              onClick={() => setDrafts((all) => [...all, newDraft(keys.current++, "order_less", cat.dishes, cat.products)])}
            >
              Добавить сценарий
            </SecondaryButton>
            <PrimaryButton type="submit" disabled={busy}>{busy ? "Считаем…" : "Посчитать"}</PrimaryButton>
          </div>
          {fail && <p role="alert" style={{ margin: 0, color: "var(--urgency-critical-fg)" }}>{fail}</p>}
        </form>
      </Card>
      {res && <div style={{ marginTop: 24 }}><Results res={res} dishes={cat.dishes} products={cat.products} /></div>}
    </section>
  );
}
