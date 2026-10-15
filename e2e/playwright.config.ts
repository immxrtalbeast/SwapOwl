import { defineConfig, devices } from "@playwright/test";

// Демо-сценарий docs/demo-script.md против запущенного стенда.
// E2E_BASE_URL — адрес фронта (API на том же адресе: /api, /events); E2E_DEMO_TOKEN — DEMO_TOKEN стенда.
export default defineConfig({
  testDir: "./tests",
  fullyParallel: false,
  workers: 1, // сценарий меняет общий стенд: сброс, время, заказы
  retries: 0,
  timeout: 90_000,
  expect: { timeout: 15_000 },
  reporter: [["list"], ["html", { open: "never" }]],
  use: {
    baseURL: process.env.E2E_BASE_URL ?? "http://localhost:5173",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    locale: "ru-RU",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
