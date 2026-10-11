import { createContext, useContext, useEffect, useState } from "react";
import type { ReactNode } from "react";
import { api } from "../api/client";
import type { ClockInfo } from "../api/client";
import { useLive } from "./useLive";

interface Snapshot {
  info: ClockInfo;
  fetchedAt: number;
}

interface ClockValue {
  /** null, пока часы не получены (или не удалось получить). */
  now: Date | null;
  info: ClockInfo | null;
}

const Ctx = createContext<ClockValue>({ now: null, info: null });

export function ClockProvider({ children }: { children: ReactNode }) {
  const [snap, setSnap] = useState<Snapshot | null>(null);
  const [tick, setTick] = useState(() => Date.now());

  useLive(async () => {
    try {
      setSnap({ info: await api.clock(), fetchedAt: Date.now() });
    } catch {
      /* связь показывает индикатор; прежние часы остаются */
    }
  }, ["clock.changed", "stand.reset"]);

  useEffect(() => {
    const t = setInterval(() => setTick(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);

  const value: ClockValue = snap
    ? { info: snap.info, now: new Date(Date.parse(snap.info.now) + Math.max(0, tick - snap.fetchedAt) * snap.info.speed) }
    : { info: null, now: null };
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export const useClock = () => useContext(Ctx);
