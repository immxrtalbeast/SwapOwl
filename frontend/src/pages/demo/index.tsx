import { useEffect, useState } from "react";
import { api } from "../../api/client";
import type { Dish } from "../../api/types";
import { useClock } from "../../app/ClockContext";
import { Card, Pill, PrimaryButton, ScreenTitle, SecondaryButton } from "../../components";
import {
  MAX_BURST,
  demoApi,
  describeBurst,
  parseBurstOrders,
  parseJumpHours,
  parseSpeed,
  tokenActionsVisible,
  TOKEN_HINT,
} from "./demoApi";

const msg = (e: unknown) => (e instanceof Error ? e.message : "Не удалось выполнить действие");

export function DemoPage() {
  const { info } = useClock();
  const [running, setRunning] = useState<boolean | null>(null);
  const [dishes, setDishes] = useState<Dish[]>([]);
  const [dishId, setDishId] = useState("");
  const [orders, setOrders] = useState("10");
  const [token, setToken] = useState("");
  const [speed, setSpeed] = useState("600");
  const [hours, setHours] = useState("6");
  const [status, setStatus] = useState<{ ok: boolean; text: string } | null>(null);
  const [busy, setBusy] = useState(false);

  // Состояние генератора приходит с /api/clock и обновляется при каждом clock.changed; после своих
  // действий перечитываем его сами.
  useEffect(() => {
    if (info) setRunning(info.simulator_running);
  }, [info]);
  useEffect(() => {
    api.dishes().then((d) => {
      setDishes(d);
      setDishId((cur) => cur || d[0]?.id || "");
    }, () => undefined);
  }, []);

  const run = async (action: () => Promise<string>) => {
    setBusy(true);
    try {
      setStatus({ ok: true, text: await action() });
    } catch (e) {
      setStatus({ ok: false, text: msg(e) });
    } finally {
      setBusy(false);
    }
  };

  const toggle = (next: boolean) =>
    run(async () => {
      const r = await demoApi.simulator(next, t);
      setRunning(r.running);
      return r.running ? "Генератор запущен" : "Генератор на паузе";
    });

  const burstN = parseBurstOrders(orders);
  const speedV = parseSpeed(speed);
  const hoursV = parseJumpHours(hours);
  const t = token.trim();
  const hasToken = tokenActionsVisible(token);

  const input: React.CSSProperties = { height: "var(--control-h)", border: "1px solid var(--border-strong)", borderRadius: 6, padding: "0 8px", font: "var(--type-body)" };
  const label: React.CSSProperties = { display: "grid", gap: 4, font: "var(--type-label)" };
  const row: React.CSSProperties = { display: "flex", gap: 12, alignItems: "end", flexWrap: "wrap" };
  return (
    <section>
      <ScreenTitle>Демо</ScreenTitle>
      <div style={{ display: "grid", gap: 24, gridTemplateColumns: "repeat(auto-fit, minmax(320px, 1fr))", alignItems: "start" }}>
        <Card title="Время стенда">
          {info ? (
            <>
              {/* Время крупно, дата строкой ниже: одной строкой 40 px «15.10.2026 11:00:00» не влезала в карточку. */}
              <p className="so-num" style={{ margin: 0, font: "var(--type-clock-lg)", fontSize: "clamp(28px, 3vw, 40px)", whiteSpace: "nowrap" }}>
                {new Date(info.now).toLocaleTimeString("ru-RU", { timeZone: info.timezone })}
              </p>
              <p style={{ margin: "4px 0 0", font: "var(--type-body)", color: "var(--text-muted)" }}>
                {new Date(info.now).toLocaleDateString("ru-RU", { timeZone: info.timezone, day: "numeric", month: "long", year: "numeric", weekday: "long" })}
              </p>
              <p style={{ margin: "8px 0 0" }}><Pill>Скорость ×{info.speed}</Pill></p>
            </>
          ) : (
            <p style={{ margin: 0, color: "var(--text-muted)" }}>…</p>
          )}
        </Card>

        <Card title="Генератор заказов">
          <p style={{ margin: "0 0 12px" }}>Сейчас: {running === null ? "…" : running ? "идёт" : "на паузе"}</p>
          <div style={row}>
            <PrimaryButton disabled={busy || !hasToken || running === true} onClick={() => void toggle(true)}>Запустить</PrimaryButton>
            <SecondaryButton disabled={busy || !hasToken || running === false} onClick={() => void toggle(false)}>Пауза</SecondaryButton>
            {!hasToken && <span style={{ color: "var(--text-muted)" }}>{TOKEN_HINT}</span>}
          </div>
        </Card>

        <Card title="Залп заказов">
          <p style={{ margin: "0 0 12px", color: "var(--text-muted)" }}>Одновременные заказы по одной порции — проверка, что последние порции не уходят дважды.</p>
          <div style={row}>
            <label style={label}>
              Блюдо
              <select style={input} value={dishId} onChange={(e) => setDishId(e.target.value)}>
                {dishes.map((d) => <option key={d.id} value={d.id}>{d.name}</option>)}
              </select>
            </label>
            <label style={label}>
              Заказов (1…{MAX_BURST})
              <input style={{ ...input, width: 80 }} value={orders} onChange={(e) => setOrders(e.target.value)} inputMode="numeric" size={4} aria-invalid={burstN === null} />
            </label>
            <PrimaryButton disabled={busy || !hasToken || !dishId || burstN === null} onClick={() => void run(async () => describeBurst(await demoApi.burst(dishId, burstN!, t)))}>
              Залп
            </PrimaryButton>
            {!hasToken && <span style={{ color: "var(--text-muted)" }}>{TOKEN_HINT}</span>}
          </div>
        </Card>

        <Card title="Управление временем">
          <label style={label}>
            Токен демо
            <input style={input} type="password" value={token} onChange={(e) => setToken(e.target.value)} autoComplete="off" />
          </label>
          {tokenActionsVisible(token) ? (
            <div style={{ display: "grid", gap: 16, marginTop: 16 }}>
              <div style={row}>
                <label style={label}>
                  Скорость (0…3600; 0 — пауза)
                  <input style={{ ...input, width: 100 }} value={speed} onChange={(e) => setSpeed(e.target.value)} size={6} aria-invalid={speedV === null} />
                </label>
                <PrimaryButton disabled={busy || speedV === null} onClick={() => void run(async () => (await demoApi.speed(speedV!, t), `Скорость ×${speedV}`))}>
                  Задать скорость
                </PrimaryButton>
              </div>
              <div style={row}>
                <label style={label}>
                  Прыжок, часов (0…720)
                  <input style={{ ...input, width: 100 }} value={hours} onChange={(e) => setHours(e.target.value)} size={6} aria-invalid={hoursV === null} />
                </label>
                <PrimaryButton disabled={busy || hoursV === null} onClick={() => void run(async () => (await demoApi.jump(hoursV!, t), `Прыжок на ${hoursV} ч`))}>
                  Прыгнуть
                </PrimaryButton>
              </div>
              <div>
                <SecondaryButton
                  disabled={busy}
                  onClick={() => {
                    if (window.confirm("Сбросить стенд к начальному состоянию?")) void run(async () => (await demoApi.reset(t), "Стенд сброшен"));
                  }}
                >
                  Сброс стенда
                </SecondaryButton>
              </div>
            </div>
          ) : (
            <p style={{ margin: "12px 0 0", color: "var(--text-muted)" }}>Скорость, прыжок и сброс доступны после ввода токена демо.</p>
          )}
        </Card>
      </div>

      {status && <p role={status.ok ? "status" : "alert"} style={{ marginTop: 16, color: status.ok ? "var(--text)" : "var(--urgency-critical-fg)" }}>{status.text}</p>}
    </section>
  );
}
