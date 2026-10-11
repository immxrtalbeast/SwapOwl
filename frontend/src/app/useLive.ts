import { useEffect, useRef } from "react";
import { events } from "../api/events";

const COALESCE_MS = 150;

/**
 * Загружает данные при монтировании, по событиям нужных типов и при каждом (пере)подключении SSE.
 * Пачка событий подряд сводится к одной перезагрузке.
 */
export function useLive(load: () => void | Promise<void>, types: readonly string[]) {
  const loadRef = useRef(load);
  loadRef.current = load;
  const key = types.join(",");

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | null = null;
    const run = () => {
      if (timer) return;
      timer = setTimeout(() => {
        timer = null;
        void loadRef.current();
      }, COALESCE_MS);
    };
    void loadRef.current();
    const offEvent = events.onEvent(run, key ? key.split(",") : undefined);
    const offResync = events.onResync(run);
    return () => {
      offEvent();
      offResync();
      if (timer) clearTimeout(timer);
    };
  }, [key]);
}
