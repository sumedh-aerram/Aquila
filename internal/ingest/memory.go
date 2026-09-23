package ingest

import (
	"context"
	"sync"
)

// Memory is an in-process Store for tests.
type Memory struct {
	mu    sync.Mutex
	spans map[string]Span
	order []string
}

// NewMemory returns an empty Memory store.
func NewMemory() *Memory {
	return &Memory{spans: make(map[string]Span)}
}

func spanKey(s Span) string {
	return s.TraceID + "/" + s.SpanID
}

// UpsertSpans implements Store.
func (m *Memory) UpsertSpans(_ context.Context, spans []Span) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.spans == nil {
		m.spans = make(map[string]Span)
	}
	for _, s := range spans {
		k := spanKey(s)
		if _, ok := m.spans[k]; !ok {
			m.order = append(m.order, k)
		}
		m.spans[k] = s
	}
	return nil
}

// ListSpans implements Store.
func (m *Memory) ListSpans(_ context.Context, q ListQuery) ([]Span, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	limit := normalizeLimit(q.Limit)
	out := make([]Span, 0, limit)
	for i := len(m.order) - 1; i >= 0; i-- {
		s := m.spans[m.order[i]]
		if q.TraceID != "" && s.TraceID != q.TraceID {
			continue
		}
		if q.Service != "" && s.ServiceName != q.Service {
			continue
		}
		out = append(out, s)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// ListTraceWindow implements Store.
func (m *Memory) ListTraceWindow(_ context.Context, maxTraces int, service string) ([]Span, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	all := make([]Span, 0, len(m.order))
	for _, k := range m.order {
		all = append(all, m.spans[k])
	}
	return SelectWindow(all, maxTraces, service), nil
}

var _ Store = (*Memory)(nil)
