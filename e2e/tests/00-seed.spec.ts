import { test, expect } from "@playwright/test";
import { Stand, hoursBetween, localHM } from "./stand";

// Условия сида из .ai/demo.md («Условия сида»): состояние сразу после сброса.
// Все проверки мягкие (expect.soft): один прогон показывает все расхождения сида со сценарием.

let stand: Stand;

test.beforeAll(async ({ playwright }, info) => {
  stand = await Stand.open(playwright, info.project.use.baseURL!);
});

test("сид после сброса соответствует условиям демо-сценария", async () => {
  await stand.reset();
  const clock = await stand.clock();
  expect.soft(localHM(clock.now, clock.timezone), "время стенда после сброса — T0 = 11:00").toBe("11:00");
  expect.soft(clock.speed, "скорость ×1").toBe(1);
  expect.soft(clock.simulator_running, "симулятор остановлен").toBe(false);
  expect.soft((await stand.stopList()).map((d) => d.name), "стоп-лист пуст").toEqual([]);

  // Лосось: Л-1 2 кг, истекает через 5 ч, уже «истекает»; Л-2 4 кг, истекает через 3 суток.
  const salmon = (await stand.products()).find((p) => p.name === "Лосось");
  expect.soft(salmon, "продукт «Лосось»").toBeTruthy();
  if (salmon) {
    const b = (await stand.batches(salmon.id)).filter((x) => x.remaining.qty > 0);
    expect.soft(b.length, "у лосося две партии (Л-1 и Л-2)").toBe(2);
    const [l1, l2] = b;
    if (l1) {
      expect.soft(l1.remaining.qty, "Л-1 — 2 кг").toBe(2000);
      expect.soft(hoursBetween(clock.now, l1.expires_at), "Л-1 истекает через 5 ч").toBeCloseTo(5, 1);
      expect.soft(l1.status, "Л-1 в статусе «истекает»").toBe("expiring");
      expect.soft(hoursBetween(l1.received_at, clock.now), "Л-1 принята 3 суток назад").toBeCloseTo(72, 0);
    }
    if (l2) {
      expect.soft(l2.remaining.qty, "Л-2 — 4 кг").toBe(4000);
      expect.soft(hoursBetween(clock.now, l2.expires_at), "Л-2 истекает через 3 суток").toBeCloseTo(72, 0);
    }
  }

  // Сливки: одна партия С-1, 600 мл, истекает через 26 ч.
  const cream = (await stand.products()).find((p) => p.name === "Сливки");
  expect.soft(cream, "продукт «Сливки»").toBeTruthy();
  if (cream) {
    const b = (await stand.batches(cream.id)).filter((x) => x.remaining.qty > 0);
    expect.soft(b.length, "у сливок одна партия (С-1)").toBe(1);
    if (b[0]) {
      expect.soft(b[0].remaining.qty, "С-1 — 600 мл").toBe(600);
      expect.soft(hoursBetween(clock.now, b[0].expires_at), "С-1 истекает через 26 ч").toBeCloseTo(26, 1);
    }
  }

  // Блюда и рецепты.
  const dishes = await stand.dishes();
  const norm = (dish: string, product: string) =>
    dishes.find((d) => d.name === dish)?.recipe.find((r) => r.product_id === product)?.quantity.qty;
  const sid = salmon?.id ?? "salmon", cid = cream?.id ?? "cream";
  for (const [dish, product, qty] of [
    ["Паста с лососем", sid, 150], ["Паста с лососем", cid, 100],
    ["Стейк из лосося", sid, 200], ["Крем-суп", cid, 200],
  ] as const) {
    expect.soft(norm(dish, product), `«${dish}»: ${product} ${qty} на порцию`).toBe(qty);
  }
  const plain = dishes.filter((d) => !d.recipe.some((r) => r.product_id === sid || r.product_id === cid));
  expect.soft(plain.length, "не менее пяти блюд без лосося и сливок").toBeGreaterThanOrEqual(5);
  for (const d of plain) {
    expect.soft(d.servings, `«${d.name}»: остальных продуктов хватает на 40 порций`).toBeGreaterThanOrEqual(40);
  }
});
