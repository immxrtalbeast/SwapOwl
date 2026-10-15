import type { Role } from "../auth/roles";
// Типы ответов совпадают с контрактом бэкенда (.ai/solution.md, 7.4).
export type Unit = "g" | "ml" | "pcs";

export interface Qty {
  qty: number;
  unit: Unit;
}

export interface ProductStock {
  id: string;
  name: string;
  unit: Unit;
  available: Qty;
  batches_count: number;
  nearest_expires_at: string | null;
  /** Срок после вскрытия упаковки, ч; null — вскрытие срок не меняет. */
  shelf_after_open_hours?: number | null;
}

export interface Batch {
  id: number;
  product_id: string;
  delivery_id: string;
  delivery_created_at: string;
  quantity: Qty;
  remaining: Qty;
  received_at: string;
  expires_at: string;
  time_left_seconds: number;
  status: "ok" | "expired";
  expiry_source: ExpirySource;
  /** Исходная партия и момент вскрытия — у партии, выделенной вскрытием упаковки. */
  parent_batch_id?: number | null;
  opened_at?: string | null;
}

export type ExpirySource = "delivery" | "product_default" | "confirmed";

/** Строка GET /api/receiving: партия со сроком из продукта, ещё не подтверждённым кладовщиком. */
export interface ReceivingBatch {
  id: number;
  product_id: string;
  product_name: string;
  delivery_id: string;
  delivery_number: string;
  quantity: Qty;
  remaining: Qty;
  received_at: string;
  expires_at: string;
  time_left_seconds: number;
  expiry_source: ExpirySource;
}

/** Тело PATCH /api/batches/{id}/expiry: ровно одно поле. */
export type ExpiryPatch = { expires_on: string } | { shelf_life_hours: number };

export interface DeliveryItemInput {
  product_id: string;
  name?: string;
  quantity: string; // только строка: "1.5"
  unit?: "g" | "kg" | "ml" | "l" | "pcs";
  shelf_life_hours?: number;
  expires_on?: string;
}

/** Позиция распознанной накладной (POST /api/invoices/recognize). quantity — строка в unit. */
export interface InvoiceLine {
  raw_name: string;
  product_id: string | null;
  quantity: string;
  unit: "g" | "kg" | "ml" | "l" | "pcs";
  expires_on: string | null;
  shelf_life_hours: number | null;
  confidence: "high" | "low";
  note?: string;
}

/** Черновик поставки с фото накладной; date — дата документа, не срок годности. demo — образец без модели. */
export interface InvoiceDraft {
  number: string;
  date?: string;
  supplier?: string;
  model: string;
  demo: boolean;
  lines: InvoiceLine[];
}

export interface DeliveryInput {
  id: string;
  number: string;
  created_at: string;
  items: DeliveryItemInput[];
}

export interface DeliveryResult {
  id: string;
  number: string;
  received_at: string;
  replayed: boolean;
  batches: { id: number; line_no: number; product_id: string; expires_at: string; status: string }[];
  warnings: { line_no: number; code: string; message: string }[];
}

export interface Dish {
  id: string;
  name: string;
  recipe: { product_id: string; quantity: Qty }[];
}

export interface OrderItemInput {
  dish_id: string;
  portions: number;
}

export interface OrderMissing {
  product_id: string;
  product_name: string;
  required: Qty;
  available: Qty;
  missing: Qty;
}

export interface OrderWriteoff {
  product_id: string;
  product_name?: string;
  batch_id: number;
  qty: Qty;
  expires_at: string;
}

export interface OrderShortfall {
  product_id: string;
  product_name: string;
  quantity: Qty;
}

export interface OrdersPage {
  orders: Order[];
  /** Число заказов с этим статусом целиком, а не размер страницы. */
  total: number;
}

export interface Order {
  id: string;
  status: "accepted" | "rejected";
  at: string;
  replayed: boolean;
  items: OrderItemInput[];
  source?: "api" | "pos";
  writeoffs: OrderWriteoff[];
  missing: OrderMissing[];
  /** Блюда заказа на паузе у менеджера (409 dish_on_hold): заказ отклонён целиком. */
  held?: { dish_id: string; note: string }[];
  shortfalls?: OrderShortfall[];
  /** Время отмены принятого заказа; есть только в списке заказов. */
  cancelled_at?: string;
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public details: Record<string, unknown> = {},
  ) {
    super(message);
  }
}

export interface StopReason {
  product_id: string;
  product_name: string;
  required: Qty;
  available: Qty;
  missing: Qty;
}

// Ручная пауза блюда (GET /api/dish-holds): менеджер снял блюдо с продажи, заказ на него отклоняется (409 dish_on_hold).
export interface DishHold {
  dish_id: string;
  note: string;
  set_at: string;
  set_by: string;
}

// Строка GET /api/dishes: порции считает бэкенд.
export interface DishServings {
  id: string;
  name: string;
  available: boolean;
  servings: number;
  missing: StopReason[];
  recipe: { product_id: string; quantity: Qty }[];
}

export interface UserAccount {
  id: number;
  login: string;
  role: Role;
  created_at: string;
}

export interface ApiKeyInfo {
  id: number;
  name: string;
  role: Exclude<Role, "admin">;
  created_at: string;
}

/** Ответ на создание ключа: поле key приходит один раз. */
export interface CreatedApiKey extends ApiKeyInfo {
  key: string;
}

