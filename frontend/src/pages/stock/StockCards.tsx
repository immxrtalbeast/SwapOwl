import { IconChevronDown, IconChevronRight } from "@tabler/icons-react";
import { UrgencyBadge } from "../../components";
import { formatQty } from "../../lib/format";
import { BATCH_STATES, toneVars } from "../../lib/urgency";
import type { ReactNode } from "react";
import { openedMark } from "./opened";
import type { StockRow } from "./view";

interface Props {
  rows: readonly StockRow[];
  changed: ReadonlySet<string>;
  expiresText: (iso: string) => string;
  exactText: (iso: string) => string;
  onToggle: (productId: string) => void;
  timeZone: string;
  /** Действие над партией («Вскрыл упаковку»); null — действия нет. */
  batchAction?: (r: Extract<StockRow, { kind: "batch" }>) => ReactNode;
}

/** Склад на телефоне (< 640 px): вместо таблицы — карточки «продукт · остаток · состояние · годна до». */
export function StockCards({ rows, changed, expiresText, exactText, onToggle, timeZone, batchAction }: Props) {
  return (
    <ul aria-label="Продукты склада" style={{ listStyle: "none", margin: 0, padding: 12, display: "grid", gap: 8 }}>
      {rows.map((r) => {
        const at = r.kind === "batch" ? r.batch.expires_at : r.product.nearest_expires_at;
        const tone = BATCH_STATES[r.state].tone;
        const bg = tone ? toneVars(tone).row : undefined;
        return (
          <li
            key={r.key}
            className={changed.has(r.key) ? "so-row-changed" : undefined}
            style={{ marginLeft: r.kind === "batch" ? 16 : 0, padding: 12, border: "1px solid var(--border)", borderRadius: 8, background: bg ?? "var(--surface)", display: "grid", gap: 6 }}
          >
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 8 }}>
              {r.kind === "product" ? (
                <button
                  type="button"
                  onClick={() => onToggle(r.product.id)}
                  aria-expanded={r.expanded}
                  aria-label={`${r.expanded ? "Свернуть" : "Раскрыть"} партии: ${r.product.name}`}
                  style={{ display: "inline-flex", alignItems: "center", gap: 8, background: "none", border: 0, padding: 0, cursor: "pointer", font: "var(--type-body-strong)", color: "var(--text)", textAlign: "left" }}
                >
                  {r.expanded ? <IconChevronDown size={16} aria-hidden="true" /> : <IconChevronRight size={16} aria-hidden="true" />}
                  {r.product.name}
                </button>
              ) : (
                <span style={{ display: "grid" }}>
                  <span className="so-num" style={{ font: "var(--type-body-strong)" }}>партия №{r.batch.id}</span>
                  {r.batch.opened_at && <span style={{ color: "var(--text-muted)" }}>{openedMark(r.batch.opened_at, timeZone)}</span>}
                </span>
              )}
              <UrgencyBadge def={BATCH_STATES[r.state]} />
            </div>
            <div style={{ display: "flex", justifyContent: "space-between", gap: 8, font: "var(--type-body)" }}>
              <span>
                <span style={{ color: "var(--text-muted)" }}>Остаток: </span>
                <strong className="so-num">{formatQty(r.kind === "product" ? r.product.available : r.batch.remaining)}</strong>
              </span>
              <span>
                <span style={{ color: "var(--text-muted)" }}>Годна до: </span>
                {at ? <span title={exactText(at)}>{expiresText(at)}</span> : "—"}
              </span>
            </div>
            {r.kind === "batch" && batchAction?.(r)}
          </li>
        );
      })}
    </ul>
  );
}
