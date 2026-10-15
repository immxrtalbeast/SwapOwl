import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { Logo } from "../../components";
import { isRescueHash, leftLabel, loadRescue, validUntilLabel } from "./model";
import type { RescueState } from "./model";

const REFRESH_MS = 30_000;

function Message({ title, text }: { title: string; text: string }) {
  return (
    <div role="status" style={{ textAlign: "center", padding: "48px 16px", display: "grid", gap: 8 }}>
      <h2 style={{ margin: 0, font: "var(--type-h2)" }}>{title}</h2>
      <p style={{ margin: 0, color: "var(--text-muted)" }}>{text}</p>
    </div>
  );
}

/** Публичная витрина «Спаси продукты»: блюда со скидкой, которые стоит заказать сегодня. Без входа, только чтение. */
export function RescuePage() {
  const [state, setState] = useState<RescueState>({ kind: "loading" });
  useEffect(() => {
    let alive = true;
    const tick = () => void loadRescue().then((s) => { if (alive) setState((prev) => (s.kind === "error" && prev.kind === "ready" ? prev : s)); });
    tick();
    const id = window.setInterval(tick, REFRESH_MS);
    return () => { alive = false; window.clearInterval(id); };
  }, []);

  return (
    <div style={{ minHeight: "100vh", background: "var(--paper)", color: "var(--text)" }}>
      <header style={{ display: "flex", alignItems: "center", gap: 12, padding: "16px var(--gutter, 16px)", background: "var(--surface)", borderBottom: "1px solid var(--border)" }}>
        <Logo withWord size={36} />
      </header>
      <main style={{ maxWidth: 960, margin: "0 auto", padding: "24px var(--gutter, 16px)" }}>
        <h1 style={{ margin: "0 0 8px", font: "var(--type-h1)" }}>Спаси продукты</h1>
        <p style={{ margin: "0 0 24px", color: "var(--text-muted)", maxWidth: 640 }}>
          Блюда, которые ресторан сегодня отдаёт со скидкой, пока продукты свежие. Закажите — и они не попадут в мусор.
        </p>
        {state.kind === "loading" && <div aria-busy="true" role="status" style={{ color: "var(--text-muted)" }}>Загружаем предложения…</div>}
        {state.kind === "off" && <Message title="Витрина пока не включена" text="Загляните позже — ресторан ещё не открыл предложения." />}
        {state.kind === "busy" && <Message title="Слишком много запросов" text="Подождите минуту и обновите страницу." />}
        {state.kind === "error" && <Message title="Не получилось загрузить" text="Нет связи с сервером. Страница обновится сама." />}
        {state.kind === "ready" && state.items.length === 0 && (
          <Message title="Сегодня всё по полной цене" text="Спасать нечего — свежих скидок сейчас нет. Загляните позже." />
        )}
        {state.kind === "ready" && state.items.length > 0 && (
          <ul aria-label="Блюда со скидкой" style={{ listStyle: "none", margin: 0, padding: 0, display: "grid", gap: 16, gridTemplateColumns: "repeat(auto-fill, minmax(260px, 1fr))" }}>
            {state.items.map((i) => (
              <li
                key={i.dish_id}
                data-testid="rescue-item"
                style={{ display: "grid", gap: 8, background: "var(--surface)", border: "1px solid var(--border)", borderRadius: "var(--radius-card)", boxShadow: "var(--shadow-sm)", padding: "var(--card-pad, 16px)", minWidth: 0 }}
              >
                <h2 style={{ margin: 0, font: "var(--type-h2)" }}>{i.dish_name}</h2>
                <p style={{ margin: 0, display: "flex", gap: 10, alignItems: "baseline", flexWrap: "wrap" }}>
                  <span className="so-num" style={{ font: "700 30px/34px var(--font-display)", color: "var(--primary)" }}>−{i.discount_percent} %</span>
                  <span style={{ font: "var(--type-label)" }}>{leftLabel(i.servings_left)}</span>
                </p>
                <p style={{ margin: 0, font: "var(--type-small)", color: "var(--text-muted)" }}>
                  {validUntilLabel(i.valid_until)}{i.note ? ` · ${i.note}` : ""}
                </p>
              </li>
            ))}
          </ul>
        )}
      </main>
    </div>
  );
}

/** Витрина открыта без входа и без лендинга: подменяет приложение по адресу `#/rescue`. */
export function RescueGate({ children }: { children: ReactNode }) {
  const [on, setOn] = useState(() => isRescueHash(window.location.hash));
  useEffect(() => {
    const h = () => setOn(isRescueHash(window.location.hash));
    window.addEventListener("hashchange", h);
    return () => window.removeEventListener("hashchange", h);
  }, []);
  return on ? <RescuePage /> : <>{children}</>;
}
