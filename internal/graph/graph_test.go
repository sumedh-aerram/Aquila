package graph

import (
	"strings"
	"testing"
	"time"

	"github.com/sumedhaerram/aquila/internal/ingest"
)

func TestBuildEmpty(t *testing.T) {
	t.Parallel()
	snap := Build(nil)
	if snap.Services == nil || snap.Edges == nil || snap.Paths == nil {
		t.Fatal("nil slices")
	}
	if snap.SpanCount != 0 || snap.TraceCount != 0 {
		t.Fatalf("%+v", snap)
	}
}

func TestBuildObservedCrossServiceEdgeAndPath(t *testing.T) {
	t.Parallel()
	start := time.Unix(1, 0).UTC()
	spans := []ingest.Span{
		{TraceID: "aa", SpanID: "01", ServiceName: "gateway", Name: "POST /checkout", Kind: "server", StartTime: start},
		{TraceID: "aa", SpanID: "02", ParentSpanID: "01", ServiceName: "checkout", Name: "POST /checkout", Kind: "server", StartTime: start.Add(time.Millisecond)},
		{TraceID: "aa", SpanID: "03", ParentSpanID: "02", ServiceName: "payment", Name: "POST /authorize", Kind: "server", StartTime: start.Add(2 * time.Millisecond)},
	}
	snap := Build(spans)
	if snap.TraceCount != 1 || snap.SpanCount != 3 {
		t.Fatalf("counts %+v", snap)
	}
	if len(snap.Edges) != 2 {
		t.Fatalf("edges=%+v", snap.Edges)
	}
	if snap.Edges[0].Provenance != ProvenanceObservedParent {
		t.Fatalf("provenance=%q", snap.Edges[0].Provenance)
	}
	got := map[string]int{}
	for _, e := range snap.Edges {
		got[e.From+"->"+e.To] = e.Count
	}
	if got["gateway->checkout"] != 1 || got["checkout->payment"] != 1 {
		t.Fatalf("edges=%v", got)
	}
	if len(snap.Paths) != 1 {
		t.Fatalf("paths=%+v", snap.Paths)
	}
	want := []string{"gateway", "checkout", "payment"}
	if strings.Join(snap.Paths[0].Services, "/") != strings.Join(want, "/") || snap.Paths[0].Traces != 1 {
		t.Fatalf("path=%+v", snap.Paths[0])
	}
}

func TestBuildDoesNotInventMissingParent(t *testing.T) {
	t.Parallel()
	spans := []ingest.Span{
		{TraceID: "bb", SpanID: "10", ParentSpanID: "missing", ServiceName: "payment", Name: "POST /authorize"},
		{TraceID: "bb", SpanID: "11", ParentSpanID: "10", ServiceName: "processor", Name: "POST /charge"},
	}
	snap := Build(spans)
	if len(snap.Edges) != 1 || snap.Edges[0].From != "payment" || snap.Edges[0].To != "processor" {
		t.Fatalf("edges=%+v", snap.Edges)
	}
	if len(snap.Paths) != 1 || strings.Join(snap.Paths[0].Services, "/") != "payment/processor" {
		t.Fatalf("paths=%+v", snap.Paths)
	}
}

func TestBuildSameServiceIsNotAnEdge(t *testing.T) {
	t.Parallel()
	spans := []ingest.Span{
		{TraceID: "cc", SpanID: "20", ServiceName: "checkout", Kind: "server"},
		{TraceID: "cc", SpanID: "21", ParentSpanID: "20", ServiceName: "checkout", Kind: "internal"},
	}
	snap := Build(spans)
	if len(snap.Edges) != 0 {
		t.Fatalf("edges=%+v", snap.Edges)
	}
	if len(snap.Paths) != 1 || strings.Join(snap.Paths[0].Services, "/") != "checkout" {
		t.Fatalf("paths=%+v", snap.Paths)
	}
}

func TestBuildFanOutPaths(t *testing.T) {
	t.Parallel()
	spans := []ingest.Span{
		{TraceID: "dd", SpanID: "30", ServiceName: "checkout"},
		{TraceID: "dd", SpanID: "31", ParentSpanID: "30", ServiceName: "users"},
		{TraceID: "dd", SpanID: "32", ParentSpanID: "30", ServiceName: "inventory"},
	}
	snap := Build(spans)
	if len(snap.Edges) != 2 {
		t.Fatalf("edges=%+v", snap.Edges)
	}
	if len(snap.Paths) != 2 {
		t.Fatalf("paths=%+v", snap.Paths)
	}
	seen := map[string]bool{}
	for _, p := range snap.Paths {
		seen[strings.Join(p.Services, "/")] = true
		if p.Traces != 1 {
			t.Fatalf("traces=%d", p.Traces)
		}
	}
	if !seen["checkout/users"] || !seen["checkout/inventory"] {
		t.Fatalf("seen=%v", seen)
	}
}

func TestBuildCycleDoesNotHang(t *testing.T) {
	t.Parallel()
	spans := []ingest.Span{
		{TraceID: "ee", SpanID: "40", ParentSpanID: "41", ServiceName: "a"},
		{TraceID: "ee", SpanID: "41", ParentSpanID: "40", ServiceName: "b"},
	}
	snap := Build(spans)
	if snap.TraceCount != 1 {
		t.Fatalf("%+v", snap)
	}
}
