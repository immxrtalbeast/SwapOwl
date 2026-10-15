import { useEffect, useState } from "react";
import { api } from "../../api/client";
import type { ReceivingBatch } from "../../api/types";
import { useConnection } from "../../app/ConnectionContext";
import { useStandFormat } from "../../app/useStandFormat";
import { Card, PrimaryButton, ScreenStates, ScreenTitle, SecondaryButton } from "../../components";
import { formatQty } from "../../lib/format";
import { SHELF_DAYS, currentShelfHours, datePatch, formatShelf } from "./shelf";
import { ScanPanel } from "./ScanPanel";
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
  // Время последнего успешного обновления: ready-состояние пересоздаётся при каждой удачной загрузке.
  const nowMs = f.now.getTime();
  useEffect(() => {
    if (state.kind === "ready") setLoadedAt(f.time(nowMs));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state]);
  const stale = conn === "offline" ? loadedAt : null;

  return (
    <section style={{ maxWidth: 760, margin: "0 auto" }}>
      <div style={{ display: "flex", justifyContent: "center", textAlign: "center" }}>
        <ScreenTitle stale={stale}>Приёмка</ScreenTitle>
      </div>
      <ScanPanel onSent={reload} />
      <p style={{ margin: "0 auto 24px", color: "var(--text-muted)", maxWidth: 560, textAlign: "center" }}>
        Срок годности — обязательная часть поставки: позицию без срока система не примет. Сканер читает «годен до» из
        маркировки (DataMatrix / GS1, AI 17); если в коде срока нет — перепишите его с упаковки. Без кода — «Добавить позицию».
      </p>
      {state.kind === "ready" && state.rows.length === 0 && (
        <Card>
          <div data-testid="receiving-empty" style={{ display: "grid", gap: 6, justifyItems: "center", textAlign: "center", padding: "20px 0" }}>
            <h2 style={{ margin: 0, font: "var(--type-h3)" }}>Подтверждать нечего</h2>
            <p style={{ margin: 0, color: "var(--text-muted)" }}>Все партии приняты со сроком годности. Новая поставка появится в «Складе».</p>
          </div>
        </Card>
      )}
      {state.kind === "loading" && <ScreenStates.Loading label="Загрузка приёмки" rows={3} />}
      {state.kind === "error" && <ScreenStates.Error what="приёмку" reason={state.message} onRetry={() => void reload()} />}
      {state.kind === "ready" && state.rows.length > 0 && (
        <p style={{ margin: "0 0 12px", font: "var(--type-label)" }}>
          Партии, принятые до этого правила со сроком из справочника, — подтвердите или поправьте срок:
        </p>
      )}
      {state.kind === "ready" && state.rows.length > 0 && (
        <ul style={{ margin: 0, padding: 0, display: "grid", gap: 12 }}>
          {state.rows.map((b) => (
            <BatchCard key={b.id} b={b} onDone={reload} />
          ))}
        </ul>
      )}
    </section>
  );
}
