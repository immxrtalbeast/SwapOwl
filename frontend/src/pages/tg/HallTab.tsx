import { useState } from "react";
import { IconBan, IconFlame, IconPin } from "@tabler/icons-react";
import { EmptyState, ErrorState, LoadingState, PrimaryButton } from "../../components";
import { hallCards } from "../hot-offers/manager";
import { useHotOffers } from "../hot-offers/useHotOffers";
import { OrderForm } from "../orders/OrderForm";
import { reasonText } from "../stop-list/reason";
import { stoppedDishes } from "./model";
import { haptic } from "./sdk";
import { InlineBack, TgCard, heading, listReset, muted, section, useBackButton, useMainButton } from "./ui";

/** «Зал»: что предлагать гостям, стоп-лист с причинами, быстрый заказ. */
export function HallTab() {
  const [state, reload] = useHotOffers(null);
  const [ordering, setOrdering] = useState(false);
  useBackButton(ordering ? () => setOrdering(false) : null);
  const inline = useMainButton(ordering ? null : "Быстрый заказ", () => setOrdering(true));

  if (state.kind === "loading") return <LoadingState rows={3} label="Загрузка предложений" />;
  if (state.kind === "error") return <ErrorState what="предложения для зала" reason={state.message} onRetry={() => void reload()} />;

  if (ordering)
    return (
      <div style={section}>
        <InlineBack onBack={() => setOrdering(false)} />
        <h2 style={heading}>Быстрый заказ</h2>
        <OrderForm
          dishes={state.menu}
          onPlaced={async () => {
            haptic("success");
            await reload();
          }}
        />
      </div>
    );

  const cards = hallCards(state.data);
  const stopped = stoppedDishes(state.menu);
  return (
    <div style={section}>
      <h2 style={heading}>Предложите гостям</h2>
      {cards.length === 0 && <EmptyState title="Сегодня без особых предложений" text="Рекомендуйте блюда как обычно." />}
      <ul aria-label="Предложить гостям" style={listReset}>
        {cards.map((c) => (
          <li key={c.dishId}>
            <TgCard accent={c.byManager} testid="tg-hall-card">
              <span style={{ display: "flex", alignItems: "center", gap: 6, font: "var(--type-small)", color: c.byManager ? "var(--primary)" : "var(--text-muted)" }}>
                {c.byManager ? <IconPin size={16} aria-hidden="true" /> : <IconFlame size={16} aria-hidden="true" />}
                {c.byManager ? "советует менеджер" : "предложите гостю"}
              </span>
              <strong style={{ fontSize: 22, lineHeight: 1.15 }}>{c.dish}</strong>
              {c.why && <span>{c.why}</span>}
            </TgCard>
          </li>
        ))}
      </ul>

      <h2 style={{ ...heading, marginTop: 8 }}>Стоп-лист</h2>
      {stopped.length === 0 && <p style={muted}>Всё меню доступно.</p>}
      <ul aria-label="Стоп-лист" style={listReset}>
        {stopped.map((d) => (
          <li key={d.id}>
            <TgCard testid="tg-stop-row">
              <span style={{ display: "flex", alignItems: "center", gap: 6 }}>
                <IconBan size={16} aria-hidden="true" color="var(--urgency-critical-fg)" />
                <strong>{d.name}</strong>
              </span>
              <span style={muted}>{reasonText(d)}</span>
            </TgCard>
          </li>
        ))}
      </ul>
      {inline && <PrimaryButton style={{ height: 48 }} onClick={() => setOrdering(true)}>Быстрый заказ</PrimaryButton>}
    </div>
  );
}
