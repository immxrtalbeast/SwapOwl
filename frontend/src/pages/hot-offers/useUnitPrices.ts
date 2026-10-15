import { useCallback, useState } from "react";
import { api } from "../../api/client";
import { unitPricesFromForecast } from "../../api/today";
import type { UnitPrices } from "../../api/today";
import { useLive } from "../../app/useLive";

/** Цены из прогноза списаний; пока не пришли или маршрута нет — пусто (суммы «не спишем» не показываются). */
export function useUnitPrices(): UnitPrices {
  const [prices, setPrices] = useState<UnitPrices>({});
  const load = useCallback(async () => {
    try {
      setPrices(unitPricesFromForecast(await api.forecastWriteoffs()));
    } catch {
      /* без цен карточка работает, только без рублей */
    }
  }, []);
  useLive(load, ["stock.changed", "clock.changed", "stand.reset"]);
  return prices;
}
