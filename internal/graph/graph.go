package graph

import (
	"sort"
	"strings"

	"github.com/sumedhaerram/aquila/internal/ingest"
)

const (
	// ProvenanceObservedParent marks an edge or path derived from parent_span_id
	// links present in the same span batch.
	ProvenanceObservedParent = "observed_parent"
	// ProvenanceTraceEntry identifies an observed root-most HTTP server span.
	ProvenanceTraceEntry = "trace_entry"
	maxPaths             = 100
	maxServices          = 500
	maxEdges             = 2000
)

// Service is a process that emitted at least one span in the window.
type Service struct {
	Name      string `json:"name"`
	SpanCount int    `json:"span_count"`
}

// Edge is a directed runtime dependency between two services.
type Edge struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Count      int    `json:"count"`
	Provenance string `json:"provenance"`
}

// Path is a root-to-leaf sequence of services observed in one or more traces.
type Path struct {
	Services   []string `json:"services"`
	Traces     int      `json:"traces"`
	Provenance string   `json:"provenance"`
}

// EntryRoute is the root-most routed server span observed in a trace.
type EntryRoute struct {
	TraceID string `json:"trace_id"`
	SpanID  string `json:"span_id"`
	Method  string `json:"method"`
	Route   string `json:"route"`
}

// Snapshot is a derived view of stored spans. Build retains a private ancestry
// index for source joins; JSON projections do not contain that index.
type Snapshot struct {
	entryBySpan map[spanKey]EntryRoute
	EntryRoutes []EntryRoute `json:"entry_routes,omitempty"`
	Services    []Service    `json:"services"`
	Edges       []Edge       `json:"edges"`
	Paths       []Path       `json:"paths"`
	SpanCount   int          `json:"span_count"`
	TraceCount  int          `json:"trace_count"`
}

type spanKey struct {
	traceID string
	spanID  string
}

type spanNode struct {
	span     ingest.Span
	children []string
}

// Build derives services, cross-service edges, root-to-leaf paths and entry routes.
// An edge exists only when the parent span is in the same batch; missing
// parents are omitted rather than inferred.
func Build(spans []ingest.Span) Snapshot {
	out := Snapshot{
		entryBySpan: make(map[spanKey]EntryRoute),
		Services:    []Service{},
		Edges:       []Edge{},
		Paths:       []Path{},
	}
	if len(spans) == 0 {
		return out
	}

	traces := make(map[string]map[string]*spanNode)
	svcCount := make(map[string]int)
	for i := range spans {
		s := spans[i]
		if s.TraceID == "" || s.SpanID == "" {
			continue
		}
		out.SpanCount++
		if s.ServiceName != "" {
			svcCount[s.ServiceName]++
		}
		byID := traces[s.TraceID]
		if byID == nil {
			byID = make(map[string]*spanNode)
			traces[s.TraceID] = byID
		}
		if existing, ok := byID[s.SpanID]; ok {
			existing.span = s
			continue
		}
		byID[s.SpanID] = &spanNode{span: s}
	}
	out.TraceCount = len(traces)

	edgeCount := make(map[string]int)
	pathTraces := make(map[string]map[string]struct{})

	for traceID, byID := range traces {
		for id, entry := range traceEntries(traceID, byID) {
			if entry.SpanID == "" {
				continue
			}
			out.entryBySpan[spanKey{
				traceID: traceID,
				spanID:  id,
			}] = entry
			if entry.SpanID == id {
				out.EntryRoutes = append(out.EntryRoutes, entry)
			}
		}
		for id, node := range byID {
			pid := node.span.ParentSpanID
			if pid == "" {
				continue
			}
			parent, ok := byID[pid]
			if !ok {
				continue
			}
			parent.children = append(parent.children, id)
			from := parent.span.ServiceName
			to := node.span.ServiceName
			if from == "" || to == "" || from == to {
				continue
			}
			edgeCount[from+"\x00"+to]++
		}
		for id, node := range byID {
			pid := node.span.ParentSpanID
			if pid != "" {
				if _, ok := byID[pid]; ok {
					continue
				}
			}
			walkPaths(traceID, id, byID, nil, map[string]bool{}, pathTraces)
		}
	}

	sort.Slice(out.EntryRoutes, func(i, j int) bool {
		a, b := out.EntryRoutes[i], out.EntryRoutes[j]
		if a.TraceID != b.TraceID {
			return a.TraceID < b.TraceID
		}
		return a.SpanID < b.SpanID
	})
	out.Services = servicesFromCount(svcCount)
	out.Edges = edgesFromCount(edgeCount)
	out.Paths = pathsFromTraces(pathTraces)
	return out
}

