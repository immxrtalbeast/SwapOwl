import { useEffect, useRef, useState } from "react";
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
    <Dialog label={`Вскрытие партии №${batchId}`} onClose={() => !busy && setOpId(null)}>
      <div style={{ display: "grid", gap: 16 }}>
        <span style={{ font: "var(--type-body)" }}>{openConfirmText(productName, hours, nowMs, batchExpiresAt, timeZone)}</span>
        <span style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
          <PrimaryButton disabled={busy} onClick={() => void confirm()}>Подтвердить</PrimaryButton>
          <SecondaryButton disabled={busy} onClick={() => setOpId(null)}>Отмена</SecondaryButton>
        </span>
        {error && <span role="alert" style={{ color: "var(--urgency-critical-fg)", font: "var(--type-body)" }}>{error}</span>}
      </div>
    </Dialog>
  );
}

/** Модальное окно поверх страницы: текст целиком, Esc закрывает, фокус уходит в окно; таблица под ним не сдвигается. */
function Dialog({ label, onClose, children }: { label: string; onClose: () => void; children: React.ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null;
    ref.current?.querySelector<HTMLElement>("button")?.focus();
    return () => prev?.focus?.();
  }, []);
  return (
    <div className="so-modal-backdrop" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <div
        ref={ref}
        className="so-modal"
        role="dialog"
        aria-modal="true"
        aria-label={label}
        onKeyDown={(e) => {
          if (e.key === "Escape") onClose();
          if (e.key === "Tab") {
            const items = Array.from(ref.current?.querySelectorAll<HTMLElement>("button:not([disabled])") ?? []);
            if (items.length === 0) return;
            const first = items[0]!;
            const last = items[items.length - 1]!;
            if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
            else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
          }
        }}
      >
        {children}
      </div>
    </div>
  );
}
