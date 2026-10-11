# e2e

Демо-сценарий `.ai/demo.md` на Playwright против запущенного стенда.

- `tests/00-seed.spec.ts` — условия сида из `.ai/demo.md` сразу после сброса. Проверки мягкие: один прогон перечисляет все расхождения сида со сценарием.
- `tests/10-demo.spec.ts` — шаги 0–9 по порядку на одной странице. Страница ни разу не перезагружается: изменения должны приходить по SSE.

Шаг 8 проверяется через API: кнопки «Принять предложенное» на экране «Поставки» пока нет. Шаг 9 пропускается, пока нет `GET /api/analytics/bench` или результата bench (правило `.ai/solution.md`, 2.7).

## Запуск

```sh
cd e2e
npm ci
npx playwright install chromium
E2E_BASE_URL=https://<стенд> E2E_DEMO_TOKEN=<DEMO_TOKEN> E2E_LATE_EVENTS_HOURS=<LATE_EVENTS_HOURS стенда> npm test
```

| Переменная | По умолчанию | Смысл |
|---|---|---|
| `E2E_BASE_URL` | `http://localhost:5173` | Адрес фронта; API на том же адресе (`/api`, `/events`) |
| `E2E_DEMO_TOKEN` | — | `DEMO_TOKEN` стенда: сброс, скорость, прыжок |
| `E2E_LATE_EVENTS_HOURS` | `2` | Окно опоздания стенда (`.ai/solution.md`, 2.4а). Шаг 6 прыгает на +8 ч, а при окне шире 2 ч — дальше, за `expires_at + окно` |

Сценарий сбрасывает стенд и двигает его время — не запускайте его на стенде, которым в это время пользуются.

## Локальный стенд

```sh
# PostgreSQL с базой e2e, затем из корня репозитория:
go build -o /tmp/api ./cmd/api
DATABASE_URL=postgres://…/e2e PORT=8080 DEMO_MODE=true DEMO_TOKEN=e2e-token \
  CLOCK_MODE=simulated SEED_ON_EMPTY=true AUTH_REQUIRED=false /tmp/api &
(cd frontend && npx vite --port 5173) &
cd e2e && E2E_DEMO_TOKEN=e2e-token npm test
```

Отчёт — `npx playwright show-report`; при падении в `test-results/` лежат скриншот и трасса.
