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
  shortfalls?: OrderShortfall[];
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
  dish_id: string;
  dish_name: string;
  priority: number;
  target_portions: number;
  usual_portions: number;
  sell_until: string;
  reasons: HotReason[];
  explanation: string;
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
