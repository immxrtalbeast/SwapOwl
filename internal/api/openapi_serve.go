package api

import (
	"net/http"

	"umirhack/docs"
)

// docsPage — страница документации по /api/openapi.yaml (Redoc с CDN, версия зафиксирована).
const docsPage = `<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>API — мониторинг остатков</title>
</head>
<body>
<redoc spec-url="/api/openapi.yaml"></redoc>
<script src="https://cdn.jsdelivr.net/npm/redoc@2.1.5/bundles/redoc.standalone.js"></script>
</body>
</html>
`

// openAPIRoutes открывает описание API без входа: оно не содержит данных стенда.
func (rt *Router) openAPIRoutes() {
	rt.HandlePublic(http.MethodGet, "/api/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		_, _ = w.Write(docs.OpenAPIYAML)
	})
	rt.HandlePublic(http.MethodGet, "/api/docs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy",
			"default-src 'none'; script-src https://cdn.jsdelivr.net 'unsafe-inline'; style-src 'unsafe-inline' https://fonts.googleapis.com; "+
				"font-src https://fonts.gstatic.com data:; img-src 'self' data: https:; connect-src 'self'; worker-src blob:")
		_, _ = w.Write([]byte(docsPage))
	})
}
