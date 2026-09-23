package locate

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sumedhaerram/aquila/internal/ingest"
	"github.com/sumedhaerram/aquila/internal/source"
)

func TestBindMapsExplicitPaymentAttrs(t *testing.T) {
	t.Parallel()
	g := fixtureGraph(t)
	snap := Bind(g, []ingest.Span{{
		TraceID:      "aa",
		SpanID:       "01",
		ServiceName:  "payment",
		Name:         "payment.Authorize",
		CodeFunction: "Handler.authorize",
		CodeFile:     "examples/shop/internal/payment/handler.go",
		HTTPMethod:   "POST",
		HTTPRoute:    "/authorize",
	}})
	if snap.Bound != 1 || len(snap.Bindings) != 1 {
		t.Fatalf("%+v", snap)
	}
	b := snap.Bindings[0]
	if b.SourceName != "authorize" || b.Provenance != ProvenanceCodeAttrs {
		t.Fatalf("%+v", b)
	}
	if b.HTTPMethod != "POST" || b.HTTPRoute != "/authorize" {
		t.Fatalf("route attrs=%+v", b)
	}
	if b.SourceID != "func:pay.Handler.authorize" {
		t.Fatalf("source_id=%q", b.SourceID)
	}
}

func TestBindNilGraphUnmapsSpans(t *testing.T) {
	t.Parallel()
	snap := Bind(nil, []ingest.Span{
		{TraceID: "aa", SpanID: "01", ServiceName: "reroute", Name: "GET /api/health"},
		{TraceID: "bb", SpanID: "02", CodeFunction: "health_check", CodeFile: "backend/server.py"},
	})
	if snap.SpanCount != 2 || snap.Bound != 0 || snap.UnmappedCount != 2 {
		t.Fatalf("%+v", snap)
	}
	if snap.Unmapped[0].Reason != ReasonMissingAttrs {
		t.Fatalf("reason=%q", snap.Unmapped[0].Reason)
	}
	if snap.Unmapped[1].Reason != ReasonNoFile {
		t.Fatalf("reason=%q", snap.Unmapped[1].Reason)
	}
}

func TestBindDoesNotUseSpanName(t *testing.T) {
	t.Parallel()
	g := fixtureGraph(t)
	snap := Bind(g, []ingest.Span{{
		TraceID:     "aa",
		SpanID:      "01",
		ServiceName: "payment",
		Name:        "POST /authorize",
		HTTPRoute:   "POST /authorize",
	}})
	if snap.Bound != 0 || snap.UnmappedCount != 1 {
		t.Fatalf("%+v", snap)
	}
	if snap.Unmapped[0].Reason != ReasonMissingAttrs {
		t.Fatalf("reason=%q", snap.Unmapped[0].Reason)
	}
}

func TestBindUnknownFile(t *testing.T) {
	t.Parallel()
	g := fixtureGraph(t)
	snap := Bind(g, []ingest.Span{{
		TraceID:      "aa",
		SpanID:       "01",
		CodeFunction: "Handler.authorize",
		CodeFile:     "examples/shop/internal/missing/handler.go",
	}})
	if snap.Unmapped[0].Reason != ReasonNoFile {
		t.Fatalf("reason=%q", snap.Unmapped[0].Reason)
	}
}

func TestBindUnknownFunction(t *testing.T) {
	t.Parallel()
	g := fixtureGraph(t)
	snap := Bind(g, []ingest.Span{{
		TraceID:      "aa",
		SpanID:       "01",
		CodeFunction: "Handler.missing",
		CodeFile:     "internal/payment/handler.go",
	}})
	if snap.Unmapped[0].Reason != ReasonNoFunction {
		t.Fatalf("reason=%q", snap.Unmapped[0].Reason)
	}
}

func TestBindAmbiguousNameInFile(t *testing.T) {
	t.Parallel()
	g, err := source.FromSnapshot(source.Snapshot{
		Module: "example.com/demo",
		Nodes: []source.Node{
			{ID: "file:dup.go", Kind: source.KindFile, Name: "dup.go", File: "dup.go"},
			{ID: "func:a.foo", Kind: source.KindFunction, Name: "foo", File: "dup.go", Line: 1},
			{ID: "func:b.foo", Kind: source.KindFunction, Name: "foo", File: "dup.go", Line: 20},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	snap := Bind(g, []ingest.Span{{
		TraceID:      "aa",
		SpanID:       "01",
		CodeFunction: "foo",
		CodeFile:     "dup.go",
	}})
	if snap.Unmapped[0].Reason != ReasonAmbiguous {
		t.Fatalf("reason=%q", snap.Unmapped[0].Reason)
	}
}

func TestBindRejectsPathTraversalFile(t *testing.T) {
	t.Parallel()
	g := fixtureGraph(t)
	snap := Bind(g, []ingest.Span{{
		TraceID:      "aa",
		SpanID:       "01",
		CodeFunction: "authorize",
		CodeFile:     "../secret.go",
	}})
	if snap.Unmapped[0].Reason != ReasonInvalidFile {
		t.Fatalf("reason=%q", snap.Unmapped[0].Reason)
	}
}

func TestBindShopPaymentAuthorize(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	g, err := source.Load(ctx, shopDir())
	if err != nil {
		t.Fatal(err)
	}
	snap := Bind(g, []ingest.Span{{
		TraceID:      "aa",
		SpanID:       "01",
		ServiceName:  "payment",
		Name:         "payment.Authorize",
		CodeFunction: "Handler.authorize",
		CodeFile:     "examples/shop/internal/payment/handler.go",
	}})
	if snap.Bound != 1 {
		t.Fatalf("%+v", snap)
	}
	if snap.Bindings[0].SourceName != "authorize" {
		t.Fatalf("%+v", snap.Bindings[0])
	}
	if !strings.Contains(snap.Bindings[0].SourceID, "payment") || !strings.Contains(snap.Bindings[0].SourceID, "authorize") {
		t.Fatalf("source_id=%q", snap.Bindings[0].SourceID)
	}
}

func fixtureGraph(t *testing.T) *source.Graph {
	t.Helper()
	g, err := source.FromSnapshot(source.Snapshot{
		Module: "example.com/demo",
		Nodes: []source.Node{
			{ID: "file:internal/payment/handler.go", Kind: source.KindFile, Name: "handler.go", File: "internal/payment/handler.go"},
			{ID: "func:pay.Handler.authorize", Kind: source.KindFunction, Name: "authorize", File: "internal/payment/handler.go", Line: 58},
			{ID: "func:pay.Handler.chargeProcessor", Kind: source.KindFunction, Name: "chargeProcessor", File: "internal/payment/handler.go", Line: 142},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func shopDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "examples", "shop"))
}
