import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { Landing } from "./Landing";
import { isLandingLocation } from "./route";

/** Показывает лендинг на корне без хэша, иначе — приложение. Лендинг не требует входа. */
export function LandingGate({ children }: { children: ReactNode }) {
  const [landing, setLanding] = useState(() => isLandingLocation(window.location));
  useEffect(() => {
    const on = () => setLanding(isLandingLocation(window.location));
    window.addEventListener("hashchange", on);
    return () => window.removeEventListener("hashchange", on);
  }, []);
  return landing ? <Landing /> : <>{children}</>;
}
