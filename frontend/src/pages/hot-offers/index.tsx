import { useEffect, useState } from "react";
import type { HotAlgo, HotOffer } from "../../api/types";
import { formatQty } from "../stock/format";
import { productIdFromHash } from "./query";
import { useHotOffers } from "./useHotOffers";
import { filterByProduct, formatUntil, portionsWord } from "./view";

function useProductFilter(): string | null {
  const [id, setId] = useState(() => productIdFromHash(window.location.hash));
  useEffect(() => {
    const on = () => setId(productIdFromHash(window.location.hash));
    window.addEventListener("hashchange", on);
    return () => window.removeEventListener("hashchange", on);
  }, []);
  return id;
}

function OfferCard({ offer }: { offer: HotOffer }) {
  const hasTarget = offer.target_portions > 0;
  return (
    <article data-testid="hot-offer" style={{ border: "1px solid #ccc", borderRadius: 8, padding: 12, background: "#fff7ed" }}>
      <h2 style={{ margin: "0 0 4px" }}>🔥 {offer.dish_name}</h2>
      <p style={{ margin: "0 0 8px", fontSize: "1.25em", fontWeight: 700 }}>
        {hasTarget && (
          <span>
            {offer.target_portions} {portionsWord(offer.target_portions)}
            <span style={{ fontWeight: 400, fontSize: "0.75em" }}> (обычно {offer.usual_portions})</span>
            {" · "}
          </span>
        )}
        <span title={new Date(offer.sell_until).toLocaleString("ru-RU")}>{formatUntil(offer.sell_until)}</span>
      </p>
      <ul style={{ margin: 0, paddingLeft: 18 }}>
        {offer.reasons.map((r) => (
          <li key={`${r.product_id}-${r.batch_id}`}>
            <strong>{r.product_name}</strong>: спасёт {formatQty(r.at_risk)} — партия №{r.batch_id}, {formatUntil(r.expires_at)}
          </li>
        ))}
      </ul>
      <p style={{ margin: "8px 0 0", fontSize: "0.85em", color: "#555" }}>{offer.explanation}</p>
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
      <h1>Горячие предложения</h1>
      <div role="group" aria-label="Алгоритм (для демо)" style={{ marginBottom: 12 }}>
        {(["a", "b"] as const).map((k) => (
          <button key={k} aria-pressed={shown === k} onClick={() => setAlgo(k)} style={{ marginRight: 8, fontWeight: shown === k ? 700 : 400 }}>
            Алгоритм {k.toUpperCase()}
          </button>
        ))}
      </div>
      {productId && (
        <p>
          Блюда, которые спасают выбранный продукт ({state.kind === "ready" ? productName(state.data.offers, productId) : productId}).{" "}
          <a href="#/hot-offers">Показать все</a>
        </p>
      )}
      {state.kind === "loading" && <p role="status">Загрузка…</p>}
      {state.kind === "error" && (
        <div role="alert">
          <p>Ошибка: {state.message}</p>
          <button onClick={() => void reload()}>Повторить</button>
        </div>
      )}
      {state.kind === "ready" && <Body state={state} productId={productId} />}
    </section>
  );
}

function productName(offers: HotOffer[], id: string): string {
  for (const o of offers) for (const r of o.reasons) if (r.product_id === id) return r.product_name;
  return id;
}

function Body({ state, productId }: { state: Extract<ReturnType<typeof useHotOffers>[0], { kind: "ready" }>; productId: string | null }) {
  const offers = filterByProduct(state.data.offers, productId);
  const excluded = productId ? state.data.excluded.filter((e) => e.product_id === productId) : state.data.excluded;
  return (
    <>
      {offers.length === 0 && (
        <p>{productId ? "Нет блюд, которые спасают этот продукт." : "Сейчас продвигать нечего: продукты не под угрозой."}</p>
      )}
      <div style={{ display: "grid", gap: 12, gridTemplateColumns: "repeat(auto-fill, minmax(280px, 1fr))" }}>
        {offers.map((o) => (
          <OfferCard key={o.dish_id} offer={o} />
        ))}
      </div>
      {excluded.length > 0 && (
        <section aria-label="Исключённые блюда" style={{ marginTop: 24 }}>
          <h2>Исключены из предложений</h2>
          <ul>
            {excluded.map((e) => (
              <li key={`${e.dish_id}-${e.product_id}`}>
                <strong>{state.dishNames[e.dish_id] ?? e.dish_id}</strong> — {e.why}
              </li>
            ))}
          </ul>
        </section>
      )}
    </>
  );
}
