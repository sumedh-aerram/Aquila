package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sumedhaerram/aquila/internal/graph"
	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/ingest"
	"github.com/sumedhaerram/aquila/internal/source"
)

func TestReportFromLocalMapsChangedFunction(t *testing.T) {
	t.Parallel()
	g, err := source.FromSnapshot(source.Snapshot{
		Module: "example.com/app",
		Nodes: []source.Node{
			{ID: "file:hello.go", Kind: source.KindFile, Name: "hello.go", File: "hello.go"},
			{ID: "func:app.Hello", Kind: source.KindFunction, Name: "Hello", File: "hello.go", Line: 3},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`--- a/hello.go
+++ b/hello.go
@@ -3,3 +3,3 @@ func Hello() int {
 context
-	return 1
+	return 2
`)
	rep, err := reportFromLocal(g, nil, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Direct) == 0 || rep.Direct[0].Name != "Hello" {
		t.Fatalf("%+v", rep.Direct)
	}
}

func TestLoadTargetSourceMissingGoMod(t *testing.T) {
	t.Parallel()
	if g := loadTargetSource(t.Context(), t.TempDir()); g != nil {
		t.Fatalf("module=%q", g.Module())
	}
}

func TestLoadTargetSourceSkipsControlPlane(t *testing.T) {
	t.Parallel()
	dir := writeModule(t, controlPlaneModule, "package p\nfunc F() {}\n")
	if g := loadTargetSource(t.Context(), dir); g != nil {
		t.Fatalf("control plane module leaked: %q", g.Module())
	}
}

func TestLoadTargetSourceKeepsForeignModule(t *testing.T) {
	t.Parallel()
	dir := writeModule(t, "example.com/app", "package app\nfunc Hello() int { return 1 }\n")
	g := loadTargetSource(t.Context(), dir)
	if g == nil || g.Module() != "example.com/app" {
		t.Fatalf("got %#v", g)
	}
}

func TestRunImpactUsesLocalModule(t *testing.T) {
	t.Parallel()
	dir := writeModule(t, "example.com/app", "package app\n\nfunc Hello() int {\n\treturn 1\n}\n")
	var posted atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/impact" {
			posted.Store(true)
			http.Error(w, "should use local source", http.StatusInternalServerError)
			return
		}
		if r.URL.Path != "/v1/spans" {
			http.NotFound(w, r)
			return
		}
		writeTestJSON(w, map[string]any{"spans": []ingest.Span{}})
	}))
	t.Cleanup(srv.Close)
	raw := `diff --git a/hello.go b/hello.go
--- a/hello.go
+++ b/hello.go
@@ -3,3 +3,3 @@ func Hello() int {
 context
-	return 1
+	return 2
`
	var out strings.Builder
	err := RunImpact(t.Context(), []string{"-api", srv.URL, "-dir", dir}, strings.NewReader(raw), &out)
	if err != nil {
		t.Fatal(err)
	}
	if posted.Load() {
		t.Fatal("posted to /v1/impact")
	}
	if !strings.Contains(out.String(), "Hello") {
		t.Fatalf("%s", out.String())
	}
}

func TestRunImpactFallsBackForControlPlane(t *testing.T) {
	t.Parallel()
	dir := writeModule(t, controlPlaneModule, "package p\nfunc F() {}\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/impact" {
			http.NotFound(w, r)
			return
		}
		writeTestJSON(w, impact.Report{
			Files:  []string{"internal/payment/handler.go"},
			Direct: []impact.Finding{{Name: "chargeProcessor", File: "internal/payment/handler.go", Reason: "changed_lines", Provenance: "diff"}},
		})
	}))
	t.Cleanup(srv.Close)
	var out strings.Builder
	err := RunImpact(t.Context(), []string{"-api", srv.URL, "-dir", dir}, strings.NewReader("diff --git a/x b/x\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "chargeProcessor") {
		t.Fatalf("%s", out.String())
	}
}

