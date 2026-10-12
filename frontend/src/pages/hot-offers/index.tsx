import { useEffect, useState } from "react";
import type { HotAlgo, HotOffer } from "../../api/types";
import { Card, ErrorState, LoadingState, EmptyState, ScreenTitle, SecondaryButton } from "../../components";
import { formatQty } from "../stock/format";
import { productIdFromHash } from "./query";
import { RealisticBlock } from "./RealisticBlock";
import { realisticTarget } from "./realistic";
import { useHotOffers } from "./useHotOffers";
import { useUnitPrices } from "./useUnitPrices";
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

function OfferCard({ offer, note, prices }: { offer: HotOffer; note?: string; prices: UnitPrices }) {
  const hasTarget = offer.target_portions > 0;
  const capped = realisticTarget(offer) !== null;
  return (
    <article
      data-testid="hot-offer"
      style={{ display: "grid", gap: 8, background: "var(--surface)", border: "1px solid var(--border)", borderRadius: 10, boxShadow: "var(--shadow-sm)", padding: "var(--card-pad)", minWidth: 0 }}
    >
      <h2 style={{ margin: 0, font: "var(--type-h3)" }}>{offer.dish_name}</h2>
      {hasTarget && !capped && (
        <p style={{ margin: 0, display: "flex", alignItems: "baseline", flexWrap: "wrap", gap: "2px 12px" }}>
          <span style={{ font: "var(--type-kpi)", fontSize: 32, lineHeight: 1.1, color: "var(--primary)" }}>
            {offer.target_today > 0 ? offer.target_today : offer.target_portions}{" "}
            <span style={{ font: "var(--type-label)", color: "var(--text)" }}>
              {portionsWord(offer.target_today > 0 ? offer.target_today : offer.target_portions)}
              {offer.target_today > 0 ? " сегодня" : " в ближайшую смену"}
            </span>
          </span>
          {note && <span style={{ font: "var(--type-label)", color: "var(--danger, var(--text))" }}>{note}</span>}
          <span style={{ font: "var(--type-small)", color: "var(--text-muted)" }}>
            {offer.target_today > 0 && <>всего {offer.target_portions} </>}<span title={new Date(offer.sell_until).toLocaleString("ru-RU")}>{formatUntil(offer.sell_until)}</span> · обычно {offer.usual_portions}
          </span>
        </p>
      )}
      {capped && <RealisticBlock offer={offer} prices={prices} />}
      {!hasTarget && (
        <p style={{ margin: 0, font: "var(--type-small)", color: "var(--text-muted)", display: "flex", gap: 12, flexWrap: "wrap" }}>
          <span title={new Date(offer.sell_until).toLocaleString("ru-RU")}>{formatUntil(offer.sell_until)}</span>
          {note && <span>{note}</span>}
        </p>
      )}
      <ul style={{ margin: 0, paddingLeft: 18, font: "var(--type-body)" }}>
        {offer.reasons.map((r) => (
          <li key={`${r.product_id}-${r.batch_id}`}>
            <strong>{r.product_name}</strong>: спасёт {formatQty(r.at_risk)} — <span style={{ font: "var(--type-num)" }}>партия №{r.batch_id}</span>, {formatUntil(r.expires_at)}
          </li>
        ))}
      </ul>
      <p style={{ margin: 0, font: "var(--type-small)", color: "var(--text-muted)" }}>{offer.explanation}</p>
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
      {state.kind === "ready" && <Body state={state} productId={productId} />}
    </section>
  );
}

function productName(offers: HotOffer[], id: string): string {
  for (const o of offers) for (const r of o.reasons) if (r.product_id === id) return r.product_name;
  return id;
}

function Body({ state, productId }: { state: Extract<ReturnType<typeof useHotOffers>[0], { kind: "ready" }>; productId: string | null }) {
  const prices = useUnitPrices();
  const offers = filterByProduct(state.data.offers, productId);
  const excluded = productId ? state.data.excluded.filter((e) => e.product_id === productId) : state.data.excluded;
  return (
    <>
      {offers.length === 0 && (
        <EmptyState
          title={productId ? "Нет подходящих блюд" : "Продвигать нечего"}
          text={productId ? "Нет блюд, которые спасают этот продукт." : "Сейчас продвигать нечего: продукты не под угрозой."}
        />
      )}
      <div style={{ display: "grid", gap: 16, gridTemplateColumns: "repeat(auto-fill, minmax(280px, 1fr))" }}>
        {offers.map((o, i) => (
          <OfferCard key={o.dish_id} offer={o} note={i === 0 ? handNote(o) : undefined} prices={prices} />
        ))}
      </div>
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
