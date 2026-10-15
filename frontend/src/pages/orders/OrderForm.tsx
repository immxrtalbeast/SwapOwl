import { useState } from "react";
import type { FormEvent } from "react";
import { IconMinus, IconPlus, IconX } from "@tabler/icons-react";
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
        {/* Строки — карточками в сетке (FS-161): новое блюдо встаёт рядом, а не под предыдущим. */}
        <div style={{ display: "grid", gap: 12, gridTemplateColumns: "repeat(auto-fill, minmax(min(100%, 300px), 1fr))" }}>
          {lines.map((l, i) => {
            const n = Number(l.portions);
            const setPortions = (v: number) => update(i, { portions: String(Math.min(10000, Math.max(1, v))) });
            return (
              <div key={i} style={{ display: "grid", gap: 10, padding: 12, border: "1px solid var(--border)", borderRadius: 10, background: "var(--surface-sunken)", position: "relative" }}>
                <label style={labelStyle}>
                  <span>Блюдо{lines.length > 1 && <span style={{ color: "var(--text-subtle)" }}> · {i + 1}</span>}</span>
                  <select required value={l.dish_id} onChange={(e) => update(i, { dish_id: e.target.value })} className="so-select" style={{ ...controlStyle, width: "100%" }}>
                    <option value="">Выберите блюдо</option>
                    {dishes.map((d) => (
                      <option key={d.id} value={d.id} disabled={(taken.has(d.id) && l.dish_id !== d.id) || (!d.available && l.dish_id !== d.id)}>
                        {dishOptionLabel(d)}
                      </option>
                    ))}
                  </select>
                </label>
                <div style={{ display: "flex", alignItems: "flex-end", justifyContent: "space-between", gap: 12 }}>
                  <label style={labelStyle}>
                    Порций
                    {/* Без стрелок браузера: − и + рядом, число можно ввести и с клавиатуры. */}
                    <span style={{ display: "inline-flex", alignItems: "stretch", border: "1px solid var(--border-strong)", borderRadius: "var(--radius-control)", overflow: "hidden", background: "var(--surface)" }}>
                      <button type="button" aria-label="Меньше на одну порцию" disabled={!(n > 1)} onClick={() => setPortions(n - 1)} style={stepStyle}><IconMinus size={16} aria-hidden="true" /></button>
                      <input
                        required inputMode="numeric" pattern="[0-9]*" aria-label="Порций"
                        value={l.portions}
                        onChange={(e) => update(i, { portions: e.target.value.replace(/\D/g, "") })}
                        style={{ width: 56, height: "var(--control-h)", border: 0, borderInline: "1px solid var(--border)", textAlign: "center", font: "var(--type-body-strong)", color: "var(--text)", background: "transparent" }}
                      />
                      <button type="button" aria-label="Больше на одну порцию" onClick={() => setPortions((n || 0) + 1)} style={stepStyle}><IconPlus size={16} aria-hidden="true" /></button>
                    </span>
                  </label>
                  {lines.length > 1 && (
                    <TextButton type="button" leftSection={<IconX size={16} aria-hidden="true" />} onClick={() => edit((ls) => ls.filter((_, j) => j !== i))}>
                      Убрать
                    </TextButton>
                  )}
                </div>
              </div>
            );
          })}
        </div>
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
      {result && <OrderResult order={result} onCancelled={onPlaced} />}
    </Card>
  );
}

const labelStyle = { display: "grid", gap: 4, font: "var(--type-label)", color: "var(--text-muted)" } as const;
const stepStyle = {
  width: 36, display: "grid", placeItems: "center", border: 0, background: "transparent", color: "var(--brand-ink)", cursor: "pointer",
} as const;
const controlStyle = {
  height: "var(--control-h)",
  padding: "0 12px",
  border: "1px solid var(--border-strong)",
  borderRadius: "var(--radius-control)",
  background: "var(--surface)",
  color: "var(--text)",
  font: "var(--type-body)",
} as const;
