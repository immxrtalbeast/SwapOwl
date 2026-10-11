import type { CSSProperties, ReactNode } from "react";

interface Props {
  title?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
  /** Без внутреннего отступа: для таблиц, которые тянутся до краёв карточки. */
  flush?: boolean;
  style?: CSSProperties;
}

/** Карточка: --surface, радиус 10, тень --shadow-sm, отступ 16. */
export function Card({ title, actions, children, flush, style }: Props) {
  return (
    <section
      style={{
        background: "var(--surface)",
        border: "1px solid var(--border)",
        borderRadius: 10,
        boxShadow: "var(--shadow-sm)",
        padding: flush ? 0 : "var(--card-pad)",
        minWidth: 0,
        ...style,
      }}
    >
      {(title || actions) && (
        <header style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, padding: flush ? "var(--card-pad) var(--card-pad) 0" : "0 0 12px" }}>
          {title && <h2 style={{ margin: 0, font: "var(--type-h3)" }}>{title}</h2>}
          {actions}
        </header>
      )}
      {children}
    </section>
  );
}
