import type { ReactNode } from "react";

/** Направление стрелки от пометки к объекту. */
type Direction = "down" | "left" | "right";

const PATHS: Record<Direction, string> = {
  down: "M6 4 C 10 14, 22 18, 30 36 M30 36 L21 30 M30 36 L32 25",
  left: "M44 20 C 30 10, 14 14, 6 26 M6 26 L16 25 M6 26 L11 17",
  right: "M4 20 C 18 10, 34 14, 42 26 M42 26 L32 25 M42 26 L37 17",
};

/**
 * Рукописная пометка Neucha + SVG-стрелка (штрих 4 px, round, от руки).
 * Одна-две на экран, только там, где объясняет число или действие; в таблицах, формах и ошибках не ставим.
 * При prefers-reduced-motion штрих не анимируется (global.css).
 */
export function HandNote({ children, arrow = "down" }: { children: ReactNode; arrow?: Direction | null }) {
  return (
    <span style={{ display: "inline-flex", alignItems: "flex-end", gap: 4, font: "var(--type-hand)", color: "var(--brand)" }}>
      <span>{children}</span>
      {arrow && (
        <svg className="so-arrow" width="48" height="40" viewBox="0 0 48 40" fill="none" aria-hidden="true">
          <path d={PATHS[arrow]} pathLength={1} stroke="var(--brand)" strokeWidth="4" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      )}
    </span>
  );
}
