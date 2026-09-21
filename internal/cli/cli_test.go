package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sumedhaerram/aquila/internal/graph"
	"github.com/sumedhaerram/aquila/internal/locate"
	"github.com/sumedhaerram/aquila/internal/source"
)

func TestRunStatusReady(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			writeTestJSON(w, map[string]string{"status": "ok"})
		case "/readyz":
			writeTestJSON(w, map[string]string{"status": "ready"})
		case "/version":
			writeTestJSON(w, map[string]string{"name": "aquila", "version": "test"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	var out strings.Builder
	err := RunStatus(t.Context(), []string{"-api", srv.URL}, &out)
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "ready    ready") || !strings.Contains(got, "version  test") {
		t.Fatalf("%q", got)
	}
}

func TestRunStatusNotReady(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			writeTestJSON(w, map[string]string{"status": "ok"})
		case "/readyz":
			w.WriteHeader(http.StatusServiceUnavailable)
			writeTestJSON(w, map[string]string{"status": "unavailable", "error": "postgres unreachable"})
		case "/version":
			writeTestJSON(w, map[string]string{"name": "aquila", "version": "test"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	var out strings.Builder
	err := RunStatus(t.Context(), []string{"-api", srv.URL}, &out)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(out.String(), "ready    unavailable") {
		t.Fatalf("%q", out.String())
	}
}

func TestRunObservePrintsHopAndBinding(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/graph":
			writeTestJSON(w, graph.Snapshot{
				TraceCount: 1,
				SpanCount:  2,
				Services:   []graph.Service{{Name: "checkout", SpanCount: 1}, {Name: "payment", SpanCount: 1}},
				Edges:      []graph.Edge{{From: "checkout", To: "payment", Count: 1, Provenance: graph.ProvenanceObservedParent}},
				Paths:      []graph.Path{{Services: []string{"checkout", "payment"}, Traces: 1, Provenance: graph.ProvenanceObservedParent}},
			})
		case "/v1/source":
			writeTestJSON(w, source.Snapshot{
				Module: "github.com/sumedhaerram/aquila/examples/shop",
				Nodes: []source.Node{
					{ID: "pkg:pay", Kind: source.KindPackage, Name: "payment"},
					{ID: "file:handler.go", Kind: source.KindFile, Name: "handler.go", File: "internal/payment/handler.go"},
					{ID: "func:authorize", Kind: source.KindFunction, Name: "authorize", File: "internal/payment/handler.go", Line: 58},
				},
			})
		case "/v1/locate":
			writeTestJSON(w, locate.Snapshot{
				SpanCount:     2,
				Bound:         1,
				UnmappedCount: 1,
				Bindings: []locate.Binding{{
					ServiceName: "payment",
					SourceName:  "authorize",
					File:        "internal/payment/handler.go",
					Line:        58,
					Provenance:  locate.ProvenanceCodeAttrs,
				}},
				Unmapped: []locate.Unmapped{{Reason: locate.ReasonMissingAttrs}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	var out, errBuf strings.Builder
	err := RunObserve(t.Context(), []string{"-api", srv.URL, "-traces", "20"}, &out, &errBuf)
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "checkout -> payment") {
		t.Fatalf("missing hop:\n%s", got)
	}
	if !strings.Contains(got, "observed_parent") {
		t.Fatalf("missing runtime provenance:\n%s", got)
	}
	if !strings.Contains(got, "bind  payment  authorize") {
		t.Fatalf("missing bind:\n%s", got)
	}
	if !strings.Contains(got, "missing_code_attrs  1") {
		t.Fatalf("missing unmapped reason:\n%s", got)
	}
	if !strings.Contains(got, "functions 1") {
		t.Fatalf("missing source counts:\n%s", got)
	}
}

func TestRunObserveSourceUnavailableStillPrintsRuntime(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/graph":
			writeTestJSON(w, graph.Snapshot{TraceCount: 1, SpanCount: 1, Services: []graph.Service{{Name: "gateway", SpanCount: 1}}})
		case "/v1/source", "/v1/locate":
			w.WriteHeader(http.StatusServiceUnavailable)
			writeTestJSON(w, map[string]string{"error": "source unavailable"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	var out, errBuf strings.Builder
	err := RunObserve(t.Context(), []string{"-api", srv.URL}, &out, &errBuf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "runtime") || !strings.Contains(out.String(), "source  unavailable") {
		t.Fatalf("%s", out.String())
	}
}

func TestNewClientRejectsNonHTTP(t *testing.T) {
	t.Parallel()
	if _, err := newClient("file:///etc/passwd"); err == nil {
		t.Fatal("expected error")
	}
}

func writeTestJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func TestClipTraces(t *testing.T) {
	t.Parallel()
	if clipTraces(0) != defaultTraces {
		t.Fatal(clipTraces(0))
	}
	if clipTraces(999) != maxTraces {
		t.Fatal(clipTraces(999))
	}
}

func TestRunStatusTimeout(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	err := RunStatus(ctx, []string{"-api", srv.URL}, io.Discard)
	if err == nil {
		t.Fatal("expected timeout")
	}
}
