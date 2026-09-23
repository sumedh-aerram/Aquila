package impact

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sumedhaerram/aquila/internal/diff"
	"github.com/sumedhaerram/aquila/internal/graph"
	"github.com/sumedhaerram/aquila/internal/locate"
	"github.com/sumedhaerram/aquila/internal/source"
)

func TestAnalyzeDirectAndCaller(t *testing.T) {
	t.Parallel()
	g := fixture(t)
	d, err := diff.Parse([]byte(`--- a/internal/payment/handler.go
+++ b/internal/payment/handler.go
@@ -40,3 +40,3 @@
 context
-	old
+	new
`))
	if err != nil {
		t.Fatal(err)
	}
	rep := Analyze(g, d, locate.Snapshot{}, graph.Snapshot{})
	if !hasFinding(rep.Direct, "chargeProcessor") {
		t.Fatalf("direct=%+v", rep.Direct)
	}
	if rep.Direct[0].Line < 1 {
		t.Fatalf("direct line=%+v", rep.Direct)
	}
	if !hasFinding(rep.Likely, "authorize") {
		t.Fatalf("likely=%+v", rep.Likely)
	}
	if hasFinding(rep.Likely, "NewEphemeral") {
		t.Fatal("callee must not be likely")
	}
}

func TestAnalyzeRuntimePathFromLocate(t *testing.T) {
	t.Parallel()
	g := fixture(t)
	d, err := diff.Parse([]byte(`--- a/internal/payment/handler.go
+++ b/internal/payment/handler.go
@@ -10,3 +10,3 @@
 context
-	old
+	new
`))
	if err != nil {
		t.Fatal(err)
	}
	loc := locate.Snapshot{Bindings: []locate.Binding{{
		SourceID:    "func:pay.Handler.authorize",
		SourceName:  "authorize",
		ServiceName: "payment",
		File:        "internal/payment/handler.go",
		HTTPMethod:  "POST",
		HTTPRoute:   "/authorize",
		Provenance:  locate.ProvenanceCodeAttrs,
	}}}
	rt := graph.Snapshot{Paths: []graph.Path{{
		Services:   []string{"checkout", "payment", "processor"},
		Traces:     1,
		Provenance: graph.ProvenanceObservedParent,
	}}}
	rep := Analyze(g, d, loc, rt)
	if !hasFinding(rep.Direct, "authorize") {
		t.Fatalf("direct=%+v", rep.Direct)
	}
	foundRoute, foundPath := false, false
	for _, f := range rep.Runtime {
		if f.Route == "POST /authorize" && f.Reason == "bound_span" {
			foundRoute = true
		}
		if strings.Contains(f.Path, "checkout -> payment") {
			foundPath = true
		}
	}
	if !foundRoute || !foundPath {
		t.Fatalf("runtime=%+v", rep.Runtime)
	}
}

func TestAnalyzeUnknownFileUnobserved(t *testing.T) {
	t.Parallel()
	g := fixture(t)
	d, err := diff.Parse([]byte(`--- a/internal/missing/x.go
+++ b/internal/missing/x.go
@@ -1,1 +1,1 @@
-a
+b
`))
	if err != nil {
		t.Fatal(err)
	}
	rep := Analyze(g, d, locate.Snapshot{}, graph.Snapshot{})
	if len(rep.Unobserved) == 0 || rep.Unobserved[0].Reason != "unknown_file" {
		t.Fatalf("%+v", rep.Unobserved)
	}
}

func TestAnalyzeShopD1ChargeProcessor(t *testing.T) {
	t.Parallel()
	g := loadedShop(t)
	raw := []byte(`diff --git a/examples/shop/internal/payment/handler.go b/examples/shop/internal/payment/handler.go
--- a/examples/shop/internal/payment/handler.go
+++ b/examples/shop/internal/payment/handler.go
@@ -142,7 +142,7 @@ func (h *Handler) chargeProcessor(ctx context.Context, req authorizeReq) error {
 	// INTENTIONAL DEFECT D1: new HTTP client on every authorize (DEFECTS.md).
-	client := svcclient.NewEphemeral()
+	client := svcclient.Shared()
 	return svcclient.PostJSON(ctx, client, h.processorURL+"/charge", map[string]any{
 		"checkout_id":  req.CheckoutID,
 		"amount_cents": req.AmountCents,
`)
	d, err := diff.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	rep := Analyze(g, d, locate.Snapshot{}, graph.Snapshot{})
	if !hasFinding(rep.Direct, "chargeProcessor") {
		t.Fatalf("direct=%+v", rep.Direct)
	}
	if !hasFinding(rep.Likely, "authorize") {
		t.Fatalf("likely=%+v", rep.Likely)
	}
}

func fixture(t *testing.T) *source.Graph {
	t.Helper()
	g, err := source.FromSnapshot(source.Snapshot{
		Module: "example.com/demo",
		Nodes: []source.Node{
			{ID: "file:internal/payment/handler.go", Kind: source.KindFile, Name: "handler.go", File: "internal/payment/handler.go"},
			{ID: "func:pay.Handler.authorize", Kind: source.KindFunction, Name: "authorize", File: "internal/payment/handler.go", Line: 10},
			{ID: "func:pay.Handler.chargeProcessor", Kind: source.KindFunction, Name: "chargeProcessor", File: "internal/payment/handler.go", Line: 40},
			{ID: "func:svc.NewEphemeral", Kind: source.KindFunction, Name: "NewEphemeral", File: "internal/svcclient/client.go", Line: 32},
		},
		Edges: []source.Edge{
			{From: "func:pay.Handler.authorize", To: "func:pay.Handler.chargeProcessor", Kind: source.EdgeCalls, Provenance: source.ProvenanceTypes},
			{From: "func:pay.Handler.chargeProcessor", To: "func:svc.NewEphemeral", Kind: source.EdgeCalls, Provenance: source.ProvenanceTypes},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func hasFinding(fs []Finding, name string) bool {
	for _, f := range fs {
		if f.Name == name {
			return true
		}
	}
	return false
}

func shopDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "examples", "shop"))
}
