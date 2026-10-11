import { useMemo } from "react";
import { formatDateTime, formatExact, formatRelative, formatTime } from "../lib/format";
import { useClock } from "./ClockContext";

const FALLBACK_TZ = "Europe/Moscow";

/**
 * Форматы времени в зоне ресторана и от времени стенда (GET /api/clock), а не от часов браузера.
 * Пока часы не получены, берётся Europe/Moscow и «сейчас» браузера.
 */
export function useStandFormat() {
  const { now, info } = useClock();
  const tz = info?.timezone ?? FALLBACK_TZ;
  const nowMs = (now ?? new Date()).getTime();
  return useMemo(
    () => ({
      timezone: tz,
      now: new Date(nowMs),
      /** «через 14 ч» */
      relative: (t: Date | string | number, opts?: { expired?: boolean }) => formatRelative(t, nowMs, opts),
      /** «9 окт., 08:00» */
      dateTime: (t: Date | string | number) => formatDateTime(t, tz, nowMs),
      /** «08:00» */
      time: (t: Date | string | number) => formatTime(t, tz),
      /** «вс, 11 окт., 19:20 МСК» — для подсказки */
      exact: (t: Date | string | number) => formatExact(t, tz),
    }),
    [tz, nowMs],
  );
}
