import { api } from "../api/client";
import type { Batch, BenchResult, HotOffer, ProductStock } from "../api/types";

/** Живые данные стенда для окна приложения на первом экране. */
export interface LiveData {
  now: Date;
  product: ProductStock;
  batch: Batch;
  offer: HotOffer | null;
}

/** Продукт с ближайшим сроком годности среди тех, у кого он есть. */
export function pickNearestProduct(products: ProductStock[]): ProductStock | null {
  const dated = products.filter((p) => p.nearest_expires_at && p.available.qty > 0);
  dated.sort((a, b) => Date.parse(a.nearest_expires_at!) - Date.parse(b.nearest_expires_at!));
  return dated[0] ?? null;
}

/** Первая истекающая (непросроченная, с ближайшим сроком) партия. */
export function pickFirstBatch(batches: Batch[]): Batch | null {
  const live = batches.filter((b) => b.status === "ok" && b.remaining.qty > 0);
  live.sort((a, b) => Date.parse(a.expires_at) - Date.parse(b.expires_at));
  return live[0] ?? null;
}

/** Цифра bench показывается только при available: true и наличии обеих величин (правило 2.7). */
export function benchNumbers(b: BenchResult | null): { runs: number; vsRandom: number; vsNearest: number } | null {
  if (!b || !b.available) return null;
  if (b.runs == null || b.waste_reduction_vs_random_pct == null || b.waste_reduction_vs_nearest_pct == null) return null;
  return { runs: b.runs, vsRandom: b.waste_reduction_vs_random_pct, vsNearest: b.waste_reduction_vs_nearest_pct };
}

export async function loadLive(): Promise<LiveData | null> {
  try {
    const [clock, products, hot] = await Promise.all([
      api.clock(),
      api.products(),
      api.hotOffers().catch(() => null),
    ]);
    const product = pickNearestProduct(products);
    if (!product) return null;
    const batch = pickFirstBatch(await api.batches(product.id));
    if (!batch) return null;
    return { now: new Date(clock.now), product, batch, offer: hot?.offers[0] ?? null };
  } catch {
    return null;
  }
}

export async function loadBench(): Promise<BenchResult | null> {
  try {
    return await api.bench();
  } catch {
    return null;
  }
}
