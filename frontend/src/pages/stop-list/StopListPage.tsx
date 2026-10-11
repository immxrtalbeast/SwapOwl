import { useCallback, useState } from "react";
import { api } from "../../api/client";
import { useLive } from "../../app/useLive";
import type { DishServings } from "../../api/types";
import { Card, EmptyState, ErrorState, LoadingState, SecondaryButton, UrgencyBadge } from "../../components";
import { DISH_STATES, dishState } from "../../lib/urgency";
import { reasonText, servingsLabel } from "./reason";

type State =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "ready"; dishes: DishServings[] };

export function StopListPage() {
  const [state, setState] = useState<State>({ kind: "loading" });

  const load = useCallback(async () => {
    try {
      setState({ kind: "ready", dishes: await api.menu() });
    } catch (e) {
      setState({ kind: "error", message: e instanceof Error ? e.message : "Не удалось загрузить меню" });
    }
  }, []);

  useLive(load, ["stoplist.changed", "stock.changed", "stand.reset"]);

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
        <ul style={{ listStyle: "none", padding: 0, margin: 0, display: "grid", gap: 8 }}>
          {state.dishes.map((d) => {
            const st = dishState({ available: d.available, portions: d.servings });
            return (
              <li key={d.id} data-testid={`dish-${d.id}`} data-state={st}>
                <Card style={{ padding: "12px 16px", borderColor: d.available ? undefined : "var(--urgency-critical-fg)" }}>
                  <div style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
                    <strong style={{ font: "var(--type-body-strong)", flex: "1 1 160px" }}>{d.name}</strong>
                    <span style={{ font: "var(--type-num)", color: "var(--text-muted)" }}>{servingsLabel(d)}</span>
                    <UrgencyBadge def={DISH_STATES[st]} />
                  </div>
                  {!d.available && (
                    <p style={{ margin: "8px 0 0", font: "var(--type-small)", color: "var(--urgency-critical-fg)" }}>{reasonText(d)}</p>
                  )}
                </Card>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
