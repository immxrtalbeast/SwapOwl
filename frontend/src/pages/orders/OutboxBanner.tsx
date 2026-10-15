import { IconCloudOff, IconCloudCheck } from "@tabler/icons-react";
import type { DishServings } from "../../api/types";
import { SecondaryButton, TextButton } from "../../components";
import type { OutboxApi } from "./useOutbox";

/** «Ждут отправки»: заказы, нажатые без связи, и итог отправки. Пусто — ничего не рисуется. */
export function OutboxBanner({ outbox, dishes }: { outbox: OutboxApi; dishes: readonly DishServings[] }) {
  const { queue, lastSent, flushing } = outbox;
  if (queue.length === 0 && lastSent.length === 0) return null;
  const name = (id: string) => dishes.find((d) => d.id === id)?.name ?? id;
  const accepted = lastSent.filter((o) => o.kind === "sent" && o.accepted).length;
  const rejected = lastSent.filter((o) => o.kind === "sent" && !o.accepted).length;
  const dropped = lastSent.filter((o) => o.kind === "dropped").length;
  return (
    <div data-testid="outbox" style={{ display: "grid", gap: 8, marginBottom: 12 }}>
      {queue.length > 0 && (
        <div role="status" style={{ display: "grid", gap: 6, padding: 12, borderRadius: 10, border: "1px solid var(--urgency-warn-fg)", background: "var(--urgency-warn-bg)", color: "var(--urgency-warn-fg)" }}>
          <strong style={{ display: "flex", alignItems: "center", gap: 8 }}>
            <IconCloudOff size={18} aria-hidden="true" /> Нет связи — {queue.length === 1 ? "заказ ждёт" : `заказов в очереди: ${queue.length}`} отправки
          </strong>
          <ul style={{ margin: 0, paddingLeft: 18 }}>
            {queue.map((q) => (
              <li key={q.id}>{q.items.map((i) => `${name(i.dish_id)} × ${i.portions}`).join(", ")}</li>
            ))}
          </ul>
          <span style={{ font: "var(--type-small)" }}>Отправим сами, как только связь появится. Повторное нажатие не создаст второй заказ.</span>
          <span><SecondaryButton size="compact-sm" loading={flushing} onClick={() => void outbox.flushNow()}>Отправить сейчас</SecondaryButton></span>
        </div>
      )}
      {lastSent.length > 0 && (
        <div role="status" data-testid="outbox-sent" style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap", padding: 12, borderRadius: 10, border: "1px solid var(--urgency-ok-fg)", background: "var(--urgency-ok-bg)", color: "var(--urgency-ok-fg)" }}>
          <IconCloudCheck size={18} aria-hidden="true" />
          <span>
            Из очереди ушло: принято {accepted}
            {rejected > 0 && `, отклонено ${rejected} (не хватило продуктов)`}
            {dropped > 0 && `, не принято ${dropped}`}.
          </span>
          <TextButton size="compact-sm" onClick={outbox.dismiss}>Закрыть</TextButton>
        </div>
      )}
    </div>
  );
}
