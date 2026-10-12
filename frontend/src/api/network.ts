import type { Qty } from "./types";

// Клиент hub (cmd/hub, docs/network.md): сводка по точкам сети. Контракты точек не меняются: hub сам опрашивает их по HTTP.
// Токен hub во фронт не попадает (бандл открыт без логина): заголовок Authorization подставляет обратный прокси на
// том же origin (в dev — vite.config.ts). Hub не отвечает на CORS preflight, поэтому прямой cross-origin запрос не работает.

export interface HubAlert {
  batch_id: number;
  product_id: string;
  product_name: string;
  remaining: Qty;
  expires_at: string;
  time_left_seconds: number;
  status: "expiring" | "expired";
  doomed: boolean;
}

export interface HubStopItem {
  id: string;
  name: string;
}

export interface HubHotOffer {
  dish_id: string;
  dish_name: string;
  explanation?: string;
}

/** Накопленные списания (копейки) с начала прогона на момент `hour` — часов стенда с его начала. */
export interface HubSeriesPoint {
  hour: number;
  cost_minor: number;
}

/** Прогон точки группы ab: генератор идёт или на паузе, списания в ₽ по часам стенда. */
export interface HubRun {
  running: boolean;
  started_at: string;
  stand_time: string;
  total_minor: number;
  series: HubSeriesPoint[];
}

export type HubStatus = "ok" | "unreachable" | "unauthorized" | "error";

export interface HubPoint {
  id: string;
  name: string;
  /** Подпись в сравнении («Со SwapOwl», «Без подсказок»), из конфига hub. */
  label?: string;
  /** Группа точек; «ab» — пара для живого сравнения. */
  group?: string;
  run?: HubRun;
  /** Адрес точки для ссылки «Открыть точку»; hub отдаёт его, если он настроен. */
  url?: string;
  status: HubStatus;
  error?: string;
  last_attempt_at: string | null;
  last_success_at: string | null;
  stand_time: string | null;
  expiring_batches: number;
  expired_batches: number;
  /** Копейки; null, пока точка не отдаёт стоимость партий (docs/network.md). */
  expiring_sum_minor: number | null;
  stop_list_count: number;
  stop_list_names: string[];
  writeoffs_today_minor: number;
  writeoffs_7d_minor: number;
  top_hot_offer: HubHotOffer | null;
  alerts?: HubAlert[];
  stop_list?: HubStopItem[];
}

export interface HubTotals {
  points: number;
  ok: number;
  unavailable: number;
  expiring_batches: number;
  stop_list_count: number;
  writeoffs_today_minor: number;
  writeoffs_7d_minor: number;
}

export interface HubSummary {
  points: HubPoint[];
  totals: HubTotals;
}

/** Подсказка о перемещении партии между точками (только подсказка, docs/network.md). */
export interface HubTransfer {
  product_id: string;
  product_name: string;
  batch_id: number;
  qty: Qty;
  from_id: string;
  from_name: string;
  to_id: string;
  to_name: string;
  expires_at: string;
  /** Копейки; null, если у hub не заданы цены (HUB_PRICES). */
  saves_minor: number | null;
  text: string;
  /** Текст накладной для ручного оформления. */
  invoice: string;
}

type Env = Record<string, string | undefined>;
const env = (): Env => ((import.meta as unknown as { env?: Env }).env ?? {});

/** Адрес hub: по умолчанию тот же origin (hub за прокси, `VITE_HUB_URL=/`). Без хвостового «/». */
export function hubBase(e: Env = env()): string {
  return (e.VITE_HUB_URL ?? "").replace(/\/+$/, "");
}

/** Пункт «Сеть» виден сразу, если при сборке задан VITE_HUB_URL (флаг: hub за прокси на этом origin). */
export function hubConfigured(e: Env = env()): boolean {
  return Boolean(e.VITE_HUB_URL);
}

async function hubGet<T>(path: string): Promise<T> {
  const res = await fetch(`${hubBase()}${path}`);
  if (!res.ok) throw new Error(res.status === 401 ? "Hub не принял токен (401)." : `Hub ответил ${res.status}.`);
  return (await res.json()) as T;
}

export const network = {
  summary: () => hubGet<HubSummary>("/api/network/summary"),
  transfers: async () => (await hubGet<{ transfers: HubTransfer[] }>("/api/network/transfers")).transfers ?? [],
  point: (id: string) => hubGet<HubPoint>(`/api/network/points/${encodeURIComponent(id)}`),
  /** Hub доступен: ответ на сводку — JSON с массивом точек (HTML-заглушка или 404 основного API — нет). */
  available: async (): Promise<boolean> => {
    try {
      const s = await hubGet<Partial<HubSummary>>("/api/network/summary");
      return Array.isArray(s?.points);
    } catch {
      return false;
    }
  },
};
