import { useEffect, useState } from "react";
import type { HotAlgo, HotOffer } from "../../api/types";
import { Card, ErrorState, LoadingState, EmptyState, ScreenTitle, SecondaryButton } from "../../components";
import { formatQty } from "../stock/format";
import { productIdFromHash } from "./query";
import { RealisticBlock } from "./RealisticBlock";
import { realisticTarget } from "./realistic";
import { useHotOffers } from "./useHotOffers";
import { useUnitPrices } from "./useUnitPrices";
import { ManagerPanel, OfferActions } from "./ManagerControls";
import { IconPin } from "@tabler/icons-react";
import type { UnitPrices } from "../../api/today";
import { filterByProduct, formatUntil, handNote, portionsWord } from "./view";

function useProductFilter(): string | null {
  const [id, setId] = useState(() => productIdFromHash(window.location.hash));
  useEffect(() => {
    const on = () => setId(productIdFromHash(window.location.hash));
    window.addEventListener("hashchange", on);
    return () => window.removeEventListener("hashchange", on);
  }, []);
  return id;
}

function OfferCard({ offer, note, prices, onChanged }: { offer: HotOffer; note?: string; prices: UnitPrices; onChanged: () => void | Promise<void> }) {
  const hasTarget = offer.target_portions > 0;
  const real = realisticTarget(offer);
  const capped = real !== null;
  // Одна строка цели у всех карточек (FS-161): число порций крупно, «реально» — если цель урезана до 2× обычного.
  const goal = real ? real.real : offer.target_today > 0 ? offer.target_today : offer.target_portions;
  return (
    <article
      data-testid="hot-offer"
      style={{ display: "flex", flexDirection: "column", gap: 10, background: "var(--surface)", border: "1px solid var(--border)", borderRadius: "var(--radius-card)", boxShadow: "var(--shadow-sm)", padding: "var(--card-pad)", minWidth: 0 }}
    >
      <h2 style={{ margin: 0, font: "var(--type-h2)" }}>{offer.dish_name}</h2>
      {offer.pinned && (
        <p style={{ margin: 0, display: "flex", gap: 6, alignItems: "center", font: "var(--type-small)", color: "var(--primary)" }}>
          <IconPin size={14} aria-hidden="true" /> закреплено менеджером{offer.manager_note ? ` — ${offer.manager_note}` : ""}
        </p>
      )}
      {hasTarget && (
        <div style={{ display: "grid", gap: 2 }}>
          <p style={{ margin: 0, display: "flex", alignItems: "baseline", gap: 8, flexWrap: "wrap" }}>
            <span style={{ font: "var(--type-label)", color: "var(--text-muted)" }}>{capped ? "реально продать" : "продать"}</span>
            <span className="so-num" style={{ font: "700 30px/34px var(--font-display)", color: "var(--primary)" }}>{goal}</span>
            <span style={{ font: "var(--type-label)" }}>
              {portionsWord(goal)}
              {offer.target_today > 0 || capped ? " сегодня" : " в ближайшую смену"}
            </span>
          </p>
          <span style={{ font: "var(--type-small)", color: "var(--text-muted)" }}>
            {offer.target_today > 0 && <>всего {offer.target_portions} · </>}<span title={new Date(offer.sell_until).toLocaleString("ru-RU")}>{formatUntil(offer.sell_until)}</span> · обычно {offer.usual_portions}
          </span>
          {note && <span style={{ font: "var(--type-label)", color: "var(--urgency-risk-fg)" }}>{note}</span>}
        </div>
      )}
      {capped && <RealisticBlock offer={offer} prices={prices} compact />}
      {!hasTarget && (
        <p style={{ margin: 0, font: "var(--type-small)", color: "var(--text-muted)", display: "flex", gap: 12, flexWrap: "wrap" }}>
          <span title={new Date(offer.sell_until).toLocaleString("ru-RU")}>{formatUntil(offer.sell_until)}</span>
          {note && <span>{note}</span>}
        </p>
      )}
      <ul style={{ margin: 0, padding: 0, listStyle: "none", font: "var(--type-small)", display: "grid", gap: 4 }}>
        {offer.reasons.map((r) => (
          <li key={`${r.product_id}-${r.batch_id}`}>
            <strong>{r.product_name}</strong>: не спишем {formatQty(r.at_risk)} — <span style={{ font: "var(--type-id)" }}>партия №{r.batch_id}</span>, {formatUntil(r.expires_at)}
          </li>
        ))}
      </ul>
      <p style={{ margin: 0, font: "var(--type-small)", color: "var(--text-muted)" }}>{offer.explanation}</p>
      {/* Кнопки менеджера всегда внизу карточки: в ряду карточек они на одной линии. */}
      <div style={{ marginTop: "auto", paddingTop: 4 }}>
        <OfferActions offer={offer} onChanged={onChanged} />
      </div>
    </article>
  );
}

