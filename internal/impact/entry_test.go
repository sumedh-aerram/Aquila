package impact

import (
	"fmt"
	"slices"
	"testing"

	"github.com/sumedhaerram/aquila/internal/diff"
	"github.com/sumedhaerram/aquila/internal/graph"
	"github.com/sumedhaerram/aquila/internal/ingest"
	"github.com/sumedhaerram/aquila/internal/locate"
	"github.com/sumedhaerram/aquila/internal/source"
)

func TestAnalyzeEntryAncestry(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		parent string
		want   []string
	}{
		{
			name:   "root a ancestor",
			parent: "a",
			want:   []string{"POST /a"},
		},
		{
			name:   "root b ancestor",
			parent: "b",
			want:   []string{"POST /b"},
		},
		{
			name:   "missing parent",
			parent: "missing",
		},
		{
			name:   "orphaned routed ancestor",
			parent: "orphan",
		},
		{
			name:   "routed cycle",
			parent: "cycle",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := fixture(t)
			spans := []ingest.Span{
				{
					TraceID:    "trace",
					SpanID:     "a",
					Kind:       "server",
					HTTPMethod: "POST",
					HTTPRoute:  "/a",
				},
				{
					TraceID:    "trace",
					SpanID:     "b",
					Kind:       "server",
					HTTPMethod: "POST",
					HTTPRoute:  "/b",
				},
				{
					TraceID:      "trace",
					SpanID:       "orphan",
					ParentSpanID: "missing",
					Kind:         "server",
					HTTPMethod:   "POST",
					HTTPRoute:    "/orphan",
				},
				{
					TraceID:      "trace",
					SpanID:       "cycle",
					ParentSpanID: "cycle-child",
					Kind:         "server",
					HTTPMethod:   "POST",
					HTTPRoute:    "/cycle",
				},
				{
					TraceID:      "trace",
					SpanID:       "cycle-child",
					ParentSpanID: "cycle",
					Kind:         "server",
					HTTPMethod:   "POST",
					HTTPRoute:    "/cycle-child",
				},
				{
					TraceID:      "trace",
					SpanID:       "changed",
					ParentSpanID: tc.parent,
					ServiceName:  "payment",
					Kind:         "internal",
					CodeFunction: "chargeProcessor",
					CodeFile:     "internal/payment/handler.go",
				},
			}
			d := diff.Diff{Files: []diff.File{{
				Path:  "internal/payment/handler.go",
				Lines: []int{41},
			}}}
			rep := Analyze(g, d, locate.Bind(g, spans), graph.Build(spans))
			if got := RuntimeRoutes(rep); !slices.Equal(got, tc.want) {
				t.Fatalf("routes=%v, want %v", got, tc.want)
			}
			for _, f := range rep.Runtime {
				if f.Route != "" && (f.Provenance != graph.ProvenanceTraceEntry || f.EntrySpanID != tc.parent || f.Via != "") {
					t.Fatalf("attribution=%+v", f)
				}
			}
		})
	}
}

func TestAnalyzeCallerDepth(t *testing.T) {
	t.Parallel()
	snap := fixture(t).Snapshot()
	snap.Nodes = append(snap.Nodes, source.Node{
		ID:      "func:pay.Handler.handle",
		Kind:    source.KindFunction,
		Name:    "handle",
		File:    "internal/payment/handler.go",
		Line:    1,
		EndLine: 5,
	})
	snap.Edges = append(snap.Edges, source.Edge{
		From:       "func:pay.Handler.handle",
		To:         "func:pay.Handler.authorize",
		Kind:       source.EdgeCalls,
		Provenance: source.ProvenanceTypes,
	})
	g, err := source.FromSnapshot(snap)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		function   string
		wantRoute  bool
		provenance string
		via        string
	}{
		{
			name:       "direct binding",
			function:   "chargeProcessor",
			wantRoute:  true,
			provenance: graph.ProvenanceTraceEntry,
		},
		{
			name:       "immediate caller only",
			function:   "authorize",
			wantRoute:  true,
			provenance: ProvenanceTraceEntryCaller,
			via:        "func:pay.Handler.authorize -> func:pay.Handler.chargeProcessor",
		},
		{
			name:     "depth two caller",
			function: "handle",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spans := []ingest.Span{
				{
					TraceID:    "trace",
					SpanID:     "entry",
					Kind:       "server",
					HTTPMethod: "POST",
					HTTPRoute:  "/checkout",
				},
				{
					TraceID:      "trace",
					SpanID:       "binding",
					ParentSpanID: "entry",
					Kind:         "internal",
					CodeFunction: tc.function,
					CodeFile:     "internal/payment/handler.go",
				},
			}
			d := diff.Diff{Files: []diff.File{{
				Path:  "internal/payment/handler.go",
				Lines: []int{41},
			}}}
			rep := Analyze(g, d, locate.Bind(g, spans), graph.Build(spans))
			if got := len(RuntimeRoutes(rep)) > 0; got != tc.wantRoute {
				t.Fatalf("runtime=%+v", rep.Runtime)
			}
			for _, f := range rep.Runtime {
				if f.Route != "" && (f.Provenance != tc.provenance || f.Via != tc.via) {
					t.Fatalf("attribution=%+v", f)
				}
			}
		})
	}
}

