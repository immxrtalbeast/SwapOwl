package simulator

import (
	"encoding/json"
	"fmt"
	"io"
)

// WriteJSON выводит отчёт в JSON.
func WriteJSON(w io.Writer, rep Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rep)
}

func rub(kop int64) string { return fmt.Sprintf("%.2f", float64(kop)/100) }

// WriteText выводит таблицу основной точки по сценариям, проверку знака и вердикт.
func WriteText(w io.Writer, rep Report) {
	fmt.Fprintf(w, "seed: %d на клетку, дней: %d. Списания по просрочке, ₽ (доля от поступившего)\n", rep.Seeds, rep.Days)
	for _, c := range rep.Cells {
		kind := "знак"
		if c.Main {
			kind = "основная точка"
		}
		fmt.Fprintf(w, "\n%s · %s · u = %.2f (%s)\n", c.Title, c.Model, c.U, kind)
		for _, a := range Algos {
			g := c.Algos[a]
			fmt.Fprintf(w, "  %-4s %12s (%.1f%%), отказов %d из %d\n", a, rub(g.Waste), g.WasteShare()*100, g.Rejected, g.Orders)
		}
		fmt.Fprintf(w, "  Bg−R: среднее %s ₽, 95%% интервал [%s; %s], знак: %s\n",
			rub(int64(c.DiffBgR.Mean)), rub(int64(c.DiffBgR.Lo)), rub(int64(c.DiffBgR.Hi)), c.SignBgVsR)
	}
	v := rep.Verdict
	fmt.Fprintf(w, "\nПобедитель: %s (%s)\n", v.Main, v.Reason)
	for _, s := range v.Scenarios {
		fmt.Fprintf(w, "  %-18s %-4s %s\n", s.Scenario, s.Outcome, s.Why)
	}
	if v.Claims {
		fmt.Fprintf(w, "Снижение списаний основного алгоритма: к случайным %.1f%%, к ближайшему сроку %.1f%%\n",
			v.Analytics.WasteReductionVsRandomPct, v.Analytics.WasteReductionVsNearestPct)
	} else {
		fmt.Fprintln(w, "Утверждений о сравнении нет.")
	}
}
