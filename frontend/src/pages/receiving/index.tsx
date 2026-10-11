import { useState } from "react";
import { api } from "../../api/client";
import type { ReceivingBatch } from "../../api/types";
import { formatQty, formatTimeLeft } from "../stock/format";
import { SHELF_DAYS, currentShelfHours, datePatch, formatShelf } from "./shelf";
import { useReceiving } from "./useReceiving";

// Крупные цели касания для телефона и ТСД (не меньше 48 px).
const tap = { minHeight: 48, minWidth: 64, padding: "8px 14px", fontSize: "1.05em", cursor: "pointer" } as const;

function BatchCard({ b, onDone }: { b: ReceivingBatch; onDone: () => Promise<void> }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const hours = currentShelfHours(b.received_at, b.expires_at);

  async function confirm(patch: Parameters<typeof api.confirmExpiry>[1]) {
    setBusy(true);
    setError(null);
    try {
      await api.confirmExpiry(b.id, patch);
      await onDone();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Не удалось подтвердить срок");
    } finally {
      setBusy(false);
    }
  }

  return (
    <li data-testid="receiving-row" style={{ border: "1px solid #ccc", borderRadius: 8, padding: 12, marginBottom: 12, listStyle: "none" }}>
      <div style={{ fontWeight: 700, fontSize: "1.15em" }}>{b.product_name}</div>
      <div>
        {formatQty(b.quantity)} · поставка {b.delivery_number || b.delivery_id}
      </div>
      <div style={{ margin: "4px 0 10px" }}>
        Срок по умолчанию: {formatShelf(hours)} ({formatTimeLeft(b.time_left_seconds)}, до {new Date(b.expires_at).toLocaleString("ru-RU")})
      </div>
      <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
        <button style={{ ...tap, fontWeight: 700 }} disabled={busy} onClick={() => void confirm({ shelf_life_hours: hours })}>
          Верно: {formatShelf(hours)}
        </button>
        {SHELF_DAYS.filter((d) => d * 24 !== hours).map((d) => (
          <button key={d} style={tap} disabled={busy} onClick={() => void confirm({ shelf_life_hours: d * 24 })}>
            {d} дн
          </button>
        ))}
        <label style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
          Годен до
          <input
            type="date"
            aria-label={`Годен до: ${b.product_name}`}
            disabled={busy}
            style={{ ...tap, padding: "4px 8px" }}
            onChange={(e) => {
              const p = datePatch(e.target.value);
              if (p) void confirm(p);
            }}
          />
        </label>
      </div>
      {error && (
        <p role="alert" style={{ color: "#b00020" }}>
          {error}
        </p>
      )}
    </li>
  );
}

export function ReceivingPage() {
  const [state, reload] = useReceiving();
  return (
    <section>
      <h1>Приёмка</h1>
      <p>Срок этих партий взят из справочника продукта: накладная его не содержала. Подтвердите или поправьте.</p>
      {state.kind === "loading" && <p role="status">Загрузка…</p>}
      {state.kind === "error" && (
        <div role="alert">
          <p>Ошибка: {state.message}</p>
          <button style={tap} onClick={() => void reload()}>
            Повторить
          </button>
        </div>
      )}
      {state.kind === "ready" && state.rows.length === 0 && <p>Все сроки за последние 48 часов подтверждены.</p>}
      {state.kind === "ready" && state.rows.length > 0 && (
        <ul style={{ margin: 0, padding: 0 }}>
          {state.rows.map((b) => (
            <BatchCard key={b.id} b={b} onDone={reload} />
          ))}
        </ul>
      )}
    </section>
  );
}
