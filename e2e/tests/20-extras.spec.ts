import { test, expect, type Page } from "@playwright/test";
import { Stand } from "./stand";

// Шаги 1, 9, 10 и 11 демо-сценария v2 (docs/demo-script.md): «Сегодня», приёмка сканом маркировки, вскрытая упаковка
// с этикеткой, экран «Сеть». Идут после 10-demo на свежем сбросе и от него не зависят.

test.describe.configure({ mode: "serial" });

let stand: Stand;
let page: Page;

test.beforeAll(async ({ playwright, browser }, info) => {
  const baseURL = info.project.use.baseURL!;
  stand = await Stand.open(playwright, baseURL);
  await stand.reset();
  page = await browser.newPage({ baseURL, locale: "ru-RU" });
});

test.afterAll(async () => {
  await page?.close();
});

const milkBatches = async () => {
  const milk = await stand.productByName("Молоко");
  return { id: milk.id, batches: await stand.batches(milk.id) };
};

test("1. «Сегодня»: сумма под риском в рублях и три блока действий", async () => {
  await page.goto("/#/today");
  const main = page.locator("main");
  await expect(main).toContainText("Под риском сегодня");
  await expect(main).toContainText(/\d[\d\s]*(,\d+)?\s*₽/);
  for (const block of ["Продать", "Заказать", "Проверить"]) await expect(main).toContainText(block);
});

test("9. Приёмка сканом: «Пример: молоко» — партия со сроком из маркировки (AI 17)", async () => {
  const before = (await milkBatches()).batches.map((b) => b.id);
  await page.goto("/#/receiving");
  await page.getByRole("button", { name: "Сканировать упаковку" }).click();
  await page.getByRole("button", { name: "Пример: молоко" }).click();
  // GTIN примера в справочнике нет — первый раз продукт выбирается вручную и запоминается на устройстве.
  const pick = page.getByLabel("Продукт");
  if (await pick.isVisible().catch(() => false)) {
    await pick.selectOption({ label: "Молоко" });
    await page.getByRole("button", { name: "Запомнить" }).click();
  }
  await expect(page.getByTestId("scan-line").first()).toContainText("годен до 2026-10-20");
  await page.getByRole("button", { name: /Принять поставку/ }).click();

  await expect.poll(async () => (await milkBatches()).batches.filter((b) => !before.includes(b.id)).length, { timeout: 15_000 }).toBe(1);
  const fresh = (await milkBatches()).batches.find((b) => !before.includes(b.id))!;
  // «Годен до 20.10.2026» — доступна весь день по времени ресторана (Europe/Moscow): до 21.10 00:00 МСК.
  expect(Date.parse(fresh.expires_at), "срок из AI 17").toBe(Date.parse("2026-10-20T21:00:00Z"));
});

test("10. Вскрытая упаковка: дочерняя партия со сроком 48 ч и этикетка «годно до»", async () => {
  const { batches: before } = await milkBatches();
  const clock = await stand.clock();
  await page.goto("/#/stock");
  await page.locator("main").getByText("Молоко", { exact: true }).first().click();
  await page.getByRole("button", { name: "Вскрыл упаковку" }).first().click();
  await page.getByRole("button", { name: "Подтвердить" }).click();

  // Ищем новую партию, а не сверяем их число: если в исходной меньше упаковки (генератор шага 1 съел
  // случайное количество), она вскрывается целиком, обнуляется и пропадает из списка — число не меняется.
  const fresh = async () => (await milkBatches()).batches.filter((b) => !before.some((x) => x.id === b.id));
  await expect.poll(async () => (await fresh()).length, { timeout: 15_000 }).toBe(1);
  const child = (await fresh())[0]!;
  expect(child.parent_batch_id, "дочерняя партия ссылается на исходную").toBeTruthy();
  expect(child.opened_at, "у дочерней партии есть время вскрытия").toBeTruthy();
  const maxExpiry = Date.parse(clock.now) + 48 * 3600_000;
  expect(Date.parse(child.expires_at), "срок вскрытой — не позже now + 48 ч").toBeLessThanOrEqual(maxExpiry + 60_000);

  await page.goto(`/#/label/${child.id}`);
  await expect(page.locator("body")).toContainText(/годно до/i);
  await expect(page.locator("body")).toContainText(String(child.id));
});

test("11. «Сеть»: две точки сравнения на связи", async () => {
  const res = await stand.api.get("/api/network/summary");
  test.skip(res.status() !== 200, `hub недоступен (${res.status()}): сравнение поднимается ab-demo.sh setup`);
  const summary = await res.json();
  const ab = (summary.points as { group?: string; status: string; label?: string }[]).filter((p) => p.group === "ab");
  expect(ab.length, "две точки группы ab").toBe(2);
  expect(ab.every((p) => p.status === "ok"), "обе на связи").toBe(true);
  await page.goto("/#/network");
  await expect(page.locator("main")).toContainText("Живое сравнение");
  for (const p of ab) await expect(page.locator("main")).toContainText(p.label!);
});
