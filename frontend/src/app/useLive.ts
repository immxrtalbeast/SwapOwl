import { useEffect, useRef } from "react";
import { events } from "../api/events";

const COALESCE_MS = 150;

/**
 * Запускает загрузки по одной: если во время загрузки пришёл запрос, она повторяется один раз после завершения.
 * Так последняя загрузка начинается после последнего события, и её ответ приходит последним.
 */
export function serialLoader(load: () => void | Promise<void>): () => Promise<void> {
  let running = false;
  let dirty = false;
  return async () => {
    if (running) {
      dirty = true;
      return;
    }
    running = true;
    try {
      do {
        dirty = false;
        try {
          await load();
        } catch {
          /* ошибку показывает сам экран */
        }
      } while (dirty);
    } finally {
      running = false;
    }
  };
}

/**
 * Загружает данные при монтировании, по событиям нужных типов и при каждом (пере)подключении SSE.
 * Пачка событий подряд сводится к одной перезагрузке, загрузки не идут внахлёст.
 */
export function useLive(load: () => void | Promise<void>, types: readonly string[]) {
  const loadRef = useRef(load);
  loadRef.current = load;
  const key = types.join(",");

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | null = null;
    const exec = serialLoader(() => loadRef.current());
    const run = () => {
      if (timer) return;
      timer = setTimeout(() => {
        timer = null;
        void exec();
      }, COALESCE_MS);
    };
    void exec();
    const offEvent = events.onEvent(run, key ? key.split(",") : undefined);
    const offResync = events.onResync(run);
    return () => {
      offEvent();
      offResync();
      if (timer) clearTimeout(timer);
    };
  }, [key]);
}
