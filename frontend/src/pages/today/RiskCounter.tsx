import { IconFlame } from "@tabler/icons-react";
import { formatRub } from "../money/format";
import { useRisk } from "./useToday";

/** Компактный счётчик в шапке: «под риском сегодня: N ₽», клик → #/today. Пока данных нет — не показывается. */
export function RiskCounter() {
  const [state] = useRisk();
  if (state.kind !== "ready") return null;
  const sum = formatRub(state.atRisk.totalMinor);
  return (
    <a
      href="#/today"
      data-testid="risk-counter"
      aria-label={`Под риском сегодня: ${sum}`}
      style={{ display: "inline-flex", alignItems: "center", gap: 4, textDecoration: "none", font: "var(--type-label)", color: state.atRisk.totalMinor > 0 ? "var(--urgency-risk-fg)" : "var(--text-muted)" }}
    >
      <IconFlame size={16} aria-hidden="true" />
      под риском сегодня: <span className="so-num">{sum}</span>
    </a>
  );
}
