import { test, expect, type Page } from "@playwright/test";
import { Stand, uuid, localHM, hoursBetween, type Batch } from "./stand";

// Демо-сценарий .ai/demo.md, шаги 0–9: действие → что на экране → проверка.
// Шаги зависят друг от друга, поэтому идут последовательно на одной странице; страница не
// перезагружается — изменения должны приходить по SSE (счётчик loads это проверяет).

test.describe.configure({ mode: "serial" });

let stand: Stand;
let page: Page;
let loads = 0;
const ids = { salmon: "", cream: "", pasta: "", steak: "", soup: "" };
let l1: Batch, l2: Batch, c1: Batch;

// Прыжок шага 6 — +8 ч (.ai/demo.md): «дворник» списывает Л-1 только после expires_at + окно
// опоздания (LATE_EVENTS_HOURS, .ai/solution.md 2.4а). Если окно стенда шире 2 ч, прыжок
// удлиняется, иначе списания не будет.
const lateHours = Number(process.env.E2E_LATE_EVENTS_HOURS ?? "2");

test.beforeAll(async ({ playwright, browser }, info) => {
  const baseURL = info.project.use.baseURL!;
  stand = await Stand.open(playwright, baseURL);
  page = await browser.newPage({ baseURL, locale: "ru-RU" });
  page.on("load", () => { loads++; });
});

test.afterAll(async () => {
  await page?.close();
});

async function go(hash: string) {
  // Переход по хэшу — без перезагрузки страницы.
  await page.evaluate((h) => { window.location.hash = h; }, hash);
}

test("0. Сброс стенда: время 11:00, стоп-лист пуст", async () => {
  await stand.reset();
  expect(await stand.stopList()).toEqual([]);
  ids.salmon = (await stand.productByName("Лосось")).id;
  ids.cream = (await stand.productByName("Сливки")).id;
  ids.pasta = (await stand.dishByName("Паста с лососем")).id;
  ids.steak = (await stand.dishByName("Стейк из лосося")).id;
  ids.soup = (await stand.dishByName("Крем-суп")).id;

  await page.goto("/#/stock");
  const clock = await stand.clock();
  await expect(page.getByLabel("Время стенда")).toContainText(localHM(clock.now, clock.timezone));
  await expect(page.getByTestId("connection")).toContainText("На связи");
});

test("1. Симулятор ×600: остатки на складе меняются без перезагрузки", async () => {
  const before = await page.locator("main").innerText();
  const loadsBefore = loads;
  await stand.speed(600);
  const sim = await stand.simulator(true);
  expect(sim.status, `запуск симулятора: ${sim.text}`).toBe(200);
  await expect.poll(() => page.locator("main").innerText(), { timeout: 20_000 }).not.toBe(before);
  // Пауза: симулятор стоп, часы стоп — дальше сценарий детерминирован.
  expect((await stand.simulator(false)).status).toBe(200);
  await stand.speed(0);
  expect(loads, "страница не перезагружалась").toBe(loadsBefore);
  await expect(page.getByTestId("connection")).toContainText("На связи");

  const salmon = await stand.batches(ids.salmon);
  [l1, l2] = salmon; // порядок (expires_at, id): Л-1 — ближайший срок
  c1 = (await stand.batches(ids.cream))[0];
  expect(l1 && l2 && c1, "партии Л-1, Л-2 и С-1 есть на складе").toBeTruthy();
});

test("2. Лента предупреждений: Л-1 «истекает», счётчик ≥ 1", async () => {
  expect((await stand.batches(ids.salmon)).find((b) => b.id === l1.id)?.status, "статус Л-1").toBe("expiring");
  const alerts = await stand.get<{ batch_id: number; status: string }[]>("/api/alerts");
  expect(alerts.some((a) => a.batch_id === l1.id && a.status === "expiring"), "Л-1 в ленте предупреждений").toBe(true);

  await expect(page.getByTestId("alerts-counter")).toHaveAttribute("aria-label", /Предупреждения: [1-9]/);
  await page.getByTestId("alerts-counter").click();
  const row = page.locator("tr", { hasText: "Лосось" }).filter({ has: page.getByRole("link", { name: "Блюда, которые спасают" }) });
  await expect(row.first()).toBeVisible();
  await expect(row.first()).toHaveAttribute("data-urgency", /expiring|doomed/);
});

