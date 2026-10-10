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

// WriteText выводит отчёт в виде таблиц по seed и итога.
func WriteText(w io.Writer, rep Report) {
	for _, pt := range rep.Points {
		kind := "дополнительная точка"
		if pt.Main {
			kind = "основная точка"
		}
		fmt.Fprintf(w, "u = %.2f (%s), модель M2, дней: %d\n", pt.U, kind, rep.Days)
		fmt.Fprintf(w, "списания по просрочке, ₽ (доля от поступившего, %%):\n")
		fmt.Fprintf(w, "%-6s %-18s %-18s %-18s %s\n", "seed", "C0", "R", "Bg", "Bg−R")
		for _, r := range pt.Rows {
			fmt.Fprintf(w, "%-6d %-18s %-18s %-18s %s\n", r.Seed, cell(r.C0), cell(r.R), cell(r.Bg), rub(r.DiffBgR))
		}
		fmt.Fprintf(w, "итого: C0 %s, R %s, Bg %s\n", rub(pt.TotalC0), rub(pt.TotalR), rub(pt.TotalBg))
		fmt.Fprintf(w, "отказов в заказах (из %d): C0 %d, R %d, Bg %d\n", pt.OrdersC0, pt.RejectsC0, pt.RejectsR, pt.RejectsBg)
		fmt.Fprintf(w, "снижение списаний к C0: R %.1f%%, Bg %.1f%%\n", pt.ReductionR*100, pt.ReductionB*100)
		fmt.Fprintf(w, "Bg−R: среднее %s ₽, 95%% интервал [%s; %s], знак: %s\n\n",
			rub(int64(pt.DiffBgR.Mean)), rub(int64(pt.DiffBgR.Lo)), rub(int64(pt.DiffBgR.Hi)), pt.SignBgVsR)
	}
	fmt.Fprintln(w, "Основной алгоритм здесь не выбирается (отдельный тикет этапа 3).")
}

func cell(r Result) string {
	return fmt.Sprintf("%s (%.1f%%)", rub(r.WriteoffCost), r.WriteoffShare()*100)
}
