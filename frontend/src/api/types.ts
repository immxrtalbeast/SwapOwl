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
}

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

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message);
  }
}
