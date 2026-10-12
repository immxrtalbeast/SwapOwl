import type { HubPoint } from "../../api/network";

export function pt(over: Partial<HubPoint> & { id: string }): HubPoint {
  return {
    name: over.id, status: "ok", last_attempt_at: "2026-10-12T10:00:00Z", last_success_at: "2026-10-12T10:00:00Z", stand_time: null,
    expiring_batches: 0, expired_batches: 0, expiring_sum_minor: null, stop_list_count: 0, stop_list_names: [],
    writeoffs_today_minor: 0, writeoffs_7d_minor: 0, top_hot_offer: null, ...over,
  };
}
