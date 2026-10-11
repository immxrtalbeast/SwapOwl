import { useEffect, useState } from "react";
import { api } from "../../api/client";
import type { Dish } from "../../api/types";
import { useClock } from "../../app/ClockContext";
import {
  MAX_BURST,
  demoApi,
  describeBurst,
  parseBurstOrders,
  parseJumpHours,
  parseSpeed,
  tokenActionsVisible,
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
      const r = await demoApi.simulator(next);
      setRunning(r.running);
      return r.running ? "Генератор запущен" : "Генератор на паузе";
    });

  const burstN = parseBurstOrders(orders);
  const speedV = parseSpeed(speed);
  const hoursV = parseJumpHours(hours);
  const t = token.trim();

  return (
    <section>
      <h1>Демо</h1>
      {info && (
        <p>
          Время стенда: {new Date(info.now).toLocaleString("ru-RU", { timeZone: info.timezone })}, скорость ×{info.speed}
        </p>
      )}

      <h2>Генератор заказов</h2>
      <p>Сейчас: {running === null ? "…" : running ? "идёт" : "на паузе"}</p>
      <button disabled={busy || running === true} onClick={() => void toggle(true)}>Запустить</button>{" "}
      <button disabled={busy || running === false} onClick={() => void toggle(false)}>Пауза</button>

      <h2>Залп заказов</h2>
      <p>Одновременные заказы по одной порции — проверка, что последние порции не уходят дважды.</p>
      <label>
        Блюдо{" "}
        <select value={dishId} onChange={(e) => setDishId(e.target.value)}>
          {dishes.map((d) => <option key={d.id} value={d.id}>{d.name}</option>)}
        </select>
      </label>{" "}
      <label>
        Заказов (1…{MAX_BURST}){" "}
        <input value={orders} onChange={(e) => setOrders(e.target.value)} inputMode="numeric" size={4} aria-invalid={burstN === null} />
      </label>{" "}
      <button disabled={busy || !dishId || burstN === null} onClick={() => void run(async () => describeBurst(await demoApi.burst(dishId, burstN!)))}>
        Залп
      </button>

      <h2>Управление временем</h2>
      <label>
        DEMO_TOKEN{" "}
        <input type="password" value={token} onChange={(e) => setToken(e.target.value)} autoComplete="off" />
      </label>
      {tokenActionsVisible(token) ? (
        <div>
          <p>
            <label>
              Скорость (0…3600; 0 — пауза){" "}
              <input value={speed} onChange={(e) => setSpeed(e.target.value)} size={6} aria-invalid={speedV === null} />
            </label>{" "}
            <button disabled={busy || speedV === null} onClick={() => void run(async () => (await demoApi.speed(speedV!, t), `Скорость ×${speedV}`))}>
              Задать скорость
            </button>
          </p>
          <p>
            <label>
              Прыжок, часов (0…720){" "}
              <input value={hours} onChange={(e) => setHours(e.target.value)} size={6} aria-invalid={hoursV === null} />
            </label>{" "}
            <button disabled={busy || hoursV === null} onClick={() => void run(async () => (await demoApi.jump(hoursV!, t), `Прыжок на ${hoursV} ч`))}>
              Прыгнуть
            </button>
          </p>
          <p>
            <button
              disabled={busy}
              onClick={() => {
                if (window.confirm("Сбросить стенд к начальному состоянию?")) void run(async () => (await demoApi.reset(t), "Стенд сброшен"));
              }}
            >
              Сброс стенда
            </button>
          </p>
        </div>
      ) : (
        <p>Скорость, прыжок и сброс доступны после ввода DEMO_TOKEN.</p>
      )}

      {status && <p role={status.ok ? "status" : "alert"}>{status.text}</p>}
    </section>
  );
}
