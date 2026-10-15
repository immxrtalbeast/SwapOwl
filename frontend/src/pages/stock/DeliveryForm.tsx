import { useState } from "react";
import type { FormEvent } from "react";
import { NativeSelect, TextInput } from "@mantine/core";
import { IconPlus, IconTrash } from "@tabler/icons-react";
import { api } from "../../api/client";
import type { DeliveryInput, ProductStock } from "../../api/types";
import { CsvImport } from "./CsvImport";
import { PhotoInvoice } from "./PhotoInvoice";
import { Card, PrimaryButton, SecondaryButton, TextButton } from "../../components";

interface Line {
  product_id: string;
  quantity: string;
  unit: string;
  shelf_life_hours: string;
}

const emptyLine = (): Line => ({ product_id: "", quantity: "", unit: "", shelf_life_hours: "" });
const UNITS = ["g", "kg", "ml", "l", "pcs"] as const;

type Status = { kind: "idle" | "sending" } | { kind: "ok" | "error"; message: string };

/** Приём поставки — карточка справа от таблицы склада (на узком экране — под ней). */
export function DeliveryForm({ products, onReceived }: { products: readonly ProductStock[]; onReceived: () => void | Promise<void> }) {
  const [number, setNumber] = useState("");
  const [lines, setLines] = useState<Line[]>([emptyLine()]);
  const [status, setStatus] = useState<Status>({ kind: "idle" });
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
        ...(l.unit ? { unit: l.unit as (typeof UNITS)[number] } : {}),
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

  const productOptions = [{ value: "", label: "Выберите продукт" }, ...products.map((p) => ({ value: p.id, label: p.name }))];

  return (
    <Card title="Приём поставки">
      <form onSubmit={submit} style={{ display: "grid", gap: 16 }}>
        <PhotoInvoice products={products} onReceived={onReceived} />
        <CsvImport products={products} onReceived={onReceived} />
        <TextInput label="Номер накладной" required value={number} onChange={(e) => setNumber(e.currentTarget.value)} />
        {lines.map((l, i) => (
          <fieldset key={i} style={{ display: "grid", gap: 8, margin: 0, padding: 12, border: "1px solid var(--border)", borderRadius: 6 }}>
            <legend style={{ font: "var(--type-label)", padding: "0 4px" }}>Позиция {i + 1}</legend>
            {products.length > 0 ? (
              <NativeSelect label="Продукт" required data={productOptions} value={l.product_id} onChange={(e) => update(i, { product_id: e.currentTarget.value })} />
            ) : (
              <TextInput label="Код продукта" required value={l.product_id} onChange={(e) => update(i, { product_id: e.currentTarget.value })} />
            )}
            {/* Единице — больше места: «как у продукта» должна читаться целиком. */}
            <div style={{ display: "grid", gridTemplateColumns: "minmax(0, 1fr) minmax(0, 1.25fr)", gap: 8 }}>
              <TextInput label="Количество" required inputMode="decimal" pattern="[0-9]+(\.[0-9]+)?" value={l.quantity} onChange={(e) => update(i, { quantity: e.currentTarget.value })} />
              <NativeSelect label="Единица" data={[{ value: "", label: "как у продукта" }, ...UNITS.map((u) => ({ value: u, label: u }))]} value={l.unit} onChange={(e) => update(i, { unit: e.currentTarget.value })} />
            </div>
            <TextInput label="Срок хранения, ч" required type="number" min={1} max={87600} step={1} value={l.shelf_life_hours} onChange={(e) => update(i, { shelf_life_hours: e.currentTarget.value })} />
            {lines.length > 1 && (
              <TextButton type="button" leftSection={<IconTrash size={16} aria-hidden="true" />} onClick={() => setLines((ls) => ls.filter((_, j) => j !== i))}>
                Убрать позицию
              </TextButton>
            )}
          </fieldset>
        ))}
        <SecondaryButton type="button" leftSection={<IconPlus size={16} aria-hidden="true" />} onClick={() => setLines((ls) => [...ls, emptyLine()])}>
          Добавить позицию
        </SecondaryButton>
        <PrimaryButton type="submit" loading={status.kind === "sending"}>Принять поставку</PrimaryButton>
        {status.kind === "ok" && <p role="status" style={{ margin: 0, color: "var(--urgency-ok-fg)" }}>{status.message}</p>}
        {status.kind === "error" && <p role="alert" style={{ margin: 0, color: "var(--urgency-critical-fg)" }}>Ошибка: {status.message}</p>}
      </form>
    </Card>
  );
}
