package ingest

import (
	"context"
	"sort"
	"sync"
	"time"
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
func (m *Memory) ListTraceWindow(_ context.Context, maxTraces int) ([]Span, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	type traceInfo struct {
		id   string
		last time.Time
	}
	byTrace := make(map[string][]Span)
	latest := make(map[string]time.Time)
	for _, k := range m.order {
		s := m.spans[k]
		byTrace[s.TraceID] = append(byTrace[s.TraceID], s)
		if t, ok := latest[s.TraceID]; !ok || s.StartTime.After(t) {
			latest[s.TraceID] = s.StartTime
		}
	}
	infos := make([]traceInfo, 0, len(byTrace))
	for id, t := range latest {
		infos = append(infos, traceInfo{id: id, last: t})
	}
	sort.Slice(infos, func(i, j int) bool {
		if infos[i].last.Equal(infos[j].last) {
			return infos[i].id > infos[j].id
		}
		return infos[i].last.After(infos[j].last)
	})
	n := normalizeTraceWindow(maxTraces)
	if n > len(infos) {
		n = len(infos)
	}
	out := make([]Span, 0)
	for _, info := range infos[:n] {
		spans := byTrace[info.id]
		sort.Slice(spans, func(i, j int) bool {
			if spans[i].StartTime.Equal(spans[j].StartTime) {
				return spans[i].SpanID < spans[j].SpanID
			}
			return spans[i].StartTime.Before(spans[j].StartTime)
		})
		out = append(out, spans...)
		if len(out) >= maxWindowSpans {
			return out[:maxWindowSpans], nil
		}
	}
	return out, nil
}

var _ Store = (*Memory)(nil)
