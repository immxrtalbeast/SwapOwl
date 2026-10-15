import { IconPin, IconFlame } from "@tabler/icons-react";
import { EmptyState, ErrorState, LoadingState, ScreenTitle } from "../../components";
import { hallCards } from "../hot-offers/manager";
import { useHotOffers } from "../hot-offers/useHotOffers";

/** «Для зала» — экран официанта: что предложить гостям сейчас, крупно и без цифр склада. Обновляется сам. */
export function HallPage() {
  const [state, reload] = useHotOffers(null);
  const cards = state.kind === "ready" ? hallCards(state.data) : [];
  return (
    <section>
      <ScreenTitle>Для зала</ScreenTitle>
      <p style={{ marginTop: 0, font: "var(--type-body)", color: "var(--text-muted)" }}>Предложите гостям эти блюда — список меняется сам, когда меняется склад или решение менеджера.</p>
      {state.kind === "loading" && <LoadingState rows={3} label="Загрузка предложений" />}
      {state.kind === "error" && <ErrorState what="предложения для зала" reason={state.message} onRetry={() => void reload()} />}
      {state.kind === "ready" && cards.length === 0 && <EmptyState title="Сегодня без особых предложений" text="Рекомендуйте блюда как обычно." />}
      <div style={{ display: "grid", gap: 16, gridTemplateColumns: "repeat(auto-fill, minmax(260px, 1fr))" }}>
        {cards.map((c) => (
          <article key={c.dishId} data-testid="hall-card" style={{ display: "grid", gap: 10, alignContent: "start", padding: 24, borderRadius: "var(--radius-card)", background: "var(--surface)", border: `${c.byManager ? 2 : 1}px solid ${c.byManager ? "var(--primary)" : "var(--border)"}`, boxShadow: "var(--shadow-sm)" }}>
            <span style={{ display: "flex", alignItems: "center", gap: 6, font: "var(--type-small)", color: c.byManager ? "var(--primary)" : "var(--urgency-expiring-fg, var(--text-muted))" }}>
              {c.byManager ? <IconPin size={16} aria-hidden="true" /> : <IconFlame size={16} aria-hidden="true" />}
              {c.byManager ? "советует менеджер" : "предложите гостю"}
            </span>
            <h2 style={{ margin: 0, fontSize: 28, lineHeight: 1.15 }}>{c.dish}</h2>
            {c.why && <p style={{ margin: 0, font: "var(--type-body)" }}>{c.why}</p>}
          </article>
        ))}
      </div>
    </section>
  );
}
