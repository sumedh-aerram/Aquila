package observability

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
)

// Metrics is process-local Prometheus text for Aquila itself, not app traces.
type Metrics struct {
	ingestSpans    atomic.Int64
	ingestRejected atomic.Int64
	http2xx        atomic.Int64
	httpOther      atomic.Int64
}

// NewMetrics returns zeroed counters.
func NewMetrics() *Metrics {
	return &Metrics{}
}

// AddIngest records accepted and rejected span counts from one OTLP batch.
func (m *Metrics) AddIngest(accepted, rejected int) {
	if m == nil {
		return
	}
	if accepted > 0 {
		m.ingestSpans.Add(int64(accepted))
	}
	if rejected > 0 {
		m.ingestRejected.Add(int64(rejected))
	}
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

// Wrap counts HTTP statuses. /metrics is included.
func (m *Metrics) Wrap(next http.Handler) http.Handler {
	if m == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(sw, r)
		if sw.code >= 200 && sw.code <= 299 {
			m.http2xx.Add(1)
		} else {
			m.httpOther.Add(1)
		}
	})
}

// ServeHTTP writes Prometheus text exposition.
func (m *Metrics) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	if m == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	var b strings.Builder
	writeGauge(&b, "aquila_up", "Aquila process is serving", 1)
	writeCounter(&b, "aquila_ingest_spans_total", "Normalized spans stored", m.ingestSpans.Load())
	writeCounter(&b, "aquila_ingest_rejected_spans_total", "Spans dropped during normalize", m.ingestRejected.Load())
	writeCounter(&b, "aquila_http_requests_2xx_total", "HTTP responses 2xx", m.http2xx.Load())
	writeCounter(&b, "aquila_http_requests_other_total", "HTTP responses not 2xx", m.httpOther.Load())
	_, _ = fmt.Fprint(w, b.String())
}

func writeCounter(b *strings.Builder, name, help string, v int64) {
	writeMetric(b, name, help, "counter", v)
}

func writeGauge(b *strings.Builder, name, help string, v int64) {
	writeMetric(b, name, help, "gauge", v)
}

func writeMetric(b *strings.Builder, name, help, kind string, v int64) {
	b.WriteString("# HELP ")
	b.WriteString(name)
	b.WriteByte(' ')
	b.WriteString(help)
	b.WriteByte('\n')
	b.WriteString("# TYPE ")
	b.WriteString(name)
	b.WriteByte(' ')
	b.WriteString(kind)
	b.WriteByte('\n')
	b.WriteString(name)
	b.WriteByte(' ')
	b.WriteString(strconv.FormatInt(v, 10))
	b.WriteByte('\n')
}
