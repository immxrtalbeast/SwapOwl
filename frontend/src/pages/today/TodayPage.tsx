import { useRef, useState } from "react";
import type { ReactNode } from "react";
import { api } from "../../api/client";
import { qtyCostMinor } from "../../api/today";
import type { UnitPrices } from "../../api/today";
import type { DishServings, HotOffer, ReceivingBatch, SupplySuggestion } from "../../api/types";
import { useAuth } from "../../auth/AuthContext";
import { hasRole } from "../../auth/roles";
import { useLive } from "../../app/useLive";
import { Card, EmptyState, ErrorState, HandNote, LoadingState, PrimaryButton, ScreenTitle, UrgencyBadge } from "../../components";
import { DISH_STATES, SUPPLY_STATES } from "../../lib/urgency";
import { useHotOffers } from "../hot-offers/useHotOffers";
import { hotOffersUrl } from "../hot-offers/query";
import { portionsWord } from "../hot-offers/view";
import { realisticTarget } from "../hot-offers/realistic";
import { RealisticBlock } from "../hot-offers/RealisticBlock";
import { formatRub } from "../money/format";
import { formatQty } from "../stock/format";
import { buildAcceptBody, offerKey, outcomeFromError, outcomeFromResult } from "../supply/accept";
import { useSupply } from "../supply/useSupply";
import { useReceiving } from "../receiving/useReceiving";
import { reasonText } from "../stop-list/reason";
import { useChangedKeys } from "./useChanged";
import { useRisk } from "./useToday";

const bigNum = { margin: 0, font: "var(--type-kpi)", lineHeight: 1.1, wordBreak: "break-word" } as const;
const list = { listStyle: "none", margin: 0, padding: 0, display: "grid", gap: 8 } as const;
const row = { display: "flex", alignItems: "center", flexWrap: "wrap", gap: "4px 12px", padding: "8px 4px", borderRadius: 6 } as const;

function useStopped(): DishServings[] | "loading" | { error: string } {
  const [s, setS] = useState<DishServings[] | "loading" | { error: string }>("loading");
  useLive(async () => {
    try {
      setS((await api.menu()).filter((d) => !d.available));
    } catch (e) {
      setS((p) => (p === "loading" ? { error: e instanceof Error ? e.message : "Не удалось загрузить меню" } : p));
    }
  }, ["stoplist.changed", "stock.changed", "stand.reset"]);
  return s;
}

function Block({ title, children, testid, glass, area }: { title: string; children: ReactNode; testid: string; glass?: boolean; area: string }) {
  return (
    <div data-testid={testid} data-kpi={glass ? "" : undefined} style={{ minWidth: 0, gridArea: area }}>
      <Card title={title} glass={glass}>{children}</Card>
    </div>
  );
}

function offerSaved(o: HotOffer, prices: UnitPrices): number {
  return o.reasons.reduce((s, r) => s + (qtyCostMinor(r.product_id, r.at_risk, prices) ?? 0), 0);
}

function AcceptNow({ row: r, ids }: { row: SupplySuggestion; ids: React.MutableRefObject<Map<string, { id: string; at: string }>> }) {
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ text: string; bad: boolean } | null>(null);
  async function accept() {
    const k = offerKey(r);
    let p = ids.current.get(k);
    if (!p) ids.current.set(k, (p = { id: crypto.randomUUID(), at: new Date().toISOString() }));
    setBusy(true);
    try {
      const res = await api.receiveDelivery(buildAcceptBody(r, p.id, p.at));
      setMsg({ text: outcomeFromResult(res.replayed).message, bad: false });
    } catch (e) {
      const o = outcomeFromError(e);
      setMsg({ text: o.kind === "need-shelf-life" ? "Нужен срок хранения — примите на экране «Поставки»" : o.message, bad: true });
    } finally {
      setBusy(false);
    }
  }
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
      <PrimaryButton size="compact-md" onClick={() => void accept()} loading={busy} disabled={busy}>Принять предложенное</PrimaryButton>
      {msg && <span role={msg.bad ? "alert" : "status"} style={{ font: "var(--type-small)" }}>{msg.text}</span>}
    </span>
  );
}

function Pending({ what, state, reload, children }: { what: string; state: { kind: string; message?: string }; reload: () => void; children: ReactNode }) {
  if (state.kind === "loading") return <LoadingState rows={2} label={`Загрузка: ${what}`} />;
  if (state.kind === "error") return <ErrorState what={what} reason={state.message} onRetry={reload} />;
  return <>{children}</>;
}