test("3. Клик по предупреждению → горячие предложения: первое — с лососем и Л-1, цель > 0", async () => {
  const loadsBefore = loads;
  await page.locator("tr", { hasText: "Лосось" }).getByRole("link", { name: "Блюда, которые спасают" }).first().click();
  const first = page.getByTestId("hot-offer").first();
  await expect(first).toContainText(`партия №${l1.id}`);
  expect(loads, "переход без перезагрузки").toBe(loadsBefore);

  const ho = await stand.get<{ offers: { dish_id: string; target_portions: number; reasons: { batch_id: number }[] }[] }>("/api/hot-offers");
  expect(ho.offers.length).toBeGreaterThan(0);
  expect(ho.offers[0].reasons.some((r) => r.batch_id === l1.id), "первая рекомендация ссылается на Л-1").toBe(true);
  expect(ho.offers[0].target_portions, "цель в порциях").toBeGreaterThan(0);
  const stopped = new Set((await stand.stopList()).map((d) => d.id));
  expect(ho.offers.filter((o) => stopped.has(o.dish_id)), "нет блюд из стоп-листа").toEqual([]);
});

test("4. Заказ: паста ×2 + стейк ×1 — лосось только из Л-1, сливки из С-1", async () => {
  const l2Before = (await stand.batches(ids.salmon)).find((b) => b.id === l2.id)!.remaining.qty;
  const r = await stand.post("/api/orders", { id: uuid(), items: [{ dish_id: ids.pasta, portions: 2 }, { dish_id: ids.steak, portions: 1 }] });
  expect(r.status, r.text).toBe(201);
  const w = r.json.writeoffs as { product_id: string; batch_id: number; qty: { qty: number } }[];
  const salmonW = w.filter((x) => x.product_id === ids.salmon);
  expect(salmonW.reduce((s, x) => s + x.qty.qty, 0), "лосось 500 г").toBe(500);
  expect(salmonW.every((x) => x.batch_id === l1.id), "весь лосось — из Л-1").toBe(true);
  expect(w.filter((x) => x.product_id === ids.cream).every((x) => x.batch_id === c1.id), "сливки — из С-1").toBe(true);
  expect((await stand.batches(ids.salmon)).find((b) => b.id === l2.id)!.remaining.qty, "Л-2 не тронута").toBe(l2Before);
});

test("5. Залп из 10 заказов на крем-суп: ровно S принято, сливки 0, блюда в стоп-листе", async () => {
  const s = (await stand.dishes()).find((d) => d.id === ids.soup)!.servings;
  expect(s, "сливок осталось на 1…5 порций крем-супа").toBeGreaterThanOrEqual(1);
  expect(s).toBeLessThanOrEqual(5);

  const r = await stand.post("/api/demo/burst", { dish_id: ids.soup, orders: 10 }, true);
  expect(r.status, r.text).toBe(200);
  expect(r.json.accepted, "принято ровно S").toBe(s);
  expect(r.json.rejected, "отклонено 10 − S").toBe(10 - s);

  const cream = (await stand.products()).find((p) => p.id === ids.cream)!;
  expect(cream.available.qty, "сливок 0").toBe(0);
  const remaining = (await stand.batches(ids.cream)).map((b) => b.remaining.qty);
  expect(remaining.every((q) => q >= 0), "остатки не отрицательны").toBe(true);

  const stopped = await stand.stopList();
  for (const id of [ids.soup, ids.pasta]) {
    const d = stopped.find((x) => x.id === id);
    expect(d, `${id} в стоп-листе`).toBeTruthy();
    expect(d!.missing.some((m) => m.product_id === ids.cream), `${id}: причина — сливки`).toBe(true);
  }

  const loadsBefore = loads;
  await go("#/menu");
  await expect(page.getByTestId(`dish-${ids.soup}`)).toContainText(/Сливк/);
  expect(loads, "без перезагрузки").toBe(loadsBefore);
});

