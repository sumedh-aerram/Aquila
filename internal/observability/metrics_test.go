package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsIngestAndHTTP(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	m.AddIngest(3, 1)
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	rec := httptest.NewRecorder()
	m.Wrap(inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/spans", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d", rec.Code)
	}
	out := httptest.NewRecorder()
	m.ServeHTTP(out, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := out.Body.String()
	for _, want := range []string{
		"aquila_up 1",
		"aquila_ingest_spans_total 3",
		"aquila_ingest_rejected_spans_total 1",
		"aquila_http_requests_2xx_total 1",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in\n%s", want, body)
		}
	}
}