func TestRunImpactEmptyDirFallsBackToAPI(t *testing.T) {
	t.Parallel()
	srv := impactAPI(t, impact.Report{
		Files:  []string{"internal/payment/handler.go"},
		Direct: []impact.Finding{{Name: "chargeProcessor"}},
	})
	var out strings.Builder
	err := RunImpact(t.Context(), []string{"-api", srv.URL, "-dir", t.TempDir()}, strings.NewReader("diff --git a/x b/x\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "chargeProcessor") {
		t.Fatalf("%s", out.String())
	}
}

func writeModule(t *testing.T, module, src string) string {
	t.Helper()
	dir := t.TempDir()
	mod := "module " + module + "\n\ngo 1.25\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRunReplayForeignGETFromSpans(t *testing.T) {
	t.Parallel()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/spans" {
			http.NotFound(w, r)
			return
		}
		writeTestJSON(w, map[string]any{
			"spans": []ingest.Span{{
				ServiceName: "api",
				Kind:        "server",
				HTTPMethod:  http.MethodGet,
				HTTPRoute:   "/v1/foo",
			}},
		})
	}))
	t.Cleanup(api.Close)
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/foo" {
			http.NotFound(w, r)
			return
		}
		writeReplayJSON(w, map[string]any{"ok": true})
	}))
	t.Cleanup(gw.Close)
	var out strings.Builder
	err := RunReplay(t.Context(), []string{"-api", api.URL, "-base", gw.URL, "-patch", gw.URL}, &out)
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "GET /v1/foo") || !strings.Contains(got, "verdict=match") {
		t.Fatalf("%s", got)
	}
	if strings.Contains(got, "verdict=pass") {
		t.Fatalf("must not report pass: %s", got)
	}
}

func TestRunReplaySkipsPOSTWithoutFixture(t *testing.T) {
	t.Parallel()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/spans" {
			http.NotFound(w, r)
			return
		}
		writeTestJSON(w, map[string]any{
			"spans": []ingest.Span{{
				ServiceName: "gateway",
				Kind:        "server",
				HTTPMethod:  http.MethodPost,
				HTTPRoute:   "/checkout",
			}},
		})
	}))
	t.Cleanup(api.Close)
	err := RunReplay(t.Context(), []string{
		"-api", api.URL,
		"-base", "http://127.0.0.1:18180",
		"-patch", "http://127.0.0.1:18280",
	}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "empty workload") {
		t.Fatalf("%v", err)
	}
}

func TestRunObserveUsesLocalModule(t *testing.T) {
	t.Parallel()
	dir := writeModule(t, "example.com/app", "package app\n\nfunc Hello() int {\n\treturn 1\n}\n")
	var usedAPISource atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/graph":
			writeTestJSON(w, graph.Snapshot{
				TraceCount: 1,
				SpanCount:  1,
				Services:   []graph.Service{{Name: "api", SpanCount: 1}},
				Edges:      []graph.Edge{{From: "gw", To: "api", Count: 1, Provenance: graph.ProvenanceObservedParent}},
			})
		case "/v1/spans":
			writeTestJSON(w, map[string]any{
				"spans": []ingest.Span{{
					TraceID:      "aa",
					SpanID:       "01",
					ServiceName:  "api",
					Kind:         "server",
					HTTPMethod:   http.MethodGet,
					HTTPRoute:    "/v1/foo",
					CodeFunction: "Hello",
					CodeFile:     "hello.go",
				}},
			})
		case "/v1/source", "/v1/locate":
			usedAPISource.Store(true)
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	var out, errBuf strings.Builder
	err := RunObserve(t.Context(), []string{"-api", srv.URL, "-dir", dir, "-traces", "20"}, &out, &errBuf)
	if err != nil {
		t.Fatal(err)
	}
	if usedAPISource.Load() {
		t.Fatal("fetched API source")
	}
	got := out.String()
	if !strings.Contains(got, "module=example.com/app") || !strings.Contains(got, "origin=cwd") {
		t.Fatalf("%s", got)
	}
	if !strings.Contains(got, "bind  api  Hello") {
		t.Fatalf("missing local bind:\n%s", got)
	}
	if !strings.Contains(got, "gw -> api") {
		t.Fatalf("missing hop:\n%s", got)
	}
}

func TestRunReplayWorkloadFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "w.json")
	raw := `{"steps":[{"method":"POST","path":"/v1/foo","body":{"ok":true}}]}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	var method atomic.Value
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method.Store(r.Method + " " + r.URL.Path)
		writeReplayJSON(w, map[string]any{"ok": true})
	}))
	t.Cleanup(gw.Close)
	var out strings.Builder
	err := RunReplay(t.Context(), []string{"-base", gw.URL, "-patch", gw.URL, "-workload", path}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if method.Load() != "POST /v1/foo" {
		t.Fatalf("method=%v", method.Load())
	}
	got := out.String()
	if !strings.Contains(got, "POST /v1/foo") || !strings.Contains(got, "provenance workload_file") {
		t.Fatalf("%s", got)
	}
	if strings.Contains(got, "verdict=pass") {
		t.Fatalf("must not report pass: %s", got)
	}
}

func TestRunReplayFixtureAndWorkloadConflict(t *testing.T) {
	t.Parallel()
	err := RunReplay(t.Context(), []string{
		"-base", "http://127.0.0.1:18180",
		"-patch", "http://127.0.0.1:18280",
		"-fixture",
		"-workload", "w.json",
	}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("%v", err)
	}
}
