package ingest

import (
	"context"
	"time"
)

const (
	maxString          = 512
	maxOTLPBytes       = 4 << 20
	maxSpansBatch      = 10000
	defaultList        = 50
	maxList            = 200
	defaultTraceWindow = 50
	maxTraceWindow     = 200
	maxWindowSpans     = 10000
)

// Span is the metadata Aquila retains from an OTLP span.
type Span struct {
	TraceID      string    `json:"trace_id"`
	SpanID       string    `json:"span_id"`
	ParentSpanID string    `json:"parent_span_id,omitempty"`
	ServiceName  string    `json:"service_name"`
	Name         string    `json:"name"`
	Kind         string    `json:"kind,omitempty"`
	StatusCode   string    `json:"status_code,omitempty"`
	HTTPMethod   string    `json:"http_method,omitempty"`
	HTTPRoute    string    `json:"http_route,omitempty"`
	HTTPStatus   int       `json:"http_status,omitempty"`
	CodeFunction string    `json:"code_function,omitempty"`
	CodeFile     string    `json:"code_file,omitempty"`
	StartTime    time.Time `json:"start_time"`
	DurationNS   int64     `json:"duration_ns"`
}

// ListQuery filters stored spans.
type ListQuery struct {
	TraceID string
	Service string
	Limit   int
}

// Store persists normalized spans.
type Store interface {
	UpsertSpans(ctx context.Context, spans []Span) error
	ListSpans(ctx context.Context, q ListQuery) ([]Span, error)
	ListTraceWindow(ctx context.Context, maxTraces int) ([]Span, error)
}

func clip(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n]
}

func normalizeLimit(n int) int {
	if n <= 0 {
		return defaultList
	}
	if n > maxList {
		return maxList
	}
	return n
}

func normalizeTraceWindow(n int) int {
	if n <= 0 {
		return defaultTraceWindow
	}
	if n > maxTraceWindow {
		return maxTraceWindow
	}
	return n
}
