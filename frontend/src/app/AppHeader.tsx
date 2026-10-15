import { IconLogout, IconUser } from "@tabler/icons-react";
import { useMediaQuery } from "@mantine/hooks";
import { Pill } from "../components/Pill";
import { TextButton } from "../components/Buttons";
import { UrgencyCounter } from "../components/UrgencyCounter";
import { BATCH_STATES } from "../lib/urgency";
import { formatClock } from "../lib/format";
import { useAuth } from "../auth/AuthContext";
import { roleLabel } from "../auth/roles";
import { modeLabel } from "./clock";
import { RiskCounter } from "../pages/today";
import { useBadges } from "./BadgesContext";
import { useClock } from "./ClockContext";
import { useConnection } from "./ConnectionContext";

const connLabel = { online: "На связи", offline: "Нет связи", connecting: "Подключение…" } as const;
const connColor = { online: "var(--urgency-ok-fg)", offline: "var(--urgency-critical-fg)", connecting: "var(--urgency-warn-fg)" } as const;

export const USER_COLLAPSE_QUERY = "(max-width: 1439px)";

/** Имя и роль; на узкой ширине — только иконка, текст в подсказке и aria-label. */
export function UserBadge({ name, role, narrow, children }: { name: string; role: string; narrow: boolean; children?: React.ReactNode }) {
  const full = `${name} · ${role}`;
  return (
    <span data-testid="user-badge" style={{ display: "inline-flex", alignItems: "center", gap: 4, whiteSpace: "nowrap", font: "var(--type-small)", color: "var(--text-muted)" }}>
      {narrow ? (
        <span title={full} aria-label={full} role="img" data-collapsed="true" style={{ display: "inline-flex" }}>
          <IconUser size={18} aria-hidden="true" />
        </span>
      ) : (
        full
      )}
      {children}
    </span>
  );
}

/** Белая шапка на каждом экране: время стенда, режим, счётчики, связь, роль. */
export function AppHeader() {
  const { now, info } = useClock();
  const conn = useConnection();
  const { state, logout } = useAuth();
  const { alerts, unconfirmed } = useBadges();
  const alertsTotal = alerts ? alerts.expiring + alerts.doomed + alerts.expired : 0;
  // Первым при нехватке места сжимается «Гость · оператор»: до иконки с подсказкой.
  const narrow = useMediaQuery(USER_COLLAPSE_QUERY) ?? false;
  const t = now && info ? formatClock(now, info.timezone) : null;
  return (
    <header
      data-testid="app-header"
      className="so-header"
      style={{
        position: "sticky", top: 0, zIndex: 10, display: "flex", alignItems: "center", flexWrap: "wrap", gap: 12, whiteSpace: "nowrap",
        minHeight: "var(--header-h)", padding: "8px var(--gutter)",
      }}
    >
      <span aria-label="Время стенда" style={{ display: "inline-flex", gap: 8, alignItems: "baseline" }}>
        <span style={{ font: "var(--type-clock)" }}>{t ? t.time : "--:--:--"}</span>
        <span className="so-header-day" style={{ font: "var(--type-small)", color: "var(--text-muted)" }}>{t?.day}</span>
      </span>
      {info && (
        <span className="so-header-optional"><Pill title={info.simulated ? "Время стенда симулированное, не реальное" : undefined}>{modeLabel(info)}</Pill></span>
      )}
      <span style={{ flex: 1 }} />
      <span className="so-header-optional"><RiskCounter /></span>
      {alerts && (
        // Контракт e2e (10-demo.spec.ts): одна ссылка data-testid="alerts-counter" с aria-label «Предупреждения: N».
        <a
          href="#/alerts"
          data-testid="alerts-counter"
          aria-label={alertsTotal === 0 ? "Предупреждений нет" : `Предупреждения: ${alertsTotal}`}
          style={{ display: "inline-flex", alignItems: "center", gap: 6, textDecoration: "none" }}
        >
          {alertsTotal === 0 && <span style={{ font: "var(--type-small)", color: "var(--text-muted)" }}>Предупреждений нет</span>}
          {alerts.expiring > 0 && <UrgencyCounter def={BATCH_STATES.expiring} n={alerts.expiring} text="истекают" />}
          {alerts.doomed > 0 && <UrgencyCounter def={BATCH_STATES.doomed} n={alerts.doomed} text="спишутся" />}
          {alerts.expired > 0 && <UrgencyCounter def={BATCH_STATES.expired} n={alerts.expired} text="просрочены" />}
        </a>
      )}
      {unconfirmed != null && unconfirmed > 0 && (
        <UrgencyCounter def={{ ...BATCH_STATES.expiring, label: "Срок не подтверждён" }} n={unconfirmed} text="без подтверждённого срока" href="#/receiving" />
      )}
      <span role="status" data-testid="connection" style={{ color: connColor[conn], font: "var(--type-label)" }}>
        ● {connLabel[conn]}
      </span>
      {state.kind === "user" && (
        <UserBadge name={state.login ?? "Гость"} role={roleLabel[state.role]} narrow={narrow}>
          {state.authRequired && (
            <TextButton size="compact-sm" onClick={() => void logout()} leftSection={<IconLogout size={16} aria-hidden="true" />}>
              Выйти
            </TextButton>
          )}
        </UserBadge>
      )}
    </header>
  );
}