// Строка GET /api/alerts: истекающие партии и просроченные за последние 24 ч стенда (7.4).
export interface Alert {
  batch_id: number;
  product_id: string;
  product_name: string;
  remaining: Qty;
  expires_at: string;
  time_left_seconds: number;
  status: "expiring" | "expired";
  doomed: boolean;
}

// Строка GET /api/supply-suggestions (7.4).
export interface SupplySuggestion {
  product_id: string;
  product_name: string;
  quantity: Qty;
  usable_stock: Qty;
  rate_per_day: Qty;
  stockout_at: string | null;
  blocks_dishes: number;
  urgency: "now" | "soon" | "planned";
  explanation: string;
  /** Обычный срок поставщика из карточки, ч: уходит в поставку явно (FS-160). null — срок вводит оператор. */
  shelf_life_hours: number | null;
}

// GET /api/analytics/writeoffs и /shortfalls (7.4): деньги — целые копейки.
export interface MoneyRow {
  product_id: string;
  product_name: string;
  reason?: string;
  quantity: Qty;
  cost_minor: number;
}
export interface MoneyDay {
  date: string;
  cost_minor: number;
}
export interface MoneyReport {
  from: string;
  to: string;
  total_minor: number;
  by_product: MoneyRow[];
  by_day: MoneyDay[];
}

// GET /api/forecast/writeoffs: под угрозой за период, стоимость по продуктам.
export interface ForecastReport {
  total_minor: number;
  products: { product_id: string; product_name: string; at_risk: Qty; total_minor: number }[];
}

// GET /api/analytics/shortages (7.4): продукты, которых не хватило отклонённым заказам.
export interface ShortageRow {
  product_id: string;
  product_name: string;
  rejected_orders: number;
  missing: Qty;
}
export interface ShortagesReport {
  from: string;
  to: string;
  rejected_orders: number;
  by_product: ShortageRow[];
}

// GET /api/analytics/bench (7.4): цифры есть только при available: true.
export interface BenchResult {
  available: boolean;
  runs?: number;
  main_algo?: string;
  waste_reduction_vs_random_pct?: number;
  waste_reduction_vs_nearest_pct?: number;
}

// GET /api/hot-offers (7.4).
export interface HotReason {
  product_id: string;
  product_name: string;
  batch_id: number;
  at_risk: Qty;
  expires_at: string;
  working_hours_left: number;
}

export interface HotOffer {
  /** Маржа порции, копейки; поля нет, если цена блюда или продукта неизвестна. */
  margin_minor?: number;
  /** margin_minor × target_today: маржа порций, которые надо продать сегодня. */
  expected_margin_minor?: number;
  dish_id: string;
  dish_name: string;
  priority: number;
  target_portions: number;
  target_today: number;
  usual_portions: number;
  sell_until: string;
  reasons: HotReason[];
  explanation: string;
  /** Менеджер ещё и закрепил это блюдо (FS-159); manager_note — подсказка для зала. */
  pinned?: boolean;
  manager_note?: string;
}

/** Блюдо, добавленное менеджером вручную: партии-причины нет, поэтому оно не в offers. */
export interface ManagerPick {
  dish_id: string;
  dish_name: string;
  note: string;
  set_at: string;
  set_by: string;
}

/** Блюдо, скрытое менеджером; in_offers — без скрытия расчёт поставил бы его в список. */
export interface HiddenDish {
  dish_id: string;
  dish_name: string;
  in_offers: boolean;
  set_at: string;
  set_by: string;
}

export interface HotExcluded {
  dish_id: string;
  product_id: string;
  why: string;
}

export type HotAlgo = "a" | "b";

export interface HotOffersResponse {
  algo: HotAlgo;
  offers: HotOffer[];
  excluded: HotExcluded[];
  manager_picks?: ManagerPick[];
  hidden?: HiddenDish[];
}

// POST /api/supply-orders и GET /api/supply-orders/{id} (FS-121): заказ поставщику через outbox.
export type SupplyOrderStatus = "sent" | "delivered" | "error";

export interface SupplyOrder {
  id: string;
  status: SupplyOrderStatus;
  created_at: string;
  items: { product_id: string; product_name?: string; qty: number; unit?: string }[];
  attempts: number;
  last_error: string | null;
  replayed: boolean;
}

export interface SupplyOrderInput {
  id: string;
  items: { product_id: string; qty: number }[];
}

// POST /api/what-if (FS-167): пересчёт прогноза списаний по сценариям на копии снимка склада, ничего не пишет.
export type WhatIfType = "discount" | "order_less" | "demand";
export interface WhatIfScenarioInput {
  type: WhatIfType;
  dish_id?: string;
  product_id?: string;
  percent?: number;
  factor?: number;
}
export interface WhatIfRequest {
  horizon_days: number;
  scenarios: WhatIfScenarioInput[];
}
export interface WhatIfProduct {
  product_id: string;
  product_name: string;
  base_minor: number;
  scenario_minor: number;
  delta_minor: number;
}
export interface WhatIfResult extends WhatIfScenarioInput {
  total_minor: number;
  delta_minor: number;
  delta_pct: number;
  products: WhatIfProduct[];
  assumptions: string[];
}
export interface WhatIfResponse {
  horizon_days: number;
  base: { total_minor: number; products: WhatIfProduct[] };
  scenarios: WhatIfResult[];
  generated_at: string;
}

/** Нет связи с сервером (fetch не дошёл): заказ можно поставить в очередь и повторить с тем же id. */
export class NetworkError extends Error {}