test("6. Прыжок времени: остаток Л-1 списан с причиной «истёк срок» в момент expires_at", async () => {
  const before = await stand.clock();
  const hours = Math.max(8, Math.ceil(hoursBetween(before.now, l1.expires_at) + lateHours + 0.5));
  await stand.jump(hours);
  const after = await stand.clock();
  expect(hoursBetween(before.now, after.now), `время стенда + ${hours} ч`).toBeCloseTo(hours, 2);

  const mv = await stand.get<{ batch_id: number; kind: string; reason: string | null; at: string }[]>(
    `/api/movements?product_id=${ids.salmon}&kind=writeoff&limit=50`);
  const w = mv.find((m) => m.batch_id === l1.id && m.reason === "expired");
  expect(w, "движение writeoff/expired по Л-1").toBeTruthy();
  expect(Date.parse(w!.at), "время списания = expires_at").toBe(Date.parse(l1.expires_at));
  expect((await stand.batches(ids.salmon)).some((b) => b.id === l1.id), "Л-1 больше не на складе").toBe(false);
});

test("7. Предложение поставок: сливки первыми, блокируют 2 блюда", async () => {
  const s = await stand.get<{ product_id: string; quantity: { qty: number }; blocks_dishes: number }[]>("/api/supply-suggestions");
  expect(s[0]?.product_id, "первой строкой — сливки").toBe(ids.cream);
  expect(s[0].quantity.qty).toBeGreaterThan(0);
  expect(s[0].blocks_dishes, "блокируют 2 блюда").toBe(2);

  await go("#/supply");
  await expect(page.locator("main tbody tr").first()).toContainText("Сливки");
});

test("8. Поставка сливок и повтор той же поставки: второй раз — «уже принята», остаток тот же", async () => {
  const s = await stand.get<{ product_id: string; quantity: { qty: number; unit: string } }[]>("/api/supply-suggestions");
  const q = s.find((x) => x.product_id === ids.cream)!.quantity;
  const body = {
    id: `E2E-${uuid()}`, number: "e2e", created_at: new Date().toISOString(),
    items: [{ product_id: ids.cream, name: "Сливки", quantity: String(q.qty), unit: q.unit, shelf_life_hours: 72 }],
  };
  const first = await stand.post("/api/deliveries", body);
  expect(first.status, first.text).toBe(201);
  const after1 = (await stand.products()).find((p) => p.id === ids.cream)!.available.qty;
  const again = await stand.post("/api/deliveries", body);
  expect(again.status, again.text).toBe(200);
  expect(again.json.replayed).toBe(true);
  expect((await stand.products()).find((p) => p.id === ids.cream)!.available.qty, "остаток тот же").toBe(after1);
  const stopped = (await stand.stopList()).map((d) => d.id);
  expect(stopped).not.toContain(ids.soup);
  expect(stopped).not.toContain(ids.pasta);
  test.info().annotations.push({ type: "gap", description: "Кнопки «Принять предложенное» на экране «Поставки» нет — шаг проверен через API." });
});

test("9. Аналитика: цифра сравнения алгоритмов с числом прогонов", async () => {
  const res = await stand.api.get("/api/analytics/bench");
  test.skip(res.status() !== 200, `GET /api/analytics/bench → ${res.status()}: экран аналитики ещё не готов (FS-73)`);
  const bench = await res.json();
  test.skip(!bench.available, "результата bench нет — по 2.7 цифру не показываем");
  await go("#/analytics");
  await expect(page.locator("main")).toContainText(String(bench.runs));
  await expect(page.locator("main")).toContainText(/%/);
});
