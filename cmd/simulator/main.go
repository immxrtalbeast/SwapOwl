// Command simulator — генератор поставок и заказов, сравнение алгоритмов горячих предложений.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"umirhack/internal/simulator"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "simulator:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) > 0 && args[0] == "run" {
		return runHTTP(args[1:], out)
	}
	if len(args) == 0 || args[0] != "bench" {
		return fmt.Errorf("использование: simulator bench [-seed N] [-n N] [-days N] [-u 1.5] [-u-extra 1.2,2.0] [-json]\n             simulator run -api URL [-key KEY] [-seed N] [-speed X] [-for 10m]")
	}
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	seed := fs.Int64("seed", 1, "первый seed")
	n := fs.Int("n", 1, "число seed подряд")
	days := fs.Int("days", 7, "длина прогона, суток")
	u := fs.Float64("u", 1.5, "основная точка u (модель M2)")
	extra := fs.String("u-extra", "", "дополнительные значения u через запятую (считаются последними)")
	asJSON := fs.Bool("json", false, "вывод в JSON")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	us := []float64{*u}
	for _, s := range strings.Split(*extra, ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return fmt.Errorf("u-extra: %w", err)
		}
		us = append(us, v)
	}
	rep, err := simulator.Bench(*seed, *n, *days, us)
	if err != nil {
		return err
	}
	if *asJSON {
		return simulator.WriteJSON(out, rep)
	}
	simulator.WriteText(out, rep)
	return nil
}

func runHTTP(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	api := fs.String("api", "http://localhost:8080", "адрес API")
	key := fs.String("key", os.Getenv("SIMULATOR_API_KEY"), "ключ API (Authorization: Bearer); по умолчанию из SIMULATOR_API_KEY")
	seed := fs.Int64("seed", 1, "seed генератора")
	speed := fs.Float64("speed", 1, "множитель темпа относительно часов стенда")
	dur := fs.Duration("for", 0, "сколько работать в реальном времени; 0 — до остановки (Ctrl+C)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if *dur > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *dur)
		defer cancel()
	}
	st, err := simulator.RunHTTP(ctx, simulator.HTTPConfig{BaseURL: *api, Key: *key, Seed: *seed, Speed: *speed, Log: out})
	fmt.Fprintf(out, "поставок: %d, заказов: %d, отклонено (нет остатков): %d, прочих ошибок: %d, повторов: %d\n",
		st.Deliveries, st.Orders, st.Rejected, st.Failed, st.Retries)
	return err
}