func walkPaths(traceID, id string, byID map[string]*spanNode, services []string, visiting map[string]bool, pathTraces map[string]map[string]struct{}) {
	node, ok := byID[id]
	if !ok {
		return
	}
	// Malformed parent links can cycle; a per-walk set stops unbounded recursion.
	if visiting[id] {
		return
	}
	next := services
	if name := node.span.ServiceName; name != "" {
		if len(next) == 0 || next[len(next)-1] != name {
			next = append(append([]string{}, services...), name)
		}
	}
	if len(node.children) == 0 {
		if len(next) == 0 {
			return
		}
		key := strings.Join(next, "\x00")
		set := pathTraces[key]
		if set == nil {
			set = make(map[string]struct{})
			pathTraces[key] = set
		}
		set[traceID] = struct{}{}
		return
	}
	visiting[id] = true
	for _, child := range node.children {
		walkPaths(traceID, child, byID, next, visiting, pathTraces)
	}
	visiting[id] = false
}

func servicesFromCount(svcCount map[string]int) []Service {
	out := make([]Service, 0, len(svcCount))
	for name, n := range svcCount {
		out = append(out, Service{Name: name, SpanCount: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SpanCount == out[j].SpanCount {
			return out[i].Name < out[j].Name
		}
		return out[i].SpanCount > out[j].SpanCount
	})
	if len(out) > maxServices {
		out = out[:maxServices]
	}
	return out
}

func edgesFromCount(edgeCount map[string]int) []Edge {
	out := make([]Edge, 0, len(edgeCount))
	for key, n := range edgeCount {
		from, to, ok := strings.Cut(key, "\x00")
		if !ok {
			continue
		}
		out = append(out, Edge{
			From:       from,
			To:         to,
			Count:      n,
			Provenance: ProvenanceObservedParent,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			if out[i].From == out[j].From {
				return out[i].To < out[j].To
			}
			return out[i].From < out[j].From
		}
		return out[i].Count > out[j].Count
	})
	if len(out) > maxEdges {
		out = out[:maxEdges]
	}
	return out
}

func pathsFromTraces(pathTraces map[string]map[string]struct{}) []Path {
	out := make([]Path, 0, len(pathTraces))
	for key, set := range pathTraces {
		svcs := []string{}
		if key != "" {
			svcs = strings.Split(key, "\x00")
		}
		out = append(out, Path{
			Services:   svcs,
			Traces:     len(set),
			Provenance: ProvenanceObservedParent,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Traces == out[j].Traces {
			return strings.Join(out[i].Services, "/") < strings.Join(out[j].Services, "/")
		}
		return out[i].Traces > out[j].Traces
	})
	if len(out) > maxPaths {
		out = out[:maxPaths]
	}
	return out
}

// EntryForSpan returns the root-most routed server in a span's complete ancestry.
// Missing parents and cycles do not establish an entry route.
func (s Snapshot) EntryForSpan(traceID, spanID string) (EntryRoute, bool) {
	entry, ok := s.entryBySpan[spanKey{
		traceID: traceID,
		spanID:  spanID,
	}]
	return entry, ok
}

func traceEntries(traceID string, byID map[string]*spanNode) map[string]EntryRoute {
	type ancestry struct {
		complete bool
		entry    EntryRoute
	}
	resolved := make(map[string]ancestry, len(byID))
	for id := range byID {
		var chain []string
		seen := make(map[string]bool)
		cursor := id
		var parent ancestry
		for {
			if cached, ok := resolved[cursor]; ok {
				parent = cached
				break
			}
			node, ok := byID[cursor]
			if !ok || seen[cursor] {
				break
			}
			seen[cursor] = true
			chain = append(chain, cursor)
			if node.span.ParentSpanID == "" {
				parent.complete = true
				break
			}
			cursor = node.span.ParentSpanID
		}
		for i := len(chain) - 1; i >= 0; i-- {
			id := chain[i]
			node := byID[id]
			if parent.complete && parent.entry.SpanID == "" && routedServer(node.span) {
				parent.entry = EntryRoute{
					TraceID: traceID,
					SpanID:  id,
					Method:  node.span.HTTPMethod,
					Route:   node.span.HTTPRoute,
				}
			}
			resolved[id] = parent
		}
	}
	entries := make(map[string]EntryRoute, len(resolved))
	for id, parent := range resolved {
		if parent.complete && parent.entry.SpanID != "" {
			entries[id] = parent.entry
		}
	}
	return entries
}

func routedServer(s ingest.Span) bool {
	return strings.EqualFold(strings.TrimSpace(s.Kind), "server") && strings.TrimSpace(s.HTTPMethod) != "" && strings.TrimSpace(s.HTTPRoute) != ""
}