export function HotOffersPage() {
  const [algo, setAlgo] = useState<HotAlgo | null>(null);
  const [state, reload] = useHotOffers(algo);
  const productId = useProductFilter();
  const shown = state.kind === "ready" ? state.data.algo : algo;

  return (
    <section>
      <ScreenTitle>Горячие предложения</ScreenTitle>
      <div role="group" aria-label="Алгоритм (для демо)" style={{ display: "flex", gap: 8, marginBottom: 16 }}>
        {(["a", "b"] as const).map((k) => (
          <SecondaryButton key={k} aria-pressed={shown === k} onClick={() => setAlgo(k)} styles={shown === k ? { root: { background: "var(--brand-tint)" } } : undefined}>
            Алгоритм {k.toUpperCase()}
          </SecondaryButton>
        ))}
      </div>
      {productId && (
        <p style={{ font: "var(--type-body)" }}>
          Блюда, которые спасают выбранный продукт ({state.kind === "ready" ? productName(state.data.offers, productId) : productId}).{" "}
          <a href="#/hot-offers" style={{ font: "var(--type-button)", color: "var(--brand-ink)", textDecoration: "none" }}>Показать все</a>
        </p>
      )}
      {state.kind === "loading" && <LoadingState rows={3} label="Загрузка предложений" />}
      {state.kind === "error" && <ErrorState what="горячие предложения" reason={state.message} onRetry={() => void reload()} />}
      {state.kind === "ready" && <Body state={state} productId={productId} reload={reload} />}
    </section>
  );
}

function productName(offers: HotOffer[], id: string): string {
  for (const o of offers) for (const r of o.reasons) if (r.product_id === id) return r.product_name;
  return id;
}

function Body({ state, productId, reload }: { state: Extract<ReturnType<typeof useHotOffers>[0], { kind: "ready" }>; productId: string | null; reload: () => Promise<void> }) {
  const prices = useUnitPrices();
  const offers = filterByProduct(state.data.offers, productId);
  const excluded = productId ? state.data.excluded.filter((e) => e.product_id === productId) : state.data.excluded;
  return (
    <>
      {offers.length === 0 && (
        <EmptyState
          title={productId ? "Нет подходящих блюд" : (state.data.manager_picks ?? []).length ? "Расчёт ничего не предлагает" : "Продвигать нечего"}
          text={productId ? "Нет блюд, которые спасают этот продукт." : "Сейчас продвигать нечего: продукты не под угрозой."}
        />
      )}
      <div style={{ display: "grid", gap: 16, gridTemplateColumns: "repeat(auto-fill, minmax(280px, 1fr))" }}>
        {offers.map((o, i) => (
          <OfferCard key={o.dish_id} offer={o} note={i === 0 ? handNote(o) : undefined} prices={prices} onChanged={reload} />
        ))}
      </div>
      {!productId && <ManagerPanel data={state.data} menu={state.menu} onChanged={reload} />}
      {excluded.length > 0 && (
        <div style={{ marginTop: 24 }}>
          <Card title="Исключены из предложений">
            <ul aria-label="Исключённые блюда" style={{ margin: 0, paddingLeft: 18, font: "var(--type-body)" }}>
              {excluded.map((e) => (
                <li key={`${e.dish_id}-${e.product_id}`}>
                  <strong>{state.dishNames[e.dish_id] ?? e.dish_id}</strong> — {e.why}
                </li>
              ))}
            </ul>
          </Card>
        </div>
      )}
    </>
  );
}
