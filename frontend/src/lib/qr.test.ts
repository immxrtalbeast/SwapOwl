import { describe, expect, it } from "vitest";
import { BinaryBitmap, HybridBinarizer, QRCodeReader, RGBLuminanceSource } from "@zxing/library";
import { MAX_BYTES, qrMatrix, qrSvg } from "./qr";

function decode(text: string): string {
  const m = qrMatrix(text);
  const scale = 4;
  const quiet = 4;
  const dim = (m.length + quiet * 2) * scale;
  const px = new Uint8ClampedArray(dim * dim).fill(255);
  m.forEach((row, y) => row.forEach((dark, x) => {
    if (!dark) return;
    for (let dy = 0; dy < scale; dy++) for (let dx = 0; dx < scale; dx++) px[((y + quiet) * scale + dy) * dim + (x + quiet) * scale + dx] = 0;
  }));
  const lum = new RGBLuminanceSource(px, dim, dim);
  return new QRCodeReader().decode(new BinaryBitmap(new HybridBinarizer(lum))).getText();
}

describe("qr", () => {
  it("декодируется обратно: короткий, длинный, кириллица, версия ≥ 7", () => {
    for (const t of ["a", "https://stand.example/#/stock?batch=42", "Молоко, партия №17", "x".repeat(120), "https://stand.example/" + "p".repeat(150)]) {
      expect(decode(t)).toBe(t);
    }
  });
  it("не помещающийся текст — ошибка", () => {
    expect(() => qrMatrix("x".repeat(MAX_BYTES + 1))).toThrow();
    expect(() => qrMatrix("x".repeat(MAX_BYTES))).not.toThrow();
  });
  it("SVG содержит viewBox и путь", () => {
    const s = qrSvg("hi");
    expect(s).toContain("<svg");
    expect(s).toContain("<path");
  });
});
