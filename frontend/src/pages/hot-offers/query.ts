/** product_id из адреса экрана: `#/hot-offers?product_id=milk`. Нет параметра — null. */
export function productIdFromHash(hash: string): string | null {
  const q = hash.split("?")[1];
  if (!q) return null;
  const v = new URLSearchParams(q).get("product_id");
  return v ? v : null;
}

/** Адрес экрана горячих предложений для блюд, спасающих продукт. */
export function hotOffersUrl(productId: string): string {
  return `#/hot-offers?product_id=${encodeURIComponent(productId)}`;
}
