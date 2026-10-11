import { describe, expect, it } from "vitest";
import { hotOffersUrl, productIdFromHash } from "./query";

describe("productIdFromHash", () => {
  it("читает product_id", () => {
    expect(productIdFromHash("#/hot-offers?product_id=milk")).toBe("milk");
    expect(productIdFromHash(hotOffersUrl("a b/ё"))).toBe("a b/ё");
  });
  it("без параметра — null", () => {
    expect(productIdFromHash("#/hot-offers")).toBeNull();
    expect(productIdFromHash("#/hot-offers?product_id=")).toBeNull();
    expect(productIdFromHash("")).toBeNull();
  });
});
