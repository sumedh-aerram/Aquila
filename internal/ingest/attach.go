package ingest

import (
	"sort"
	"time"
)

const (
	// DefaultAttachList is the default GET /v1/attaches page size.
	DefaultAttachList = 20
	maxAttaches       = 50
)

// Attach is one OTEL service.name seen in the span store. It is not a tenant.
type Attach struct {
	Service  string    `json:"service"`
	Spans    int       `json:"spans"`
	LastSeen time.Time `json:"last_seen,omitempty"`
}

func clipAttaches(n int) int {
	if n <= 0 {
		return DefaultAttachList
	}
	if n > maxAttaches {
		return maxAttaches
	}
	return n
}

func attachesFrom(spans []Span, limit int) []Attach {
	type acc struct {
		n    int
		last time.Time
	}
	by := map[string]*acc{}
	for _, s := range spans {
		name := ClipService(s.ServiceName)
		if name == "" {
			continue
		}
		a := by[name]
		if a == nil {
			a = &acc{}
			by[name] = a
		}
		a.n++
		if s.StartTime.After(a.last) {
			a.last = s.StartTime
		}
	}
	out := make([]Attach, 0, len(by))
	for name, a := range by {
		out = append(out, Attach{Service: name, Spans: a.n, LastSeen: a.last.UTC()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastSeen.Equal(out[j].LastSeen) {
			return out[i].Service < out[j].Service
		}
		return out[i].LastSeen.After(out[j].LastSeen)
	})
	limit = clipAttaches(limit)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
