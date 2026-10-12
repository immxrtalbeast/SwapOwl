import { useEffect, useState } from "react";
import { hubConfigured, network } from "../../api/network";

/**
 * Показывать ли пункт «Сеть»: задан VITE_HUB_URL либо hub отвечает на том же адресе.
 * На обычном стенде одной точки hub нет, и меню не меняется.
 */
export function useHubAvailable(): boolean {
  const [available, setAvailable] = useState(hubConfigured());
  useEffect(() => {
    if (available) return;
    let alive = true;
    void network.available().then((ok) => alive && setAvailable(ok));
    return () => {
      alive = false;
    };
  }, [available]);
  return available;
}
