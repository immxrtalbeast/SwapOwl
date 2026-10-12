import { useState } from "react";
import { api } from "../../api/client";
import { ApiError } from "../../api/types";
import { PrimaryButton, SecondaryButton } from "../../components";
import { openConfirmText } from "./opened";

interface Props {
  batchId: number;
  productName: string;
  /** shelf_after_open_hours продукта. */
  hours: number;
  batchExpiresAt: string;
  nowMs: number;
  timeZone: string;
  onOpened: () => void | Promise<void>;
}

/**
 * «Вскрыл упаковку» в строке партии: подтверждение с новым сроком, затем POST /api/batches/{id}/open.
 * Идентификатор операции создаётся при показе подтверждения и сохраняется при повторе после ошибки сети:
 * повтор с тем же id безопасен.
 */
export function BatchOpen({ batchId, productName, hours, batchExpiresAt, nowMs, timeZone, onOpened }: Props) {
  const [opId, setOpId] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (opId === null) {
    return (
      <SecondaryButton size="xs" style={{ height: 32 }} onClick={() => { setError(null); setOpId(crypto.randomUUID()); }}>
        Вскрыл упаковку
      </SecondaryButton>
    );
  }
  const id = opId;

  async function confirm() {
    setBusy(true);
    setError(null);
    try {
      await api.openBatch(batchId, id);
      setOpId(null);
      await onOpened();
    } catch (e) {
      setError(e instanceof ApiError || e instanceof Error ? e.message : "Не удалось вскрыть упаковку");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div role="group" aria-label={`Вскрытие партии №${batchId}`} style={{ display: "grid", gap: 6, maxWidth: 280 }}>
      <span style={{ font: "var(--type-body)" }}>{openConfirmText(productName, hours, nowMs, batchExpiresAt, timeZone)}</span>
      <span style={{ display: "flex", gap: 8 }}>
        <PrimaryButton size="xs" style={{ height: 32 }} disabled={busy} onClick={() => void confirm()}>Подтвердить</PrimaryButton>
        <SecondaryButton size="xs" style={{ height: 32 }} disabled={busy} onClick={() => setOpId(null)}>Отмена</SecondaryButton>
      </span>
      {error && <span role="alert" style={{ color: "var(--urgency-critical-fg)", font: "var(--type-body)" }}>{error}</span>}
    </div>
  );
}
