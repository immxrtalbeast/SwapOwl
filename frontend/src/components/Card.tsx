import type { CSSProperties, ReactNode } from "react";

interface Props {
  title?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
  /** Без внутреннего отступа: для таблиц, которые тянутся до краёв карточки. */
  flush?: boolean;
  /** Стекло (FS-161): только для оболочки и KPI; таблицы и формы — на непрозрачной карточке. */
  glass?: boolean;
  style?: CSSProperties;
}

/** Карточка: --surface, рамка --border, радиус --radius-card, отступ 16; glass — полупрозрачная с размытием. */
export function Card({ title, actions, children, flush, glass, style }: Props) {
  return (
    <section
      className={glass ? "so-glass" : undefined}
      style={{
        ...(glass ? {} : { background: "var(--surface)", border: "1px solid var(--border)", boxShadow: "var(--shadow-sm)" }),
        borderRadius: "var(--radius-card)",
        padding: flush ? 0 : "var(--card-pad)",
        minWidth: 0,
        overflow: flush ? "hidden" : undefined,
        ...style,
      }}
    >
      {(title || actions) && (
        <header style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, padding: flush ? "12px var(--card-pad)" : "0 0 12px", borderBottom: flush ? "1px solid var(--border)" : undefined }}>
          {title && <h2 style={{ margin: 0, font: "var(--type-h3)" }}>{title}</h2>}
          {actions}
        </header>
      )}
      {children}
    </section>
  );
}
