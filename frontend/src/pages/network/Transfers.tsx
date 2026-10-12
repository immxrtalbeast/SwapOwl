import { useState } from "react";
import type { HubTransfer } from "../../api/network";
import { Card, EmptyState } from "../../components";
import { formatQty, formatRub } from "../../lib/format";

/** Текст подсказки: «Лосось 1,2 кг: точка A → точка B, спасёт ≈ N ₽». */
export function transferText(t: HubTransfer): string {
  const base = `${t.product_name} ${formatQty(t.qty)}: ${t.from_name} → ${t.to_name}`;
  return t.saves_minor == null ? base : `${base}, спасёт ≈ ${formatRub(t.saves_minor)}`;
}

function CopyInvoice({ invoice }: { invoice: string }) {
  const [state, setState] = useState<"idle" | "done" | "failed">("idle");
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(invoice);
      setState("done");
    } catch {
      setState("failed");
    }
  };
  return (
    <span style={{ display: "inline-flex", gap: 8, alignItems: "center" }}>
      <button type="button" onClick={() => void copy()} style={{ font: "var(--type-button)", color: "var(--brand-ink)", cursor: "pointer" }}>
        Скопировать накладную перемещения
      </button>
      <span role="status" style={{ color: "var(--text-muted)" }}>
        {state === "done" ? "Скопировано" : state === "failed" ? "Не удалось скопировать" : ""}
      </span>
    </span>
  );
}

/** Блок «Перемещения»: только подсказки, перемещение в API точки не выполняется. */
export function Transfers({ transfers }: { transfers: HubTransfer[] }) {
  return (
    <Card title="Перемещения">
      {transfers.length === 0 ? (
        <EmptyState title="Перемещать нечего" text="Нет партий под риском, которые другая точка успеет продать." />
      ) : (
        <ul style={{ listStyle: "none", margin: 0, padding: 0, display: "grid", gap: 12 }}>
          {transfers.map((t) => (
            <li key={`${t.from_id}-${t.batch_id}-${t.to_id}`} style={{ display: "flex", gap: 12, alignItems: "center", flexWrap: "wrap" }}>
              <span style={{ font: "var(--type-body-strong)" }}>{transferText(t)}</span>
              <CopyInvoice invoice={t.invoice} />
            </li>
          ))}
        </ul>
      )}
      <p style={{ margin: "12px 0 0", color: "var(--text-muted)" }}>
        Только подсказка: перемещение оформляется вручную по накладной, в системе точки оно пока не выполняется.
      </p>
    </Card>
  );
}
