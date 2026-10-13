import { IconBan, IconCircleCheck } from "@tabler/icons-react";
import type { Order } from "../../api/types";
import { ORDER_STATES } from "./view";
import { rejectionReason, shortfallText, writeoffLines } from "./view";
import { toneVars } from "../../lib/urgency";

/** Результат нового заказа: успех со списанием по партиям или отказ с причиной. Пометок маркером в сообщениях нет. */
export function OrderResult({ order }: { order: Order }) {
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
          </>
        ) : (
          <span style={{ font: "var(--type-body)" }}>{rejectionReason(order.missing)}</span>
        )}
      </div>
    </div>
  );
}
