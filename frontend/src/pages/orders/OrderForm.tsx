import { useState } from "react";
import type { FormEvent } from "react";
import { api } from "../../api/client";
import { ApiError } from "../../api/types";
import type { DishServings, Order } from "../../api/types";
import { Card } from "../../components";
import { PrimaryButton, SecondaryButton, TextButton } from "../../components";
import { OrderResult } from "./OrderResult";
import { dishOptionLabel, stoppedLines } from "./view";

interface Line {
  dish_id: string;
  portions: string;
}

export function OrderForm({ dishes, onPlaced }: { dishes: DishServings[]; onPlaced: () => void | Promise<void> }) {
  const [lines, setLines] = useState<Line[]>([{ dish_id: "", portions: "1" }]);
  const [result, setResult] = useState<Order | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [sending, setSending] = useState(false);
  // Идентификатор заказа создаётся на каждое нажатие; при ошибке сети повтор идёт с тем же id (идемпотентность).
  const [orderId, setOrderId] = useState(() => crypto.randomUUID());

  // Любое изменение состава заказа — новый заказ: тот же id допустим только для повтора того же тела.
  const edit = (fn: (ls: Line[]) => Line[]) => {
    setLines(fn);
    setOrderId(crypto.randomUUID());
  };
  const update = (i: number, patch: Partial<Line>) => edit((ls) => ls.map((l, j) => (j === i ? { ...l, ...patch } : l)));
  const taken = new Set(lines.map((l) => l.dish_id));
  // Блюдо ушло в стоп-лист, пока собирали заказ: просим убрать его, а не отправляем заказ на заведомый отказ.
  const stopped = stoppedLines(lines, dishes);

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
      if (err instanceof ApiError && err.code === "order_conflict") setOrderId(crypto.randomUUID());
      setError(err instanceof Error ? err.message : "Не удалось отправить заказ");
    } finally {
      setSending(false);
    }
  }

  return (
    <Card title="Новый заказ">
      <form onSubmit={submit} style={{ display: "grid", gap: 12 }}>
        {lines.map((l, i) => (
          <div key={i} style={{ display: "flex", flexWrap: "wrap", alignItems: "flex-end", gap: 12 }}>
            <label style={labelStyle}>
              Блюдо
              <select required value={l.dish_id} onChange={(e) => update(i, { dish_id: e.target.value })} style={{ ...controlStyle, minWidth: 220 }}>
                <option value="">— выберите —</option>
                {dishes.map((d) => (
                  <option key={d.id} value={d.id} disabled={(taken.has(d.id) && l.dish_id !== d.id) || (!d.available && l.dish_id !== d.id)}>
                    {dishOptionLabel(d)}
                  </option>
                ))}
              </select>
            </label>
            <label style={labelStyle}>
              Порций
              <input required type="number" min={1} max={10000} step={1} value={l.portions} onChange={(e) => update(i, { portions: e.target.value })} style={{ ...controlStyle, width: 96 }} />
            </label>
            {lines.length > 1 && (
              <TextButton type="button" onClick={() => edit((ls) => ls.filter((_, j) => j !== i))}>
                Убрать
              </TextButton>
            )}
          </div>
        ))}
        <div style={{ display: "flex", flexWrap: "wrap", gap: 12 }}>
          <SecondaryButton type="button" onClick={() => edit((ls) => [...ls, { dish_id: "", portions: "1" }])}>
            Добавить блюдо
          </SecondaryButton>
          <PrimaryButton type="submit" loading={sending} disabled={stopped.length > 0}>
            Оформить заказ
          </PrimaryButton>
        </div>
        {stopped.length > 0 && (
          <p role="alert" style={{ margin: 0, color: "var(--urgency-critical-fg)" }}>
            {stopped.join(", ")} {stopped.length > 1 ? "ушли" : "ушло"} в стоп-лист — уберите из заказа, чтобы оформить остальное.
          </p>
        )}
        {error && (
          <p role="alert" style={{ margin: 0, color: "var(--urgency-critical-fg)" }}>
            Ошибка: {error}
          </p>
        )}
      </form>
      {result && <OrderResult order={result} />}
    </Card>
  );
}

const labelStyle = { display: "grid", gap: 4, font: "var(--type-label)", color: "var(--text-muted)" } as const;
const controlStyle = {
  height: "var(--control-h)",
  padding: "0 12px",
  border: "1px solid var(--border-strong)",
  borderRadius: "var(--radius-control)",
  background: "var(--surface)",
  color: "var(--text)",
  font: "var(--type-body)",
} as const;
