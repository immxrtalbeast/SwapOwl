import { useState } from "react";
import type { FormEvent } from "react";
import { api } from "../../api/client";
import type { Dish, Order } from "../../api/types";
import { formatQty } from "../stock/format";

interface Line {
  dish_id: string;
  portions: string;
}

export function OrderForm({ dishes, onPlaced }: { dishes: Dish[]; onPlaced: () => void | Promise<void> }) {
  const [lines, setLines] = useState<Line[]>([{ dish_id: "", portions: "1" }]);
  const [result, setResult] = useState<Order | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [sending, setSending] = useState(false);
  // Идентификатор заказа создаётся на каждое нажатие; при ошибке сети повтор идёт с тем же id (идемпотентность).
  const [orderId, setOrderId] = useState(() => crypto.randomUUID());

  const update = (i: number, patch: Partial<Line>) => setLines((ls) => ls.map((l, j) => (j === i ? { ...l, ...patch } : l)));
  const taken = new Set(lines.map((l) => l.dish_id));

  async function submit(e: FormEvent) {
    e.preventDefault();
    setSending(true);
    setError(null);
    try {
      const r = await api.placeOrder(
        orderId,
        lines.map((l) => ({ dish_id: l.dish_id, portions: Number(l.portions) })),
      );
      setResult(r);
      setLines([{ dish_id: "", portions: "1" }]);
      setOrderId(crypto.randomUUID());
      await onPlaced();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Не удалось отправить заказ");
    } finally {
      setSending(false);
    }
  }

  return (
    <form onSubmit={submit} style={{ marginBottom: 24 }}>
      <h2>Новый заказ</h2>
      {lines.map((l, i) => (
        <div key={i} style={{ marginBottom: 8 }}>
          <label>
            Блюдо{" "}
            <select required value={l.dish_id} onChange={(e) => update(i, { dish_id: e.target.value })}>
              <option value="">— выберите —</option>
              {dishes.map((d) => (
                <option key={d.id} value={d.id} disabled={taken.has(d.id) && l.dish_id !== d.id}>
                  {d.name}
                </option>
              ))}
            </select>
          </label>{" "}
          <label>
            Порций{" "}
            <input required type="number" min={1} max={10000} step={1} value={l.portions} onChange={(e) => update(i, { portions: e.target.value })} />
          </label>{" "}
          {lines.length > 1 && (
            <button type="button" onClick={() => setLines((ls) => ls.filter((_, j) => j !== i))}>
              Убрать
            </button>
          )}
        </div>
      ))}
      <button type="button" onClick={() => setLines((ls) => [...ls, { dish_id: "", portions: "1" }])}>
        Добавить блюдо
      </button>{" "}
      <button type="submit" disabled={sending}>
        Оформить заказ
      </button>
      {sending && <p role="status">Отправка…</p>}
      {error && <p role="alert">Ошибка: {error}</p>}
      {result?.status === "accepted" && (
        <p role="status">Заказ принят{result.replayed ? " (повтор)" : ""}: списано из партий — {result.writeoffs.length}.</p>
      )}
      {result?.status === "rejected" && (
        <div role="alert">
          <p>Заказ отклонён: не хватает продуктов.</p>
          <MissingList order={result} />
        </div>
      )}
    </form>
  );
}

export function MissingList({ order }: { order: Order }) {
  return (
    <ul>
      {order.missing.map((m) => (
        <li key={m.product_id}>
          {m.product_name}: нужно {formatQty(m.required)}, доступно {formatQty(m.available)}, не хватает {formatQty(m.missing)}
        </li>
      ))}
    </ul>
  );
}
