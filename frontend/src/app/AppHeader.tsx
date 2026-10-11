import type { ReactNode } from "react";
import { formatStandTime, modeLabel } from "./clock";
import { useClock } from "./ClockContext";
import { useConnection } from "./ConnectionContext";

const connLabel = { online: "На связи", offline: "Нет связи", connecting: "Подключение…" } as const;

/** Шапка на каждом экране: время стенда, режим, счётчик предупреждений (слот), индикатор связи. */
export function AppHeader({ alerts }: { alerts?: ReactNode }) {
  const { now, info } = useClock();
  const conn = useConnection();
  const t = now && info ? formatStandTime(now, info.timezone) : null;
  const color = conn === "online" ? "#1a7f37" : conn === "offline" ? "#b00020" : "#8a6d00";
  return (
    <>
      <header
        data-testid="app-header"
        style={{ display: "flex", alignItems: "center", flexWrap: "wrap", gap: 16, padding: "8px 16px", borderBottom: "1px solid #ddd", fontFamily: "system-ui, sans-serif" }}
      >
        <strong>StockSense</strong>
        <span aria-label="Время стенда" style={{ display: "inline-flex", gap: 8, alignItems: "baseline" }}>
          <span style={{ font: "500 20px/24px ui-monospace, monospace" }}>{t ? t.time : "--:--:--"}</span>
          <span>{t?.day}</span>
        </span>
        {info && (
          <span title={info.simulated ? "Время стенда симулированное, не реальное" : undefined}>
            {modeLabel(info)}
            {info.simulated && <em> · время симулированное</em>}
          </span>
        )}
        <span style={{ flex: 1 }} />
        {alerts}
        <span role="status" data-testid="connection" style={{ color, fontWeight: 600 }}>
          ● {connLabel[conn]}
        </span>
      </header>
      {conn === "offline" && (
        <div role="alert" style={{ background: "#fdecee", color: "#b00020", padding: "8px 16px", fontFamily: "system-ui, sans-serif" }}>
          Нет связи с сервером. Данные на экране могут быть устаревшими; идёт переподключение, после него всё обновится само.
        </div>
      )}
    </>
  );
}
