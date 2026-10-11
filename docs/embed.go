// Package docs встраивает в бинарь машинночитаемые результаты, которые показывает API.
package docs

import _ "embed"

// BenchJSON — результат сравнения алгоритмов горячих предложений (bench.md), отдаётся GET /api/analytics/bench.
//
//go:embed bench.json
var BenchJSON []byte
