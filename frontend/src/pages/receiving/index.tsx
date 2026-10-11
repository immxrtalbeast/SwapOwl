import { useState } from "react";
import { api } from "../../api/client";
import type { ReceivingBatch } from "../../api/types";
import { useConnection } from "../../app/ConnectionContext";
import { useStandFormat } from "../../app/useStandFormat";
import { Card, PrimaryButton, ScreenStates, ScreenTitle, SecondaryButton } from "../../components";
import { formatQty } from "../../lib/format";
import { SHELF_DAYS, currentShelfHours, datePatch, formatShelf } from "./shelf";
import { useReceiving } from "./useReceiving";

// Крупные цели касания для телефона и ТСД: не меньше 44 px (берём 48).
const TAP = { height: 48, minWidth: 64 } as const;

function BatchCard({ b, onDone }: { b: ReceivingBatch; onDone: () => Promise<void> }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const f = useStandFormat();
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
    <li data-testid="receiving-row" style={{ listStyle: "none" }}>
      <Card>
        <div style={{ font: "var(--type-h2)" }}>{b.product_name}</div>
        <div style={{ color: "var(--text-muted)", margin: "4px 0" }}>
          <span className="so-num">{formatQty(b.quantity)}</span> · поставка <span className="so-num">{b.delivery_number || b.delivery_id}</span> ·{" "}
          <span className="so-num">партия №{b.id}</span>
        </div>
        <div style={{ margin: "4px 0 16px" }}>
          Срок по умолчанию: <strong>{formatShelf(hours)}</strong> —{" "}
          <span title={f.exact(b.expires_at)}>{f.relative(b.expires_at, { expired: true })}, до {f.dateTime(b.expires_at)}</span>
        </div>
        <div style={{ display: "flex", flexWrap: "wrap", gap: 8, alignItems: "center" }}>
          <PrimaryButton style={TAP} disabled={busy} onClick={() => void confirm({ shelf_life_hours: hours })}>
            Верно: {formatShelf(hours)}
          </PrimaryButton>
          {SHELF_DAYS.filter((d) => d * 24 !== hours).map((d) => (
            <SecondaryButton key={d} style={TAP} disabled={busy} onClick={() => void confirm({ shelf_life_hours: d * 24 })}>
              {d} дн
            </SecondaryButton>
          ))}
          <label style={{ display: "inline-flex", alignItems: "center", gap: 8, font: "var(--type-label)" }}>
            Годен до
            <input
              type="date"
              aria-label={`Годен до: ${b.product_name}`}
              disabled={busy}
              style={{ ...TAP, boxSizing: "border-box", padding: "0 12px", font: "var(--type-body)", border: "2px solid var(--border-strong)", borderRadius: "var(--radius-control)", background: "var(--surface)", color: "var(--text)" }}
              onChange={(e) => {
                const p = datePatch(e.target.value);
                if (p) void confirm(p);
              }}
            />
          </label>
        </div>
        {error && (
          <p role="alert" style={{ color: "var(--urgency-critical-fg)", margin: "12px 0 0" }}>
            {error}
          </p>
        )}
      </Card>
    </li>
  );
}

export function ReceivingPage() {
  const [state, reload] = useReceiving();
  const conn = useConnection();
  const f = useStandFormat();
  const [loadedAt, setLoadedAt] = useState<string | null>(null);
  if (state.kind === "ready" && loadedAt === null) setLoadedAt(f.time(f.now));
  const stale = conn === "offline" ? loadedAt : null;

  return (
    <section>
      <ScreenTitle stale={stale}>Приёмка</ScreenTitle>
      <p style={{ margin: "0 0 16px", color: "var(--text-muted)" }}>
        Срок этих партий взят из справочника продукта: накладная его не содержала. Подтвердите или поправьте.
      </p>
      {state.kind === "loading" && <ScreenStates.Loading label="Загрузка приёмки" rows={3} />}
      {state.kind === "error" && <ScreenStates.Error what="приёмку" reason={state.message} onRetry={() => void reload()} />}
      {state.kind === "ready" && state.rows.length === 0 && (
        <Card>
          <ScreenStates.Empty title="Все сроки подтверждены" text="За последние 48 часов партий без подтверждённого срока нет." />
        </Card>
      )}
      {state.kind === "ready" && state.rows.length > 0 && (
        <ul style={{ margin: 0, padding: 0, display: "grid", gap: 12, maxWidth: 760 }}>
          {state.rows.map((b) => (
            <BatchCard key={b.id} b={b} onDone={reload} />
          ))}
        </ul>
      )}
    </section>
  );
}
