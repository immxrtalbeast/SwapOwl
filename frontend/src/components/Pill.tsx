import type { ReactNode } from "react";

/** Плашка: «Теневой режим», «Реальное время». Фон --brand-tint, текст --brand-ink, без капса. */
export function Pill({ children, title }: { children: ReactNode; title?: string }) {
  return (
    <span
      title={title}
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: 6,
        padding: "8px 16px",
        borderRadius: "var(--radius-pill)",
        background: "var(--brand-tint)",
        color: "var(--brand-ink)",
        font: "var(--type-caption)",
        whiteSpace: "nowrap",
      }}
    >
      {children}
    </span>
  );
}
