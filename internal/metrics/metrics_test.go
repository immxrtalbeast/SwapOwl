package metrics

import (
	"strings"
	"testing"
	"time"
)

func TestWrite(t *testing.T) {
	r := New()
	r.ObserveRequest("GET", "/api/x/{id}", 200, 30*time.Millisecond)
	r.ObserveRequest("GET", "/api/x/{id}", 200, 20*time.Second)
	r.ObserveRequest("POST", "unmatched", 404, time.Millisecond)
	r.Gauge("sse_subscribers", "h", func() float64 { return 3 })
	var sb strings.Builder
	if err := r.Write(&sb); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	for _, want := range []string{
		`http_requests_total{method="GET",route="/api/x/{id}",status="200"} 2`,
		`http_requests_total{method="POST",route="unmatched",status="404"} 1`,
		`http_request_duration_seconds_bucket{route="/api/x/{id}",le="0.05"} 1`,
		`http_request_duration_seconds_bucket{route="/api/x/{id}",le="10"} 1`,
		`http_request_duration_seconds_bucket{route="/api/x/{id}",le="+Inf"} 2`,
		`http_request_duration_seconds_count{route="/api/x/{id}"} 2`,
		"sse_subscribers 3\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("нет строки %q в:\n%s", want, out)
		}
	}
}
