import { useEffect, useState } from "react";
import "./landing.css";
import { Logo, HandNote, Pill } from "../components";
import { formatQty, formatRelative } from "../lib/format";
import { PITCH_NUMBERS, benchNumbers, loadBench, loadLive } from "./data";
import type { LiveData } from "./data";
import type { BenchResult } from "../api/types";

const DEMO_HREF = "/#/stock";

const STEPS = [
  { n: "1", title: "Вручную", text: "Приёмка и заказы на экранах приложения: кладовщик вносит поставку и срок, официант — заказ. Ничего подключать не нужно.", note: "можно начать сегодня" },
  { n: "2", title: "Файл накладной", text: "CSV накладной поставщика со сроками годности: загрузили файл — получили партии и остатки.", note: "сроки — в файле" },
  { n: "3", title: "REST API или адаптер", text: "Поставки и продажи приходят по API или через адаптер к учётной системе. iiko — пример; теневой режим: сначала только сверка, без записи в кассу.", note: "iiko — лишь пример" },
];

function LiveWindow({ live }: { live: LiveData | null }) {
  const reason = live?.offer?.reasons[0];
  return (
    <div className="ld-win">
      {live && (
        <div className="ld-note1">
          <HandNote arrow="down">партия истекает {formatRelative(live.batch.expires_at, live.now)}</HandNote>
        </div>
      )}
      <div className="ld-app" aria-label="Окно приложения SwapOwl с данными стенда">
        <div className="ld-rail"><Logo variant="inverse" size={28} /></div>
        <div className="ld-pane">
          <h2>Склад</h2>
          {live ? (
            <>
              <div className="ld-row">
                <span className="k">Истекающая партия №{live.batch.id}</span>
                <span className="v">{live.product.name} · {formatQty(live.batch.remaining)}</span>
                <span className="ld-muted">срок {formatRelative(live.batch.expires_at, live.now)}</span>
              </div>
              {live.offer ? (
                <div className="ld-offer">
                  <span className="k ld-muted">Горячее предложение</span>
                  <b>{live.offer.dish_name}</b>
                  <span className="goal">{live.offer.target_portions} порц.</span>
                  {reason && <span className="ld-muted">под риском {formatQty(reason.at_risk)} · {reason.product_name}</span>}
                </div>
              ) : (
                <p className="ld-muted">Горячих предложений сейчас нет.</p>
              )}
            </>
          ) : (
            <p className="ld-muted">Данные стенда сейчас недоступны. Откройте демо, когда стенд будет запущен.</p>
          )}
        </div>
      </div>
      {live?.offer && reason && (
        <div className="ld-note2">
          <HandNote arrow="left">{formatQty(reason.at_risk)} — в мусор, если не продать</HandNote>
        </div>
      )}
    </div>
  );
}

function Numbers() {
  const n = PITCH_NUMBERS;
  return (
    <section className="ld-nums" aria-labelledby="ld-nums-h">
      <div className="ld-wrap">
        <h2 id="ld-nums-h">Цифры</h2>
        <div className="ld-kpis">
          <div className="ld-kpi"><b>{n.wasteWithoutPct} → {n.wasteWithPct}%</b><span className="ld-muted">списания в % себестоимости: без учёта сроков → с SwapOwl</span></div>
          <div className="ld-kpi"><b>≈ {n.savedKRubPerMonth} тыс. ₽</b><span className="ld-muted">в месяц на точку в {n.checksPerDay} чеков в день</span></div>
        </div>
        <p className="ld-muted">Симулятор, {n.runs} прогонов, допущения — в документации.</p>
      </div>
    </section>
  );
}

function Bench({ bench }: { bench: BenchResult | null }) {
  const n = benchNumbers(bench);
  if (!n) return null;
  return (
    <section className="ld-bench" aria-labelledby="ld-bench-h">
      <div className="ld-wrap">
        <h2 id="ld-bench-h">Меньше списаний на тех же данных</h2>
        <div className="ld-kpis">
          <div className="ld-kpi"><b>−{Math.round(n.vsRandom)}%</b><span className="ld-muted">списаний к случайному выбору блюд</span></div>
          <div className="ld-kpi"><b>−{Math.round(n.vsNearest)}%</b><span className="ld-muted">к «продавать ближайшее по сроку»</span></div>
        </div>
        <p className="ld-muted">Результат прогона симулятора, {n.runs} прогонов; не обещание для конкретного ресторана.</p>
      </div>
    </section>
  );
}

/** Лендинг SwapOwl в языке линейки Swap. Данные берёт у стенда; без них показывает честную заглушку. */
export function Landing() {
  const [live, setLive] = useState<LiveData | null>(null);
  const [bench, setBench] = useState<BenchResult | null>(null);
  useEffect(() => {
    let alive = true;
    void loadLive().then((d) => alive && setLive(d));
    void loadBench().then((b) => alive && setBench(b));
    return () => {
      alive = false;
    };
  }, []);

  return (
    <div className="ld">
      <header className="ld-top">
        <div className="ld-wrap">
          <Logo withWord size={34} />
          <a className="ld-btn" href={DEMO_HREF}>Попробовать</a>
        </div>
      </header>
      <section className="ld-hero">
        <div className="ld-wrap">
          <div>
            <div className="ld-pills">
              <Pill>Работает самостоятельно</Pill>
              <Pill>Интеграции по API</Pill>
            </div>
            <h1>Партии, сроки и <em>стоп-лист</em> — <em>без списаний</em></h1>
            <p className="ld-lead">
              SwapOwl ведёт остатки по партиям, списывает по FEFO и подсказывает блюда, которые стоит продать, пока продукты не испортились.
            </p>
            <div className="ld-ctas">
              <a className="ld-btn big" href={DEMO_HREF}>Открыть демо</a>
              <a className="ld-link" href="#connect">Как подключить</a>
            </div>
          </div>
          <LiveWindow live={live} />
        </div>
      </section>
      <section className="ld-how" id="connect" aria-labelledby="ld-how-h">
        <div className="ld-wrap">
          <h2 id="ld-how-h">Три способа начать</h2>
          <ol className="ld-steps">
            {STEPS.map((s) => (
              <li key={s.n}>
                <span className="ld-step-n" aria-hidden="true">{s.n}</span>
                <h3>{s.title}</h3>
                <p>{s.text}</p>
                <div className="ld-step-note"><HandNote arrow={null}>{s.note}</HandNote></div>
              </li>
            ))}
          </ol>
        </div>
      </section>
      <Numbers />
      <Bench bench={bench} />
      <footer className="ld-foot">
        <div className="ld-wrap">Сделано по кейсу «Флагмана» · UMIRHack 2026</div>
      </footer>
    </div>
  );
}
