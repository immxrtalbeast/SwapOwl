package hub

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

// Server — HTTP API hub. Все маршруты, кроме /healthz, требуют Bearer HUB_TOKEN.
type Server struct {
	c     *Collector
	token string
}

// NewServer создаёт обработчик. Пустой token недопустим: вызывающий проверяет его при старте.
func NewServer(c *Collector, token string) http.Handler {
	s := &Server{c: c, token: token}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/network/summary", s.auth(s.summary))
	mux.HandleFunc("GET /api/network/points/{id}", s.auth(s.point))
	return mux
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || s.token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

func (s *Server) summary(w http.ResponseWriter, _ *http.Request) {
	states := s.c.Snapshot()
	for i := range states { // в сводке ленты не нужны
		states[i].Alerts, states[i].StopList = nil, nil
	}
	writeJSON(w, http.StatusOK, map[string]any{"points": states, "totals": Summarize(s.c.Snapshot())})
}

func (s *Server) point(w http.ResponseWriter, r *http.Request) {
	st, ok := s.c.Point(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown_point"})
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
