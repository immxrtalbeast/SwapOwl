import type { ForecastAccuracy } from "../../api/types";

/** Сколько суток проверяем: сутки для проверки точности мало (один день — не оценка), поэтому не меньше недели. */
export function accuracyDays(periodDays: number): number {
  return Math.min(30, Math.max(7, periodDays));
}

const pct = (v: number) => `${v.toLocaleString("ru-RU", { minimumFractionDigits: 0, maximumFractionDigits: 1 })} %`;

/** Главная фраза карточки: ошибка прогноза с обучением и без него; мало истории — честное «пока рано». */
export function accuracySentence(a: ForecastAccuracy): string {
  if (!a.enough_data) {
    return `Пока мало истории: проверено ${a.days_used} из ${a.days} суток, нужно хотя бы 3 с заказами.`;
  }
  const better = a.baseline_wape_pct - a.wape_pct;
  const tail =
    better > 0.05
      ? `Без обучения, по одному априору, ошибка была бы ${pct(a.baseline_wape_pct)} — движок точнее на ${pct(Math.round(better * 10) / 10)}.`
      : `По одному априору ошибка ${pct(a.baseline_wape_pct)} — обучение пока не добавило точности.`;
  return `Прогноз спроса за ${a.days_used} суток ошибался в среднем на ${pct(a.wape_pct)}. ${tail}`;
}

/** Блюда, где прогнозу есть куда расти: у которых есть факт, по убыванию ошибки (сервер уже отсортировал). */
export function worstDishes(a: ForecastAccuracy, n = 5): ForecastAccuracy["dishes"] {
  return a.dishes.filter((d) => d.actual_per_day > 0).slice(0, n);
}
