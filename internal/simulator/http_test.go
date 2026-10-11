package simulator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type fakeStand struct {
	mu        sync.Mutex
	now       time.Time
	deliv     []string
	orders    []string
	badAuth   int
	failFirst map[string]bool // id → ещё не отдавали 500
	rejectAll bool
}

func (f *fakeStand) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/clock", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.now = f.now.Add(6 * time.Hour) // каждый опрос — шаг стенда
		_ = json.NewEncoder(w).Encode(map[string]any{"now": f.now})
	})
	post := func(list *[]string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if r.Header.Get("Authorization") != "Bearer k1" {
				f.badAuth++
			}
			var b struct {
				ID string `json:"id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&b)
			if f.failFirst[b.ID] {
				f.failFirst[b.ID] = false
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			*list = append(*list, b.ID)
			if r.URL.Path == "/api/orders" && f.rejectAll {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"error":{"code":"insufficient_stock"}}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
		}
	}
	mux.HandleFunc("/api/deliveries", post(&f.deliv))
	mux.HandleFunc("/api/orders", post(&f.orders))
	return mux
}

func runAgainst(t *testing.T, f *fakeStand, seed int64) HTTPStats {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	f.now = time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	st, err := RunHTTP(context.Background(), HTTPConfig{
		BaseURL: srv.URL, Key: "k1", Seed: seed, StopAt: f.now.Add(48 * time.Hour),
		MaxRetries: 3,
		Sleep:      func(context.Context, time.Duration) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestRunHTTPSendsEventsWithBearer(t *testing.T) {
	f := &fakeStand{failFirst: map[string]bool{}}
	st := runAgainst(t, f, 1)
	if st.Deliveries == 0 || st.Orders == 0 {
		t.Fatalf("ничего не отправлено: %+v", st)
	}
	if f.badAuth != 0 {
		t.Fatalf("запросов без Bearer-ключа: %d", f.badAuth)
	}
	if st.Failed != 0 || st.Retries != 0 {
		t.Fatalf("лишние ошибки: %+v", st)
	}
}

func TestRunHTTPRetriesOn5xxWithSameID(t *testing.T) {
	f := &fakeStand{failFirst: map[string]bool{}}
	for d := int64(20000); d < 21000; d++ {
		f.failFirst[fmt.Sprintf("D-1-%d", d)] = true
	}
	st := runAgainst(t, f, 1)
	if st.Retries == 0 {
		t.Fatalf("повторов нет: %+v", st)
	}
	n := 0
	for _, id := range f.deliv {
		if id == f.deliv[0] {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("поставка с тем же id принята %d раз, ожидалась 1 (после повтора)", n)
	}
}

func TestRunHTTPInsufficientStockIsNotFatal(t *testing.T) {
	f := &fakeStand{failFirst: map[string]bool{}, rejectAll: true}
	st := runAgainst(t, f, 1)
	if st.Rejected == 0 || st.Orders != 0 {
		t.Fatalf("ожидались отклонённые заказы: %+v", st)
	}
	if st.Deliveries == 0 {
		t.Fatalf("поставки должны проходить: %+v", st)
	}
}

func TestRunHTTPUnauthorizedIsFatal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/clock" {
			_ = json.NewEncoder(w).Encode(map[string]any{"now": time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC).Add(time.Duration(time.Now().UnixNano()%1000+1) * time.Hour)})
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	_, err := RunHTTP(context.Background(), HTTPConfig{
		BaseURL: srv.URL, Seed: 1, StopAt: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		Sleep: func(context.Context, time.Duration) error { return nil },
	})
	if err == nil {
		t.Fatal("ожидалась ошибка 401")
	}
}
