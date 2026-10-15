import { useCallback, useState } from "react";
import { api } from "../../api/client";
import { useLive } from "../../app/useLive";
import type { DishHold, DishServings } from "../../api/types";
import { RoleGate } from "../../auth/AuthContext";
import { Card, EmptyState, ErrorState, LoadingState, SecondaryButton, UrgencyBadge } from "../../components";
import { DISH_STATES, dishState } from "../../lib/urgency";
import { reasonText, servingsLabel } from "./reason";

type State =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "ready"; dishes: DishServings[]; holds: Record<string, DishHold> };

/** Пауза блюда: «Поставить на паузу» / «Снять паузу» — только operator и выше. Ошибка — в той же строке. */
function HoldControl({ dish, held, onChanged }: { dish: DishServings; held: DishHold | undefined; onChanged: () => Promise<void> }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [note, setNote] = useState("");
  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setError(null);
    try {
      await fn();
      setNote("");
      await onChanged();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Не удалось сохранить");
    } finally {
      setBusy(false);
    }
  };
  return (
    <RoleGate min="operator">
      <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap", marginTop: 8 }}>
        {held ? (
          <SecondaryButton size="xs" disabled={busy} onClick={() => void run(() => api.deleteDishHold(dish.id))}>Снять паузу</SecondaryButton>
        ) : (
          <>
            <input
              aria-label={`Причина паузы: ${dish.name}`}
              placeholder="Причина (необязательно)"
              maxLength={200}
              value={note}
              onChange={(e) => setNote(e.currentTarget.value)}
              style={{ font: "var(--type-small)", padding: "4px 8px", border: "1px solid var(--border)", borderRadius: 8, background: "var(--surface)", color: "var(--text)", minWidth: 180 }}
            />
            <SecondaryButton size="xs" disabled={busy} onClick={() => void run(() => api.setDishHold(dish.id, note))}>Поставить на паузу</SecondaryButton>
          </>
        )}
        {error && <span role="alert" style={{ color: "var(--urgency-critical-fg)", font: "var(--type-small)" }}>{error}</span>}
      </div>
    </RoleGate>
  );
}

export function StopListPage() {
  const [state, setState] = useState<State>({ kind: "loading" });

  const load = useCallback(async () => {
    try {
      const [dishes, list] = await Promise.all([api.menu(), api.dishHolds().catch(() => [] as DishHold[])]);
      setState({ kind: "ready", dishes, holds: Object.fromEntries(list.map((h) => [h.dish_id, h])) });
    } catch (e) {
      setState({ kind: "error", message: e instanceof Error ? e.message : "Не удалось загрузить меню" });
    }
  }, []);

  useLive(load, ["stoplist.changed", "stock.changed", "stand.reset"]);
  const reload = async () => { await load(); };

  const retry = () => {
    setState({ kind: "loading" });
    void load();
  };

  return (
    <section aria-labelledby="stop-list-title">
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, flexWrap: "wrap", marginBottom: 16 }}>
        <h1 id="stop-list-title" style={{ margin: 0, font: "var(--type-h1)" }}>Меню и стоп-лист</h1>
        <SecondaryButton onClick={() => void load()}>Обновить</SecondaryButton>
      </div>
      {state.kind === "loading" && <LoadingState label="Загрузка меню" />}
      {state.kind === "error" && <ErrorState what="меню" reason={state.message} onRetry={retry} />}
      {state.kind === "ready" && state.dishes.length === 0 && <EmptyState title="Блюд нет" text="Блюд в меню нет." />}
      {state.kind === "ready" && state.dishes.length > 0 && (
        // Одна карточка, строки через линию (FS-161): блюдо из стоп-листа — красная полоса слева и причина.
        <Card flush>
          <ul style={{ listStyle: "none", padding: 0, margin: 0 }}>
            {state.dishes.map((d, i) => {
              const st = dishState({ available: d.available, portions: d.servings });
              const held = state.holds[d.id];
              return (
                <li
                  key={d.id}
                  data-testid={`dish-${d.id}`}
                  data-state={st}
                  style={{
                    padding: "12px 16px", borderTop: i > 0 ? "1px solid var(--border-cell)" : undefined,
                    boxShadow: d.available && !held ? undefined : "inset 3px 0 0 var(--urgency-critical-solid)",
                    background: d.available && !held ? undefined : "var(--urgency-critical-bg)",
                  }}
                >
                  <div style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
                    <strong style={{ font: "var(--type-body-strong)", flex: "1 1 160px" }}>{d.name}</strong>
                    <span style={{ font: "var(--type-num)", color: "var(--text-muted)", whiteSpace: "nowrap" }}>{servingsLabel(d)}</span>
                    <span style={{ minWidth: 112, display: "inline-flex", justifyContent: "flex-end" }}><UrgencyBadge def={DISH_STATES[st]} /></span>
                  </div>
                  {held && (
                    <p data-testid={`hold-${d.id}`} style={{ margin: "6px 0 0", font: "var(--type-small)", color: "var(--urgency-critical-fg)" }}>
                      На паузе у менеджера{held.note ? `: ${held.note}` : ""} — заказать нельзя
                    </p>
                  )}
                  {!d.available && (
                    <p style={{ margin: "6px 0 0", font: "var(--type-small)", color: "var(--urgency-critical-fg)" }}>{reasonText(d)}</p>
                  )}
                  <HoldControl dish={d} held={held} onChanged={reload} />
                </li>
              );
            })}
          </ul>
        </Card>
      )}
    </section>
  );
}
