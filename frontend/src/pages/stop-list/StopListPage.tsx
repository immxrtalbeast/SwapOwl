import { useCallback, useState } from "react";
import { api } from "../../api/client";
import { useLive } from "../../app/useLive";
import type { DishServings } from "../../api/types";
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

  return (
    <section aria-labelledby="stop-list-title">
      <h2 id="stop-list-title">Меню и стоп-лист</h2>
      {state.kind === "loading" && <p role="status">Загрузка…</p>}
      {state.kind === "error" && (
        <div role="alert">
          <p>Ошибка: {state.message}</p>
          <button onClick={() => { setState({ kind: "loading" }); void load(); }}>Повторить</button>
        </div>
      )}
      {state.kind === "ready" && state.dishes.length === 0 && <p>Блюд в меню нет.</p>}
      {state.kind === "ready" && (
        <ul style={{ listStyle: "none", padding: 0 }}>
          {state.dishes.map((d) => (
            <li
              key={d.id}
              data-testid={`dish-${d.id}`}
              style={{
                padding: "8px 12px",
                marginBottom: 6,
                borderRadius: 6,
                border: `1px solid ${d.available ? "#ddd" : "#b00020"}`,
                background: d.available ? "transparent" : "#fdecee",
              }}
            >
              <strong>{d.name}</strong> — <span style={{ color: d.available ? undefined : "#b00020" }}>{servingsLabel(d)}</span>
              {!d.available && (
                <div style={{ color: "#b00020" }}>
                  <strong>Стоп-лист.</strong> {reasonText(d)}
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
      <button onClick={() => void load()}>Обновить</button>
    </section>
  );
}