/** Экран «Сегодня»: что продать, заказать, проверить и сколько рублей под риском. Всё считает бэкенд, здесь — сборка. */
export function TodayPage() {
  const [risk, reloadRisk] = useRisk();
  const [hot, reloadHot] = useHotOffers(null);
  const [supply, reloadSupply] = useSupply();
  const [receiving, reloadReceiving] = useReceiving();
  const stopped = useStopped();
  const { state: auth } = useAuth();
  const canAccept = auth.kind === "user" && hasRole(auth.role, "operator");
  const ids = useRef(new Map<string, { id: string; at: string }>());

  const prices = risk.kind === "ready" ? risk.prices : {};
  const offers = hot.kind === "ready" ? hot.data.offers.slice(0, 3) : [];
  const urgent = supply.kind === "ready" ? supply.rows.filter((r) => r.urgency === "now") : [];
  // id поставки живёт, пока строка видна (как на «Поставках»): вернувшееся предложение — новая поставка.
  const live = new Set(urgent.map(offerKey));
  if (supply.kind === "ready") for (const k of ids.current.keys()) if (!live.has(k)) ids.current.delete(k);
  const unconfirmed = receiving.kind === "ready" ? receiving.rows : [];
  const stops = Array.isArray(stopped) ? stopped : [];

  const changed = useChangedKeys({
    risk: risk.kind === "ready" ? String(risk.atRisk.totalMinor) : "",
    today: risk.kind === "ready" ? String(risk.today) : "",
    week: risk.kind === "ready" ? String(risk.week) : "",
    ...Object.fromEntries(offers.map((o) => [`h${o.dish_id}`, `${o.target_today}|${o.target_portions}`])),
    ...Object.fromEntries(urgent.map((r) => [`s${r.product_id}`, `${r.quantity.qty}`])),
    ...Object.fromEntries(unconfirmed.map((b) => [`r${b.id}`, `${b.remaining.qty}`])),
    ...Object.fromEntries(stops.map((d) => [`d${d.id}`, reasonText(d)])),
  });
  const cls = (k: string) => (changed.has(k) ? "so-row-changed" : undefined);

  return (
    <section>
      <ScreenTitle>Сегодня</ScreenTitle>
      {/* Высоту карточки задаёт содержимое (FS-161): соседи не растягивают друг друга. */}
      <div className="so-today-grid">
        <Block title="Под риском сегодня, ₽" testid="today-risk" glass area="risk">
          <Pending what="суммы под риском" state={risk} reload={() => void reloadRisk()}>
            {risk.kind === "ready" && (
              <div className={cls("risk")} style={{ display: "grid", gap: 8 }}>
                <p data-testid="today-risk-total" style={{ ...bigNum, color: risk.atRisk.totalMinor > 0 ? "var(--urgency-risk-fg)" : "var(--text)" }}>
                  {formatRub(risk.atRisk.totalMinor)}
                </p>
                <HandNote>это уйдёт в мусор, если не продать</HandNote>
                <p style={{ margin: 0, font: "var(--type-small)", color: "var(--text-muted)" }}>
                  Оценка по цене из прогноза списаний.
                  {risk.atRisk.unpriced > 0 && ` Партий без цены (не вошли в сумму): ${risk.atRisk.unpriced}.`}
                </p>
              </div>
            )}
          </Pending>
        </Block>

        <Block title="Списано" testid="today-writeoffs" glass area="writeoffs">
          <Pending what="списания" state={risk} reload={() => void reloadRisk()}>
            {risk.kind === "ready" && (
              <div style={{ display: "grid", gap: 12 }}>
                <div className={cls("today")}>
                  <div style={{ font: "var(--type-caption)", textTransform: "uppercase", color: "var(--text-muted)" }}>Списано за сегодня, ₽</div>
                  <p data-testid="today-writeoff-day" style={bigNum}>{formatRub(risk.today)}</p>
                </div>
                <div className={cls("week")}>
                  <div style={{ font: "var(--type-caption)", textTransform: "uppercase", color: "var(--text-muted)" }}>За 7 дней, ₽</div>
                  <p data-testid="today-writeoff-week" style={bigNum}>{formatRub(risk.week)}</p>
                </div>
              </div>
            )}
          </Pending>
        </Block>

        <Block title="Продать" testid="today-sell" area="sell">
          <Pending what="горячие предложения" state={hot} reload={() => void reloadHot()}>
            {offers.length === 0 ? (
              <EmptyState title="Продвигать нечего" text="Партий под риском нет." />
            ) : (
              // Каждое блюдо — отдельный блок через линию (FS-161): название и «спасёт ₽» в одной строке,
              // цель — плашкой с числом, остаток и что с ним делать — таблицей ниже.
              <ul style={{ listStyle: "none", margin: 0, padding: 0 }}>
                {offers.map((o, i) => {
                  const real = realisticTarget(o);
                  const portions = real ? real.real : o.target_today > 0 ? o.target_today : o.target_portions;
                  // Урезанная цель спасает только проданную часть: остаток считает RealisticBlock рядом.
                  const saved = real ? Math.round((offerSaved(o, prices) * real.real) / real.target) : offerSaved(o, prices);
                  return (
                    <li
                      key={o.dish_id}
                      className={cls(`h${o.dish_id}`)}
                      style={{ display: "grid", gap: 8, padding: "12px 0", borderTop: i > 0 ? "1px solid var(--border-cell)" : undefined }}
                    >
                      <div style={{ display: "flex", alignItems: "baseline", justifyContent: "space-between", gap: 12, flexWrap: "wrap" }}>
                        <a href={hotOffersUrl(o.reasons[0]?.product_id ?? "")} style={{ font: "var(--type-h3)", fontSize: 16, color: "var(--brand-ink)", textDecoration: "none" }}>{o.dish_name}</a>
                        {saved > 0 && (
                          <span className="so-num" style={{ padding: "2px 10px", borderRadius: "var(--radius-pill)", background: "var(--brand-tint)", color: "var(--brand-ink)", font: "600 13px/20px var(--font-body)", whiteSpace: "nowrap" }}>
                            спасёт {formatRub(saved)}
                          </span>
                        )}
                      </div>
                      <div style={{ display: "flex", alignItems: "baseline", gap: 8, flexWrap: "wrap" }}>
                        <span style={{ font: "var(--type-small)", color: "var(--text-muted)" }}>продать{o.target_today > 0 || real ? " сегодня" : ""}</span>
                        <span className="so-num" style={{ font: "700 22px/26px var(--font-display)", color: "var(--primary)" }}>{portions}</span>
                        <span style={{ font: "var(--type-label)" }}>{portionsWord(portions)}</span>
                        {real && <span style={{ font: "var(--type-small)", color: "var(--text-subtle)" }}>· обычно {o.usual_portions} в день</span>}
                      </div>
                      {real && <RealisticBlock offer={o} prices={prices} compact />}
                    </li>
                  );
                })}
              </ul>
            )}
          </Pending>
        </Block>

        <Block title="Заказать" testid="today-order" area="order">
          <Pending what="предложение поставок" state={supply} reload={() => void reloadSupply()}>
            {urgent.length === 0 ? (
              <EmptyState title="Срочного нет" text="Заказывать «сейчас» нечего." />
            ) : (
              <ul style={list}>
                {urgent.map((r) => (
                  <li key={r.product_id} className={cls(`s${r.product_id}`)} style={row}>
                    <UrgencyBadge def={SUPPLY_STATES[r.urgency]} />
                    <span style={{ font: "var(--type-body-strong)" }}>{r.product_name}</span>
                    <span className="so-num">{formatQty(r.quantity)}</span>
                    {canAccept && <AcceptNow row={r} ids={ids} />}
                  </li>
                ))}
              </ul>
            )}
          </Pending>
        </Block>

        <Block title="Проверить" testid="today-check" area="check">
          <Pending what="партии на проверку" state={receiving} reload={() => void reloadReceiving()}>
            {/* Срок теперь обязателен в поставке (FS-160); здесь — только партии со сроком из справочника, принятые раньше. */}
            {unconfirmed.length > 0 && <h3 style={{ margin: "0 0 8px", font: "var(--type-label)" }}>Срок не подтверждён · {unconfirmed.length}</h3>}
            {unconfirmed.length > 0 && (
              <ul style={{ ...list, marginBottom: 12 }}>
                {unconfirmed.slice(0, 5).map((b: ReceivingBatch) => (
                  <li key={b.id} className={cls(`r${b.id}`)} style={row}>
                    <a href="#/receiving" style={{ color: "var(--brand-ink)", font: "var(--type-body-strong)" }}>{b.product_name}</a>
                    <span className="so-num">{formatQty(b.remaining)} · №{b.id}</span>
                  </li>
                ))}
                {unconfirmed.length > 5 && <li><a href="#/receiving" style={{ color: "var(--brand-ink)" }}>Все {unconfirmed.length}</a></li>}
              </ul>
            )}
          </Pending>
          {stopped === "loading" && <LoadingState rows={1} label="Загрузка стоп-листа" />}
          {typeof stopped === "object" && !Array.isArray(stopped) && <ErrorState what="стоп-лист" reason={stopped.error} />}
          {Array.isArray(stopped) && (
            <>
              <h3 style={{ margin: "0 0 8px", font: "var(--type-label)" }}>В стоп-листе · {stops.length}</h3>
              {stops.length === 0 ? (
                <p style={{ margin: 0, color: "var(--text-muted)" }}>Стоп-лист пуст.</p>
              ) : (
                <ul style={list}>
                  {stops.map((d) => (
                    <li key={d.id} className={cls(`d${d.id}`)} style={row}>
                      <UrgencyBadge def={DISH_STATES.stopped} />
                      <span style={{ font: "var(--type-body-strong)" }}>{d.name}</span>
                      <span style={{ font: "var(--type-small)", color: "var(--text-muted)" }}>{reasonText(d)}</span>
                    </li>
                  ))}
                </ul>
              )}
            </>
          )}
        </Block>
      </div>
      <p style={{ margin: "16px 0 0" }}><a href="#/shift-report" style={{ color: "var(--brand-ink)", font: "var(--type-body-strong)" }}>Отчёт смены на печать →</a></p>
    </section>
  );
}
