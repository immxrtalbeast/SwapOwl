import { createContext, useContext, useEffect, useState } from "react";
import type { ReactNode } from "react";
import { events } from "../api/events";
import type { ConnectionStatus } from "../api/events";

const Ctx = createContext<ConnectionStatus>("connecting");

/** Владеет единственным EventSource приложения: открывает его при монтировании. */
export function ConnectionProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<ConnectionStatus>(events.getStatus());
  useEffect(() => {
    const off = events.onStatus(setStatus);
    events.start();
    setStatus(events.getStatus());
    return () => {
      off();
      events.stop();
    };
  }, []);
  return <Ctx.Provider value={status}>{children}</Ctx.Provider>;
}

export const useConnection = () => useContext(Ctx);
