// Package docs встраивает в бинарь машиночитаемые результаты, которые показывает продукт.
package docs

import _ "embed"

// BenchJSON — результат `cmd/simulator bench` (solution.md, 2.7, «Граница»).
//
//go:embed bench.json
var BenchJSON []byte

// OpenAPIYAML — описание API; отдаётся как GET /api/openapi.yaml.
//
//go:embed openapi.yaml
var OpenAPIYAML []byte
