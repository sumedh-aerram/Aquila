package ingest

import (
	"sort"
	"strings"
	"time"
)

type traceInfo struct {
	id    string
	last  time.Time
	spans []Span
}

func probeRoute(s Span) bool {
	route := strings.ToLower(strings.TrimSpace(s.HTTPRoute))
	name := strings.ToLower(strings.TrimSpace(s.Name))
	switch route {
	case "/healthz", "/livez", "/readyz", "/health":
		return true
	}
	switch name {
	case "get /healthz", "head /healthz", "/healthz", "get /readyz", "get /livez":
		return true
	}
	return false
}

func probeOnly(spans []Span) bool {
	if len(spans) == 0 {
		return true
	}
	for _, s := range spans {
		if !probeRoute(s) {
			return false
		}
	}
	return true
}

func groupTraces(spans []Span) []traceInfo {
	by := make(map[string]*traceInfo)
	for _, s := range spans {
		if s.TraceID == "" {
			continue
		}
		t, ok := by[s.TraceID]
		if !ok {
			t = &traceInfo{id: s.TraceID, last: s.StartTime}
			by[s.TraceID] = t
		}
		t.spans = append(t.spans, s)
		if s.StartTime.After(t.last) {
			t.last = s.StartTime
		}
	}
	out := make([]traceInfo, 0, len(by))
	for _, t := range by {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].last.Equal(out[j].last) {
			return out[i].id > out[j].id
		}
		return out[i].last.After(out[j].last)
	})
	return out
}

func sortTraceSpans(spans []Span) {
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].StartTime.Equal(spans[j].StartTime) {
			return spans[i].SpanID < spans[j].SpanID
		}
		return spans[i].StartTime.Before(spans[j].StartTime)
	})
}

// SelectWindow returns spans from the newest non-probe traces, newest first by trace.
func SelectWindow(spans []Span, maxTraces int) []Span {
	n := normalizeTraceWindow(maxTraces)
	infos := groupTraces(spans)
	out := make([]Span, 0)
	kept := 0
	for _, info := range infos {
		if probeOnly(info.spans) {
			continue
		}
		sortTraceSpans(info.spans)
		out = append(out, info.spans...)
		kept++
		if kept >= n || len(out) >= maxWindowSpans {
			break
		}
	}
	if len(out) > maxWindowSpans {
		return out[:maxWindowSpans]
	}
	return out
}
