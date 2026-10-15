import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { Logo } from "../../components";
import { GUEST_MENU_HASH, MENU_STATUS_LABEL, RESCUE_HASH, isGuestMenuHash, isRescueHash, leftLabel, loadGuestMenu, loadRescue, sortGuestMenu, validUntilLabel } from "./model";
import type { GuestMenuState, RescueState } from "./model";

const REFRESH_MS = 30_000;

function Message({ title, text }: { title: string; text: string }) {
  return (
    <div role="status" style={{ textAlign: "center", padding: "48px 16px", display: "grid", gap: 8 }}>
      <h2 style={{ margin: 0, font: "var(--type-h2)" }}>{title}</h2>
      <p style={{ margin: 0, color: "var(--text-muted)" }}>{text}</p>
    </div>
  );
}

/** Опрос публичной ручки: сразу и раз в 30 с; сбой после успешного ответа не стирает показанное. */
function usePolled<T extends { kind: string }>(load: () => Promise<T>): T | { kind: "loading" } {
  const [state, setState] = useState<T | { kind: "loading" }>({ kind: "loading" });
  useEffect(() => {
    let alive = true;
    const tick = () => void load().then((s) => { if (alive) setState((prev) => (s.kind === "error" && prev.kind === "ready" ? prev : s)); });
    tick();
    const id = window.setInterval(tick, REFRESH_MS);
    return () => { alive = false; window.clearInterval(id); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  return state;
}

function PublicShell({ current, title, lead, children }: { current: "rescue" | "menu"; title: string; lead: string; children: ReactNode }) {
  const link = (href: string, text: string, on: boolean) => (
    <a
      href={href}
      aria-current={on ? "page" : undefined}
      style={{ padding: "8px 14px", borderRadius: "var(--radius-pill)", textDecoration: "none", font: "var(--type-label)", color: on ? "var(--brand-ink)" : "var(--text-muted)", background: on ? "var(--brand-tint)" : "transparent" }}
    >
      {text}
    </a>
  );
  return (
    <div style={{ minHeight: "100vh", background: "var(--paper)", color: "var(--text)" }}>
      <header style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap", padding: "12px var(--gutter, 16px)", background: "var(--surface)", borderBottom: "1px solid var(--border)" }}>
        <Logo withWord size={36} />
        <nav aria-label="Разделы для гостей" style={{ display: "flex", gap: 4, marginLeft: "auto" }}>
          {link(RESCUE_HASH, "Спаси продукты", current === "rescue")}
          {link(GUEST_MENU_HASH, "Меню", current === "menu")}
        </nav>
      </header>
      <main style={{ maxWidth: 960, margin: "0 auto", padding: "24px var(--gutter, 16px)" }}>
        <h1 style={{ margin: "0 0 8px", font: "var(--type-h1)" }}>{title}</h1>
        <p style={{ margin: "0 0 24px", color: "var(--text-muted)", maxWidth: 640 }}>{lead}</p>
        {children}
      </main>
    </div>
  );
}

const grid = { listStyle: "none", margin: 0, padding: 0, display: "grid", gap: 16, gridTemplateColumns: "repeat(auto-fill, minmax(260px, 1fr))" } as const;
const card = { display: "grid", gap: 8, background: "var(--surface)", border: "1px solid var(--border)", borderRadius: "var(--radius-card)", boxShadow: "var(--shadow-sm)", padding: "var(--card-pad, 16px)", minWidth: 0 } as const;

/** Публичная витрина «Спаси продукты»: блюда со скидкой, которые стоит заказать сегодня. Без входа, только чтение. */
export function RescuePage() {
  const state: RescueState = usePolled(loadRescue);
  return (
    <PublicShell
      current="rescue"
      title="Спаси продукты"
      lead="Блюда, которые ресторан сегодня отдаёт со скидкой, пока продукты свежие. Закажите — и они не попадут в мусор."
    >
      {state.kind === "loading" && <div aria-busy="true" role="status" style={{ color: "var(--text-muted)" }}>Загружаем предложения…</div>}
      {state.kind === "off" && <Message title="Витрина пока не включена" text="Загляните позже — ресторан ещё не открыл предложения." />}
      {state.kind === "busy" && <Message title="Слишком много запросов" text="Подождите минуту и обновите страницу." />}
      {state.kind === "error" && <Message title="Не получилось загрузить" text="Нет связи с сервером. Страница обновится сама." />}
      {state.kind === "ready" && state.items.length === 0 && (
        <Message title="Сегодня всё по полной цене" text="Спасать нечего — свежих скидок сейчас нет. Загляните позже." />
      )}
      {state.kind === "ready" && state.items.length > 0 && (
        <ul aria-label="Блюда со скидкой" style={grid}>
          {state.items.map((i) => (
            <li key={i.dish_id} data-testid="rescue-item" style={card}>
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
    </PublicShell>
  );
}

const STATUS_COLOR = { available: "var(--urgency-ok-fg)", few: "var(--urgency-risk-fg)", stopped: "var(--text-muted)" } as const;

/** Публичное меню гостя со стоп-листом: что можно заказать сейчас. Без входа, только чтение. */
export function GuestMenuPage() {
  const state: GuestMenuState = usePolled(loadGuestMenu);
  return (
    <PublicShell current="menu" title="Меню" lead="Что можно заказать прямо сейчас. Блюда, которых нет в наличии, отмечены и убраны вниз.">
      {state.kind === "loading" && <div aria-busy="true" role="status" style={{ color: "var(--text-muted)" }}>Загружаем меню…</div>}
      {state.kind === "off" && <Message title="Меню пока не опубликовано" text="Загляните позже." />}
      {state.kind === "busy" && <Message title="Слишком много запросов" text="Подождите минуту и обновите страницу." />}
      {state.kind === "error" && <Message title="Не получилось загрузить" text="Нет связи с сервером. Страница обновится сама." />}
      {state.kind === "ready" && state.items.length === 0 && <Message title="Меню пока пусто" text="Загляните позже." />}
      {state.kind === "ready" && state.items.length > 0 && (
        <ul aria-label="Меню" style={grid}>
          {sortGuestMenu(state.items).map((d) => (
            <li key={d.dish_id} data-testid="guest-dish" data-status={d.status} style={{ ...card, opacity: d.status === "stopped" ? 0.7 : 1 }}>
              <h2 style={{ margin: 0, font: "var(--type-h2)" }}>{d.dish_name}</h2>
              <p style={{ margin: 0, font: "var(--type-label)", color: STATUS_COLOR[d.status] }}>{MENU_STATUS_LABEL[d.status]}</p>
            </li>
          ))}
        </ul>
      )}
    </PublicShell>
  );
}

/** Публичные страницы гостя открываются без входа и без лендинга: подменяют приложение по `#/rescue` и `#/guest-menu`. */
export function RescueGate({ children }: { children: ReactNode }) {
  const read = () => (isGuestMenuHash(window.location.hash) ? "menu" : isRescueHash(window.location.hash) ? "rescue" : null);
  const [page, setPage] = useState(read);
  useEffect(() => {
    const h = () => setPage(read());
    window.addEventListener("hashchange", h);
    return () => window.removeEventListener("hashchange", h);
  }, []);
  if (page === "menu") return <GuestMenuPage />;
  if (page === "rescue") return <RescuePage />;
  return <>{children}</>;
}
