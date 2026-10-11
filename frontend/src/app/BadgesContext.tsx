import { createContext, useContext } from "react";
import type { ReactNode } from "react";
import { useAlerts } from "../pages/alerts/useAlerts";
import { countAlerts } from "../pages/alerts/urgency";
import type { AlertCounts } from "../pages/alerts/urgency";
import { useReceiving } from "../pages/receiving/useReceiving";

interface Badges {
  /** null, пока не загружено. */
  alerts: AlertCounts | null;
  /** Партий без подтверждённого срока; null, пока не загружено. */
  unconfirmed: number | null;
}

const Ctx = createContext<Badges>({ alerts: null, unconfirmed: null });

/** Один раз читает ленту предупреждений и приёмку для счётчиков в шапке и на пунктах меню. */
export function BadgesProvider({ children }: { children: ReactNode }) {
  const [alerts] = useAlerts();
  const [receiving] = useReceiving();
  const value: Badges = {
    alerts: alerts.kind === "ready" ? countAlerts(alerts.alerts) : null,
    unconfirmed: receiving.kind === "ready" ? receiving.rows.length : null,
  };
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export const useBadges = () => useContext(Ctx);
