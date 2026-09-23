package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

func TestRunImpactMissingGoModUsesFiles(t *testing.T) {
	t.Parallel()
	var posted atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/impact" {
			posted.Store(true)
			http.Error(w, "must not use shop snapshot", http.StatusInternalServerError)
			return
		}
		if r.URL.Path != "/v1/spans" {
			http.NotFound(w, r)
			return
		}
		writeTestJSON(w, map[string]any{"spans": []ingest.Span{}})
	}))
	t.Cleanup(srv.Close)
	raw := `diff --git a/backend/server.py b/backend/server.py
--- a/backend/server.py
+++ b/backend/server.py
@@ -1,1 +1,1 @@
-a
+b
`
	var out strings.Builder
	err := RunImpact(t.Context(), []string{"-api", srv.URL, "-dir", t.TempDir()}, strings.NewReader(raw), &out)
	if err != nil {
		t.Fatal(err)
	}
	if posted.Load() {
		t.Fatal("posted to /v1/impact")
	}
	got := out.String()
	if !strings.Contains(got, "origin=files") || !strings.Contains(got, "backend/server.py") {
		t.Fatalf("%s", got)
	}
	if strings.Contains(got, "chargeProcessor") {
		t.Fatalf("shop leak:\n%s", got)
	}
}

func TestRunImpactWorktree(t *testing.T) {
	t.Parallel()
	dir := writeModule(t, "example.com/app", "package app\n\nfunc Hello() int {\n\treturn 1\n}\n")
	runGitInit(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package app\n\nfunc Hello() int {\n\treturn 2\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/spans" {
			http.NotFound(w, r)
			return
		}
		writeTestJSON(w, map[string]any{"spans": []ingest.Span{}})
	}))
	t.Cleanup(srv.Close)
	var out strings.Builder
	err := RunImpact(t.Context(), []string{"-api", srv.URL, "-dir", dir}, strings.NewReader(""), &out)
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "diff=worktree") || !strings.Contains(got, "Hello") {
		t.Fatalf("%s", got)
	}
}

func TestRunObserveForeignDirNotShop(t *testing.T) {
	t.Parallel()
	var usedAPISource atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/graph":
			writeTestJSON(w, graph.Snapshot{
				TraceCount: 1,
				SpanCount:  1,
				Services:   []graph.Service{{Name: "reroute", SpanCount: 1}},
			})
		case "/v1/spans":
			writeTestJSON(w, map[string]any{
				"spans": []ingest.Span{{
					TraceID:     "aa",
					SpanID:      "01",
					ServiceName: "reroute",
					Kind:        "server",
					HTTPMethod:  http.MethodGet,
					HTTPRoute:   "/api/health",
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
	err := RunObserve(t.Context(), []string{"-api", srv.URL, "-dir", t.TempDir(), "-traces", "20"}, &out, &errBuf)
	if err != nil {
		t.Fatal(err)
	}
	if usedAPISource.Load() {
		t.Fatal("fetched API source")
	}
	got := out.String()
	if !strings.Contains(got, "origin=none") || !strings.Contains(got, "GET /api/health") {
		t.Fatalf("%s", got)
	}
	if strings.Contains(got, "examples/shop") {
		t.Fatalf("shop leak:\n%s", got)
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

func runGitInit(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_TERMINAL_PROMPT=0",
			"GIT_AUTHOR_NAME=aquila",
			"GIT_AUTHOR_EMAIL=aquila@test",
			"GIT_COMMITTER_NAME=aquila",
			"GIT_COMMITTER_EMAIL=aquila@test",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("add", ".")
	run("-c", "user.email=aquila@test", "-c", "user.name=aquila", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "init")
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

func TestRunAskHitsHealthRoute(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/graph":
			writeTestJSON(w, graph.Snapshot{
				Services: []graph.Service{{Name: "reroute"}},
			})
		case "/v1/spans":
			writeTestJSON(w, map[string]any{
				"spans": []ingest.Span{{
					TraceID:     "aa",
					SpanID:      "01",
					ServiceName: "reroute",
					Kind:        "server",
					HTTPMethod:  http.MethodGet,
					HTTPRoute:   "/api/health",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	var out, errBuf strings.Builder
	err := RunAsk(t.Context(), []string{"-api", srv.URL, "-dir", t.TempDir(), "health"}, strings.NewReader(""), &out, &errBuf)
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "/api/health") {
		t.Fatalf("%s", got)
	}
}

func TestAllowsShopPair(t *testing.T) {
	t.Parallel()
	if allowsShopPair(t.TempDir(), t.TempDir()) {
		t.Fatal("foreign python-style dir must not start shop compose")
	}
	dir := writeModule(t, controlPlaneModule, "package p\nfunc F() {}\n")
	if !allowsShopPair(dir, "") {
		t.Fatal("control-plane dir should allow shop pair")
	}
	root := moduleRoot(t)
	shop := filepath.Join(root, "examples", "shop")
	if !allowsShopPair(shop, "") {
		t.Fatal("examples/shop")
	}
	if allowsShopPair(t.TempDir(), shop) {
		t.Fatal("-shop must not license a foreign -dir")
	}
}

func TestRunWorkerRejectsInvalidJob(t *testing.T) {
	t.Parallel()
	err := RunWorker(t.Context(), []string{"-once", "-job", "not-hex"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "invalid job id") {
		t.Fatalf("%v", err)
	}
}

func TestRunReplayMixedWindowRequiresService(t *testing.T) {
	t.Parallel()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/spans" {
			http.NotFound(w, r)
			return
		}
		writeTestJSON(w, map[string]any{
			"spans": []ingest.Span{
				{ServiceName: "gateway", Kind: "server", HTTPMethod: http.MethodGet, HTTPRoute: "/users/{id}"},
				{ServiceName: "ledger", Kind: "server", HTTPMethod: http.MethodGet, HTTPRoute: "/invoice"},
			},
		})
	}))
	t.Cleanup(api.Close)
	err := RunReplay(t.Context(), []string{
		"-api", api.URL,
		"-base", "http://127.0.0.1:19191",
		"-patch", "http://127.0.0.1:19192",
	}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "mixed services") {
		t.Fatalf("got %v", err)
	}
}

func TestRunReplayServiceDropsOtherAttachRoutes(t *testing.T) {
	t.Parallel()
	var queried string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/spans" {
			http.NotFound(w, r)
			return
		}
		queried = r.URL.Query().Get("service")
		writeTestJSON(w, map[string]any{
			"spans": []ingest.Span{
				{TraceID: "shop", SpanID: "1", ServiceName: "gateway", Kind: "server", HTTPMethod: http.MethodGet, HTTPRoute: "/users/{id}", StartTime: time.Unix(20, 0).UTC()},
				{TraceID: "app", SpanID: "2", ServiceName: "ledger", Kind: "server", HTTPMethod: http.MethodGet, HTTPRoute: "/invoice", StartTime: time.Unix(10, 0).UTC()},
			},
		})
	}))
	t.Cleanup(api.Close)
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/invoice" {
			http.NotFound(w, r)
			return
		}
		writeReplayJSON(w, map[string]any{"id": "inv-1"})
	}))
	t.Cleanup(gw.Close)
	var out strings.Builder
	err := RunReplay(t.Context(), []string{
		"-api", api.URL, "-service", "ledger",
		"-base", gw.URL, "-patch", gw.URL,
	}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if queried != "ledger" {
		t.Fatalf("query service=%q", queried)
	}
	got := out.String()
	if !strings.Contains(got, "GET /invoice") || strings.Contains(got, "/users") {
		t.Fatalf("%s", got)
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
