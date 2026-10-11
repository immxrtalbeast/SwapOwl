import {
  IconAlertTriangle, IconBan, IconBolt, IconCalendarEvent, IconCircleCheck, IconCircleDashed,
  IconCircleX, IconClock, IconHourglassLow, IconTrashX,
} from "@tabler/icons-react";
import type { UrgencyIcon } from "../lib/urgency";

export const URGENCY_ICONS: Record<UrgencyIcon, typeof IconBan> = {
  "circle-check": IconCircleCheck,
  "hourglass-low": IconHourglassLow,
  "trash-x": IconTrashX,
  "circle-x": IconCircleX,
  "circle-dashed": IconCircleDashed,
  bolt: IconBolt,
  clock: IconClock,
  "calendar-event": IconCalendarEvent,
  "alert-triangle": IconAlertTriangle,
  ban: IconBan,
};
