import type { ReactNode } from "react";
import type { Tone } from "../lib/urgency";
import { toneVars } from "../lib/urgency";

export interface Column<T> {
  key: string;
  header: ReactNode;
  cell: (row: T) => ReactNode;
  /** Числа и количества: --type-num, выравнивание вправо. */
  numeric?: boolean;
  width?: number | string;
}

interface Props<T> {
  columns: readonly Column<T>[];
  rows: readonly T[];
  rowKey: (row: T) => string;
  /** Тон строки по шкале срочности: фон только у warn и risk. */
  rowTone?: (row: T) => Tone | undefined;
  /** Ключи строк, изменившихся по SSE: подсветка --row-changed с затуханием 1,5 с. */
  changed?: ReadonlySet<string>;
  caption: string;
  onRowClick?: (row: T) => void;
}

/**
 * Таблица: шапка --type-caption на --surface-sunken, числа --type-num, строка 44 px.
 * На узком экране прокручивается горизонтально внутри карточки, страница не двигается.
 */
export function DataTable<T>({ columns, rows, rowKey, rowTone, changed, caption, onRowClick }: Props<T>) {
  return (
    <div style={{ overflowX: "auto", maxWidth: "100%" }}>
      <table style={{ width: "100%", borderCollapse: "collapse", font: "var(--type-body)", fontVariantNumeric: "tabular-nums" }}>
        <caption style={{ position: "absolute", width: 1, height: 1, overflow: "hidden", clip: "rect(0 0 0 0)" }}>{caption}</caption>
        <thead>
          <tr style={{ background: "var(--surface-sunken)" }}>
            {columns.map((c) => (
              <th
                key={c.key}
                scope="col"
                style={{
                  padding: "var(--cell-py) var(--cell-px)",
                  textAlign: c.numeric ? "right" : "left",
                  font: "var(--type-caption)",
                  textTransform: "uppercase",
                  letterSpacing: ".04em",
                  color: "var(--text-muted)",
                  borderBottom: "1px solid var(--border)",
                  width: c.width,
                  whiteSpace: "nowrap",
                }}
              >
                {c.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => {
            const key = rowKey(row);
            const tone = rowTone?.(row);
            const bg = tone ? toneVars(tone).row : undefined;
            return (
              <tr
                key={key}
                className={changed?.has(key) ? "so-row-changed" : undefined}
                onClick={onRowClick ? () => onRowClick(row) : undefined}
                style={{ height: "var(--row-h)", background: bg, borderBottom: "1px solid var(--border)", cursor: onRowClick ? "pointer" : undefined }}
              >
                {columns.map((c) => (
                  <td
                    key={c.key}
                    className={c.numeric ? "so-num" : undefined}
                    style={{ padding: "var(--cell-py) var(--cell-px)", textAlign: c.numeric ? "right" : "left", verticalAlign: "middle" }}
                  >
                    {c.cell(row)}
                  </td>
                ))}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
