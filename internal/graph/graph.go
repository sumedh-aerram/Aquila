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
	maxPaths                 = 100
	maxServices              = 500
	maxEdges                 = 2000
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

// Snapshot is a derived view of stored spans. It is not a telemetry backend.
type Snapshot struct {
	Services   []Service `json:"services"`
	Edges      []Edge    `json:"edges"`
	Paths      []Path    `json:"paths"`
	SpanCount  int       `json:"span_count"`
	TraceCount int       `json:"trace_count"`
}

type spanNode struct {
	span     ingest.Span
	children []string
}

// Build derives services, cross-service edges, and root-to-leaf paths.
// An edge exists only when the parent span is in the same batch; missing
// parents are omitted rather than inferred.
func Build(spans []ingest.Span) Snapshot {
	out := Snapshot{
		Services: []Service{},
		Edges:    []Edge{},
		Paths:    []Path{},
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
