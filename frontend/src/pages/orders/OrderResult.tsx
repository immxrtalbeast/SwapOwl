import { useState } from "react";
import { IconBan, IconCircleCheck } from "@tabler/icons-react";
import { api } from "../../api/client";
import type { Order } from "../../api/types";
import { PrimaryButton, SecondaryButton, TextButton } from "../../components";
import { ORDER_STATES } from "./view";
import { rejectionReason, shortfallText, writeoffLines } from "./view";
import { toneVars } from "../../lib/urgency";

type CancelState = { kind: "idle" } | { kind: "asking" } | { kind: "busy" } | { kind: "done" } | { kind: "error"; message: string };

/**
 * Отмена по ошибке (миссклик): сначала вопрос, потом POST /api/orders/{id}/cancel — проданное вернётся на склад.
 * Ключ идемпотентности живёт, пока виден этот результат: повтор после потерянного ответа — тот же запрос.
 */
function CancelOrder({ orderId, onCancelled }: { orderId: string; onCancelled?: () => void | Promise<void> }) {
  const [state, setState] = useState<CancelState>({ kind: "idle" });
  const [key] = useState(() => crypto.randomUUID());
  async function cancel() {
    setState({ kind: "busy" });
    try {
      await api.cancelOrder(orderId, key);
      setState({ kind: "done" });
      await onCancelled?.();
    } catch (e) {
      setState({ kind: "error", message: e instanceof Error ? e.message : "Не удалось отменить заказ" });
    }
  }
  if (state.kind === "done") return <span role="status" style={{ font: "var(--type-body-strong)" }}>Заказ отменён — продукты вернулись на склад.</span>;
  if (state.kind === "idle")
    return <span><TextButton size="compact-sm" onClick={() => setState({ kind: "asking" })} style={{ paddingInline: 0 }}>Отменить заказ</TextButton></span>;
  return (
    <span style={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 8, marginTop: 4 }}>
      <span style={{ font: "var(--type-body)" }}>Отменить этот заказ? Списанное вернётся на склад.</span>
      <PrimaryButton size="compact-sm" loading={state.kind === "busy"} onClick={() => void cancel()}>Да, отменить</PrimaryButton>
      <SecondaryButton size="compact-sm" disabled={state.kind === "busy"} onClick={() => setState({ kind: "idle" })}>Нет</SecondaryButton>
      {state.kind === "error" && <span role="alert" style={{ width: "100%" }}>{state.message}</span>}
    </span>
  );
}

/** Результат нового заказа: успех со списанием по партиям или отказ с причиной. Пометок маркером в сообщениях нет. */
export function OrderResult({ order, onCancelled }: { order: Order; onCancelled?: () => void | Promise<void> }) {
  const ok = order.status === "accepted";
  const v = toneVars(ORDER_STATES[order.status].tone);
  const Icon = ok ? IconCircleCheck : IconBan;
  const lines = writeoffLines(order);
  const short = shortfallText(order.shortfalls);
  return (
    <div
      role={ok ? "status" : "alert"}
      data-testid="order-result"
      style={{ display: "flex", gap: 12, padding: 16, borderRadius: 10, background: v.bg, color: v.fg, marginTop: 16 }}
    >
      <Icon size={24} aria-hidden="true" style={{ flex: "none" }} />
      <div style={{ display: "grid", gap: 4 }}>
        <strong style={{ font: "var(--type-h3)" }}>{ok ? (order.replayed ? "Заказ уже принят" : "Заказ принят") : "Заказ отклонён"}</strong>
        {ok ? (
          <>
            {lines.length > 0 ? (
              <>
                <span style={{ font: "var(--type-small)" }}>Списано по партиям:</span>
                <ul style={{ margin: 0, paddingLeft: 18 }}>
                  {lines.map((l) => (
                    <li key={l}>{l}</li>
                  ))}
                </ul>
              </>
            ) : (
              <span style={{ font: "var(--type-small)" }}>Продукты не списывались.</span>
            )}
            {short && <span style={{ font: "var(--type-small)" }}>{short}</span>}
            <CancelOrder key={order.id} orderId={order.id} onCancelled={onCancelled} />
          </>
        ) : (
          <span style={{ font: "var(--type-body)" }}>{rejectionReason(order.missing)}</span>
        )}
      </div>
    </div>
  );
}
