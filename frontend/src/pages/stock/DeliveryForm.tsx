import { useState } from "react";
import type { FormEvent } from "react";
import { api } from "../../api/client";
import type { DeliveryInput } from "../../api/types";

interface Line {
  product_id: string;
  quantity: string;
  unit: string;
  shelf_life_hours: string;
}

const emptyLine = (): Line => ({ product_id: "", quantity: "", unit: "", shelf_life_hours: "" });

export function DeliveryForm({ onReceived }: { onReceived: () => void | Promise<void> }) {
  const [number, setNumber] = useState("");
  const [lines, setLines] = useState<Line[]>([emptyLine()]);
  const [status, setStatus] = useState<{ kind: "idle" | "sending" } | { kind: "ok" | "error"; message: string }>({ kind: "idle" });
  // Идентификатор поставки создаётся один раз на отправку и сохраняется при повторе после ошибки сети:
  // повтор с тем же id безопасен (идемпотентность по внешнему id).
  const [deliveryId, setDeliveryId] = useState(() => crypto.randomUUID());

  const update = (i: number, patch: Partial<Line>) =>
    setLines((ls) => ls.map((l, j) => (j === i ? { ...l, ...patch } : l)));

  async function submit(e: FormEvent) {
    e.preventDefault();
    setStatus({ kind: "sending" });
    const body: DeliveryInput = {
      id: deliveryId,
      number,
      created_at: new Date().toISOString(),
      items: lines.map((l) => ({
        product_id: l.product_id.trim(),
        quantity: l.quantity.trim(),
        ...(l.unit ? { unit: l.unit as "g" | "kg" | "ml" | "l" | "pcs" } : {}),
        shelf_life_hours: Number(l.shelf_life_hours),
      })),
    };
    try {
      const r = await api.receiveDelivery(body);
      setStatus({ kind: "ok", message: `${r.replayed ? "Поставка уже была принята" : "Поставка принята"}: партий ${r.batches.length}` + (r.warnings.length ? `; предупреждений: ${r.warnings.length}` : "") });
      setLines([emptyLine()]);
      setNumber("");
      setDeliveryId(crypto.randomUUID());
      await onReceived();
    } catch (err) {
      setStatus({ kind: "error", message: err instanceof Error ? err.message : "Не удалось отправить поставку" });
    }
  }

  return (
    <form onSubmit={submit} style={{ marginTop: 24 }}>
      <h2>Приём поставки</h2>
      <label>
        Номер накладной <input required value={number} onChange={(e) => setNumber(e.target.value)} />
      </label>
      {lines.map((l, i) => (
        <fieldset key={i}>
          <legend>Позиция {i + 1}</legend>
          <label>Код продукта <input required value={l.product_id} onChange={(e) => update(i, { product_id: e.target.value })} /></label>{" "}
          <label>Количество <input required inputMode="decimal" pattern="[0-9]+(\.[0-9]+)?" value={l.quantity} onChange={(e) => update(i, { quantity: e.target.value })} /></label>{" "}
          <label>
            Единица{" "}
            <select value={l.unit} onChange={(e) => update(i, { unit: e.target.value })}>
              <option value="">как у продукта</option>
              {["g", "kg", "ml", "l", "pcs"].map((u) => <option key={u} value={u}>{u}</option>)}
            </select>
          </label>{" "}
          <label>Срок хранения, ч <input required type="number" min={1} max={87600} step={1} value={l.shelf_life_hours} onChange={(e) => update(i, { shelf_life_hours: e.target.value })} /></label>
          {lines.length > 1 && <button type="button" onClick={() => setLines((ls) => ls.filter((_, j) => j !== i))}>Убрать</button>}
        </fieldset>
      ))}
      <button type="button" onClick={() => setLines((ls) => [...ls, emptyLine()])}>Добавить позицию</button>{" "}
      <button type="submit" disabled={status.kind === "sending"}>Принять поставку</button>
      {status.kind === "sending" && <p role="status">Отправка…</p>}
      {status.kind === "ok" && <p role="status">{status.message}</p>}
      {status.kind === "error" && <p role="alert">Ошибка: {status.message}</p>}
    </form>
  );
}