func TestAnalyzeDeduplicatesBeforeFindingCap(t *testing.T) {
	t.Parallel()
	g := fixture(t)
	spans := []ingest.Span{
		{
			TraceID:    "trace",
			SpanID:     "a",
			Kind:       "server",
			HTTPMethod: "POST",
			HTTPRoute:  "/a",
		},
		{
			TraceID:    "trace",
			SpanID:     "b",
			Kind:       "server",
			HTTPMethod: "POST",
			HTTPRoute:  "/b",
		},
	}
	var loc locate.Snapshot
	for i := 0; i < 2*maxFindings+1; i++ {
		parent := "a"
		if i == 2*maxFindings {
			parent = "b"
		}
		id := fmt.Sprintf("binding-%04d", i)
		spans = append(spans, ingest.Span{
			TraceID:      "trace",
			SpanID:       id,
			ParentSpanID: parent,
			Kind:         "internal",
		})
		loc.Bindings = append(loc.Bindings, locate.Binding{
			TraceID:     "trace",
			SpanID:      id,
			SourceID:    "func:pay.Handler.chargeProcessor",
			SourceName:  "chargeProcessor",
			ServiceName: "payment",
		})
	}
	d := diff.Diff{Files: []diff.File{{
		Path:  "internal/payment/handler.go",
		Lines: []int{41},
	}}}
	rep := Analyze(g, d, loc, graph.Build(spans))
	if got := RuntimeRoutes(rep); !slices.Equal(got, []string{"POST /a", "POST /b"}) {
		t.Fatalf("routes=%v", got)
	}
	count := 0
	for _, f := range rep.Runtime {
		if f.Route != "" {
			count++
		}
	}
	if !slices.Equal(rep.RouteTraceCounts, []RouteTraceCount{{Route: "POST /a", Traces: 1}, {Route: "POST /b", Traces: 1}}) {
		t.Fatalf("counts=%v", rep.RouteTraceCounts)
	}
	if count != 2 {
		t.Fatalf("route findings=%d, want 2", count)
	}
}

func TestAnalyzeUsesEntryInsteadOfNestedRoute(t *testing.T) {
	t.Parallel()
	g := fixture(t)
	spans := []ingest.Span{
		{
			TraceID:    "trace",
			SpanID:     "entry",
			Kind:       "server",
			HTTPMethod: "POST",
			HTTPRoute:  "/checkout",
		},
		{
			TraceID:      "trace",
			SpanID:       "changed",
			ParentSpanID: "entry",
			Kind:         "server",
			HTTPMethod:   "POST",
			HTTPRoute:    "/authorize",
			CodeFunction: "chargeProcessor",
			CodeFile:     "internal/payment/handler.go",
		},
	}
	d := diff.Diff{Files: []diff.File{{
		Path:  "internal/payment/handler.go",
		Lines: []int{41},
	}}}
	rep := Analyze(g, d, locate.Bind(g, spans), graph.Build(spans))
	if got := RuntimeRoutes(rep); !slices.Equal(got, []string{"POST /checkout"}) {
		t.Fatalf("routes=%v", got)
	}
}

func TestAnalyzeDirectCallerPreservesBothAttributions(t *testing.T) {
	t.Parallel()
	g := fixture(t)
	spans := []ingest.Span{
		{
			TraceID:    "trace",
			SpanID:     "entry",
			Kind:       "server",
			HTTPMethod: "POST",
			HTTPRoute:  "/checkout",
		},
		{
			TraceID:      "trace",
			SpanID:       "binding",
			ParentSpanID: "entry",
			Kind:         "internal",
			CodeFunction: "authorize",
			CodeFile:     "internal/payment/handler.go",
		},
	}
	d := diff.Diff{Files: []diff.File{{
		Path:  "internal/payment/handler.go",
		Lines: []int{11, 41},
	}}}
	rep := Analyze(g, d, locate.Bind(g, spans), graph.Build(spans))
	seen := make(map[string]string)
	for _, f := range rep.Runtime {
		if f.Route != "" {
			seen[f.Provenance] = f.Via
		}
	}
	observed, ok := seen[graph.ProvenanceTraceEntry]
	if !ok || observed != "" || seen[ProvenanceTraceEntryCaller] != "func:pay.Handler.authorize -> func:pay.Handler.chargeProcessor" {
		t.Fatalf("attributions=%v", seen)
	}
}

func TestAnalyzeCountsDistinctTracesAcrossBindings(t *testing.T) {
	t.Parallel()
	var spans []ingest.Span
	for _, trace := range []string{"one", "two"} {
		spans = append(spans, ingest.Span{TraceID: trace, SpanID: "entry", Kind: "server", HTTPMethod: "POST", HTTPRoute: "/checkout"})
		for _, name := range []string{"authorize", "chargeProcessor"} {
			for i := range 3 {
				spans = append(spans, ingest.Span{
					TraceID:      trace,
					SpanID:       fmt.Sprintf("%s-%d", name, i),
					ParentSpanID: "entry",
					Kind:         "internal",
					CodeFunction: name,
					CodeFile:     "internal/payment/handler.go",
				})
			}
		}
	}
	g := fixture(t)
	d := diff.Diff{Files: []diff.File{{Path: "internal/payment/handler.go", Lines: []int{41}}}}
	rep := Analyze(g, d, locate.Bind(g, spans), graph.Build(spans))
	if !slices.Equal(rep.RouteTraceCounts, []RouteTraceCount{{Route: "POST /checkout", Traces: 2}}) {
		t.Fatalf("counts=%v", rep.RouteTraceCounts)
	}
	if len(rep.Runtime) != 2 {
		t.Fatalf("runtime=%v", rep.Runtime)
	}
}
