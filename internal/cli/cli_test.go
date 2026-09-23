package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sumedhaerram/aquila/internal/evidence"
	"github.com/sumedhaerram/aquila/internal/graph"
	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/locate"
	"github.com/sumedhaerram/aquila/internal/pair"
	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
	"github.com/sumedhaerram/aquila/internal/runs"
	"github.com/sumedhaerram/aquila/internal/source"
)

func shopSnapshotDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mod := "module " + controlPlaneModule + "\n\ngo 1.25\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

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

func TestRunImpactPrintsDirectAndLikely(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/impact" {
			http.NotFound(w, r)
			return
		}
		writeTestJSON(w, impact.Report{
			Files:  []string{"internal/payment/handler.go"},
			Direct: []impact.Finding{{Name: "chargeProcessor", File: "internal/payment/handler.go", Reason: "changed_lines", Provenance: "diff"}},
			Likely: []impact.Finding{{Name: "authorize", File: "internal/payment/handler.go", Reason: "caller", Provenance: "types"}},
		})
	}))
	t.Cleanup(srv.Close)
	var out strings.Builder
	err := RunImpact(t.Context(), []string{"-api", srv.URL}, strings.NewReader("diff --git a/x b/x\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "chargeProcessor") || !strings.Contains(got, "authorize") {
		t.Fatalf("%s", got)
	}
}

func TestRunImpactEmptyDiff(t *testing.T) {
	t.Parallel()
	err := RunImpact(t.Context(), []string{"-api", "http://127.0.0.1:8080", "-dir", t.TempDir()}, strings.NewReader(""), io.Discard)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no local changes") {
		t.Fatalf("%v", err)
	}
}

func TestRunEnvPrintsPrepared(t *testing.T) {
	t.Parallel()
	shop := t.TempDir()
	if err := os.WriteFile(filepath.Join(shop, "go.mod"), []byte("module example.com/s\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shop, "Dockerfile"), []byte("FROM alpine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(shop, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shop, "internal", "a.go"), []byte("package p\nconst X = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw := "--- a/internal/a.go\n+++ b/internal/a.go\n@@ -1,2 +1,2 @@\n package p\n-const X = 1\n+const X = 2\n"
	var out strings.Builder
	err := RunEnv(t.Context(), []string{"-shop", shop, "-out", t.TempDir()}, strings.NewReader(raw), &out)
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "status=prepared") || !strings.Contains(got, "not started") {
		t.Fatalf("%s", got)
	}
}

func TestRunEnvEmptyDiff(t *testing.T) {
	t.Parallel()
	err := RunEnv(t.Context(), nil, strings.NewReader(""), io.Discard)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunReplayDetectsJSONDifference(t *testing.T) {
	t.Parallel()
	base := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeReplayJSON(w, map[string]any{"status": "ok"})
	}))
	t.Cleanup(base.Close)
	patch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeReplayJSON(w, map[string]any{"status": "ok", "debug": true})
	}))
	t.Cleanup(patch.Close)
	var out strings.Builder
	err := RunReplay(t.Context(), []string{"-base", base.URL, "-patch", patch.URL, "-fixture"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "verdict=differ") {
		t.Fatalf("%s", got)
	}
	if strings.Contains(got, "verdict=pass") || strings.Contains(got, "verdict=PASS") {
		t.Fatalf("must not report pass: %s", got)
	}
	if !strings.Contains(got, "p95=withheld") {
		t.Fatalf("n=1 must withhold p95: %s", got)
	}
}

func TestRunReplayIncompleteWhenPatchDown(t *testing.T) {
	t.Parallel()
	base := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeReplayJSON(w, map[string]any{"status": "ok"})
	}))
	t.Cleanup(base.Close)
	down := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	down.Close()
	err := RunReplay(t.Context(), []string{"-base", base.URL, "-patch", down.URL, "-fixture"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("down patch must be incomplete, err=%v", err)
	}
}

func TestRunReplayRequiresBothTargets(t *testing.T) {
	t.Parallel()
	err := RunReplay(t.Context(), []string{"-fixture", "-base", "http://127.0.0.1:18180"}, io.Discard)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunReplayEmptySpansWithoutFixture(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/spans" {
			http.NotFound(w, r)
			return
		}
		writeTestJSON(w, map[string]any{"spans": []any{}})
	}))
	t.Cleanup(srv.Close)
	err := RunReplay(t.Context(), []string{
		"-api", srv.URL,
		"-base", "http://127.0.0.1:18180",
		"-patch", "http://127.0.0.1:18280",
	}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "empty workload") {
		t.Fatalf("%v", err)
	}
}

func writeReplayJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func TestRunReplayRepeatsAndWithholdsP95(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		writeReplayJSON(w, map[string]any{"status": "ok"})
	}))
	t.Cleanup(srv.Close)
	var out strings.Builder
	err := RunReplay(t.Context(), []string{"-base", srv.URL, "-patch", srv.URL, "-fixture", "-n", "3"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if hits.Load() != 18 {
		t.Fatalf("hits=%d want 18 (3 steps × 3 repeats × 2 targets)", hits.Load())
	}
	if !strings.Contains(got, "n=3") || !strings.Contains(got, "p95=withheld") || !strings.Contains(got, "verdict=match") {
		t.Fatalf("%s", got)
	}
	if strings.Contains(got, "verdict=pass") || strings.Contains(got, "verdict=PASS") {
		t.Fatalf("must not report pass: %s", got)
	}
}

func TestRunReplayRejectsHugeN(t *testing.T) {
	t.Parallel()
	err := RunReplay(t.Context(), []string{
		"-base", "http://127.0.0.1:18180",
		"-patch", "http://127.0.0.1:18280",
		"-fixture",
		"-n", "101",
	}, io.Discard)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunFaultRequiresTarget(t *testing.T) {
	t.Parallel()
	err := RunFault(t.Context(), []string{"-listen", "127.0.0.1:0"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "target") {
		t.Fatalf("%v", err)
	}
}

func TestRunFaultRejectsNonLoopback(t *testing.T) {
	t.Parallel()
	err := RunFault(t.Context(), []string{"-listen", "0.0.0.0:19080", "-target", "http://127.0.0.1:18180"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("%v", err)
	}
}

func TestRunFaultRejectsHugeDelay(t *testing.T) {
	t.Parallel()
	err := RunFault(t.Context(), []string{"-listen", "127.0.0.1:0", "-target", "http://127.0.0.1:18180", "-delay", "31s"}, io.Discard)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunFaultInjectsThenStops(t *testing.T) {
	t.Parallel()
	hits := 0
	up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits++
	}))
	t.Cleanup(up.Close)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	out := &syncWriter{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunFault(ctx, []string{"-listen", "127.0.0.1:0", "-target", up.URL, "-status", "502"}, out)
	}()
	var addr string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-errCh:
			t.Fatalf("fault exited: %v out=%q", err, out.String())
		default:
		}
		addr = listenHostPort(out.String())
		if addr != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if addr == "" {
		t.Fatalf("no listen line: %q", out.String())
	}
	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway || !strings.Contains(string(body), "injected") {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if hits != 0 {
		t.Fatal("injected status must not call upstream")
	}
	got := out.String()
	if strings.Contains(got, "verdict=pass") || strings.Contains(got, "verdict=PASS") {
		t.Fatalf("must not report pass: %s", got)
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fault did not stop")
	}
}

func listenHostPort(out string) string {
	const p = "listen="
	i := strings.Index(out, p)
	if i < 0 {
		return ""
	}
	rest := out[i+len(p):]
	if j := strings.IndexByte(rest, ' '); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(rest)
}

type syncWriter struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncWriter) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestRunPlanPrintsOperatorFaultWhenRuntime(t *testing.T) {
	t.Parallel()
	api := impactAPI(t, impact.Report{
		Files:   []string{"internal/payment/handler.go"},
		Direct:  []impact.Finding{{Name: "chargeProcessor", File: "internal/payment/handler.go", Reason: "changed_lines", Provenance: "diff"}},
		Runtime: []impact.Finding{{Path: "gateway -> payment", Reason: "observed_path", Provenance: "observed_parent"}},
	})
	var out strings.Builder
	err := RunPlan(t.Context(), []string{"-api", api.URL}, strings.NewReader("diff --git a/x b/x\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "fault_status") || !strings.Contains(got, "operator") {
		t.Fatalf("%s", got)
	}
	if !strings.Contains(got, "not validated") {
		t.Fatalf("%s", got)
	}
	if strings.Contains(got, "overall=pass") || strings.Contains(got, "verdict=pass") {
		t.Fatalf("must not report pass: %s", got)
	}
}

func TestRunPlanOmitsFaultWithoutRuntime(t *testing.T) {
	t.Parallel()
	api := impactAPI(t, impact.Report{
		Files:  []string{"internal/payment/handler.go"},
		Direct: []impact.Finding{{Name: "chargeProcessor", File: "internal/payment/handler.go", Reason: "changed_lines"}},
	})
	var out strings.Builder
	err := RunPlan(t.Context(), []string{"-api", api.URL}, strings.NewReader("diff --git a/x b/x\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.Contains(got, "fault_status") {
		t.Fatalf("fault without runtime: %s", got)
	}
	if !strings.Contains(got, "fault omitted") {
		t.Fatalf("%s", got)
	}
}

func TestRunPlanRejectsEmptyImpact(t *testing.T) {
	t.Parallel()
	api := impactAPI(t, impact.Report{})
	err := RunPlan(t.Context(), []string{"-api", api.URL}, strings.NewReader("diff --git a/x b/x\n"), io.Discard)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunPlanEmptyDiff(t *testing.T) {
	t.Parallel()
	err := RunPlan(t.Context(), []string{"-api", "http://127.0.0.1:8080", "-dir", t.TempDir()}, strings.NewReader(""), io.Discard)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunExperimentMatchIsNotPass(t *testing.T) {
	t.Parallel()
	api := impactAPI(t, impact.Report{
		Files:   []string{"internal/payment/handler.go"},
		Direct:  []impact.Finding{{Name: "chargeProcessor"}},
		Runtime: []impact.Finding{{Path: "gateway -> payment", Reason: "observed_path"}},
	})
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeReplayJSON(w, map[string]any{"status": "ok"})
	}))
	t.Cleanup(gw.Close)
	var out strings.Builder
	err := RunExperiment(t.Context(), []string{
		"-api", api.URL, "-base", gw.URL, "-patch", gw.URL, "-fixture", "-n", "1", "-dir", shopSnapshotDir(t),
	}, strings.NewReader("diff --git a/x b/x\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "overall=match") {
		t.Fatalf("%s", got)
	}
	if !strings.Contains(got, "skipped") || !strings.Contains(got, "fault_status") {
		t.Fatalf("operator fault must be skipped: %s", got)
	}
	if strings.Contains(got, "overall=pass") || strings.Contains(got, "verdict=pass") {
		t.Fatalf("must not report pass: %s", got)
	}
	if !strings.Contains(got, "not validated") {
		t.Fatalf("%s", got)
	}
}

func TestRunExperimentDetectsJSONDifference(t *testing.T) {
	t.Parallel()
	api := impactAPI(t, impact.Report{
		Files:  []string{"a.go"},
		Direct: []impact.Finding{{Name: "F"}},
	})
	base := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeReplayJSON(w, map[string]any{"status": "ok"})
	}))
	t.Cleanup(base.Close)
	patch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeReplayJSON(w, map[string]any{"status": "ok", "debug": true})
	}))
	t.Cleanup(patch.Close)
	var out strings.Builder
	err := RunExperiment(t.Context(), []string{
		"-api", api.URL, "-base", base.URL, "-patch", patch.URL, "-fixture", "-n", "1", "-dir", shopSnapshotDir(t),
	}, strings.NewReader("diff --git a/x b/x\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "overall=differ") {
		t.Fatalf("%s", out.String())
	}
}

func TestRunExperimentIncompleteWhenPatchDown(t *testing.T) {
	t.Parallel()
	api := impactAPI(t, impact.Report{
		Files:  []string{"a.go"},
		Direct: []impact.Finding{{Name: "F"}},
	})
	base := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeReplayJSON(w, map[string]any{"status": "ok"})
	}))
	t.Cleanup(base.Close)
	down := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	down.Close()
	err := RunExperiment(t.Context(), []string{
		"-api", api.URL, "-base", base.URL, "-patch", down.URL, "-fixture", "-n", "1", "-dir", shopSnapshotDir(t),
	}, strings.NewReader("diff --git a/x b/x\n"), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("down patch must be incomplete, err=%v", err)
	}
}

func TestRunExperimentForeignDirRequiresGateways(t *testing.T) {
	t.Parallel()
	err := RunExperiment(t.Context(), []string{
		"-n", "1", "-dir", t.TempDir(),
	}, strings.NewReader(""), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "-base and -patch are required") {
		t.Fatalf("got %v", err)
	}
}

func TestRunExperimentFixtureRejectedWithDefaultShop(t *testing.T) {
	t.Parallel()
	shop := filepath.Join(moduleRoot(t), "examples", "shop")
	err := RunExperiment(t.Context(), []string{
		"-fixture", "-n", "1", "-dir", t.TempDir(), "-shop", shop,
		"-base", "http://127.0.0.1:19280", "-patch", "http://127.0.0.1:19281",
	}, strings.NewReader("diff --git a/x b/x\n"), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "-fixture is shop") {
		t.Fatalf("got %v", err)
	}
}

func TestRunExperimentFixtureRejectedOffShop(t *testing.T) {
	t.Parallel()
	err := RunExperiment(t.Context(), []string{
		"-fixture", "-n", "1", "-dir", t.TempDir(),
		"-base", "http://127.0.0.1:19280", "-patch", "http://127.0.0.1:19281",
	}, strings.NewReader("diff --git a/x b/x\n"), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "-fixture is shop") {
		t.Fatalf("got %v", err)
	}
}

func TestRunExperimentRequiresTargetsTogether(t *testing.T) {
	t.Parallel()
	err := RunExperiment(t.Context(), []string{"-fixture", "-base", "http://127.0.0.1:18180"}, strings.NewReader("diff --git a/x b/x\n"), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "together") {
		t.Fatalf("got %v", err)
	}
}

func TestRunExperimentLocalShopMissing(t *testing.T) {
	pairHookMu.Lock()
	defer pairHookMu.Unlock()
	api := impactAPI(t, impact.Report{
		Files:  []string{"a.go"},
		Direct: []impact.Finding{{Name: "F"}},
	})
	err := RunExperiment(t.Context(), []string{
		"-api", api.URL, "-fixture", "-n", "1", "-shop", t.TempDir(), "-dir", t.TempDir(),
	}, strings.NewReader("diff --git a/x b/x\n"), io.Discard)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunExperimentLocalEnvPrepared(t *testing.T) {
	pairHookMu.Lock()
	defer pairHookMu.Unlock()
	origP, origU, origD := preparePair, startPair, stopPair
	t.Cleanup(func() {
		preparePair, startPair, stopPair = origP, origU, origD
	})
	api := impactAPI(t, impact.Report{
		Files:  []string{"internal/payment/handler.go"},
		Direct: []impact.Finding{{Name: "chargeProcessor"}},
	})
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeReplayJSON(w, map[string]any{"status": "ok"})
	}))
	t.Cleanup(gw.Close)
	u, err := url.Parse(gw.URL)
	if err != nil {
		t.Fatal(err)
	}
	var stopped atomic.Bool
	preparePair = func(context.Context, pair.PrepareOpts) (pair.Env, error) {
		return pair.Env{
			ID:       "aaaaaaaaaaaa",
			Baseline: pair.Side{Gateway: u.Host},
			Patch:    pair.Side{Gateway: u.Host},
		}, nil
	}
	startPair = func(context.Context, pair.Env) error { return nil }
	stopPair = func(context.Context, pair.Env) error {
		stopped.Store(true)
		return nil
	}
	var out strings.Builder
	err = RunExperiment(t.Context(), []string{
		"-api", api.URL, "-fixture", "-n", "1", "-dir", shopSnapshotDir(t),
	}, strings.NewReader("diff --git a/x b/x\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "status=up") || !strings.Contains(got, "prepared") {
		t.Fatalf("%s", got)
	}
	if !strings.Contains(got, "overall=match") {
		t.Fatalf("%s", got)
	}
	if strings.Contains(got, "overall=pass") || strings.Contains(got, "verdict=pass") {
		t.Fatalf("%s", got)
	}
	if !stopped.Load() {
		t.Fatal("must tear down")
	}
}

func TestRunExperimentLocalEnvTearsDownOnStartFailure(t *testing.T) {
	pairHookMu.Lock()
	defer pairHookMu.Unlock()
	origP, origU, origD := preparePair, startPair, stopPair
	t.Cleanup(func() {
		preparePair, startPair, stopPair = origP, origU, origD
	})
	api := impactAPI(t, impact.Report{
		Files:  []string{"a.go"},
		Direct: []impact.Finding{{Name: "F"}},
	})
	var stopped atomic.Bool
	preparePair = func(context.Context, pair.PrepareOpts) (pair.Env, error) {
		return pair.Env{ID: "aaaaaaaaaaaa"}, nil
	}
	startPair = func(context.Context, pair.Env) error {
		return errors.New("compose failed")
	}
	stopPair = func(context.Context, pair.Env) error {
		stopped.Store(true)
		return nil
	}
	err := RunExperiment(t.Context(), []string{
		"-api", api.URL, "-fixture", "-n", "1", "-dir", shopSnapshotDir(t),
	}, strings.NewReader("diff --git a/x b/x\n"), io.Discard)
	if err == nil {
		t.Fatal("expected start failure")
	}
	if !stopped.Load() {
		t.Fatal("must tear down after failed up")
	}
}

func TestRunExperimentWritesEvidenceJSON(t *testing.T) {
	t.Parallel()
	api := impactAPI(t, impact.Report{
		Files:  []string{"internal/payment/handler.go"},
		Direct: []impact.Finding{{Name: "chargeProcessor"}},
	})
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeReplayJSON(w, map[string]any{"status": "ok"})
	}))
	t.Cleanup(gw.Close)
	dir := shopSnapshotDir(t)
	path := filepath.Join(t.TempDir(), "evidence.json")
	var out strings.Builder
	err := RunExperiment(t.Context(), []string{
		"-api", api.URL, "-base", gw.URL, "-patch", gw.URL, "-fixture", "-n", "1", "-out", path, "-dir", dir,
	}, strings.NewReader("diff --git a/x b/x\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "wrote") {
		t.Fatalf("%s", out.String())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sku-widget") || strings.Contains(string(raw), `"validated": true`) {
		t.Fatalf("bodies or validated true: %s", raw)
	}
	var md strings.Builder
	if err := RunReport(t.Context(), []string{path}, &md); err != nil {
		t.Fatal(err)
	}
	got := md.String()
	if !strings.Contains(got, "overall: match") || !strings.Contains(got, "validated: false") || !strings.Contains(got, "not validated") {
		t.Fatalf("%s", got)
	}
	if strings.Contains(got, "overall: pass") {
		t.Fatalf("%s", got)
	}
}

func TestRunExperimentWritesIncompleteEvidence(t *testing.T) {
	t.Parallel()
	api := impactAPI(t, impact.Report{
		Files:  []string{"a.go"},
		Direct: []impact.Finding{{Name: "F"}},
	})
	base := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeReplayJSON(w, map[string]any{"status": "ok"})
	}))
	t.Cleanup(base.Close)
	down := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	down.Close()
	path := filepath.Join(t.TempDir(), "evidence.json")
	err := RunExperiment(t.Context(), []string{
		"-api", api.URL, "-base", base.URL, "-patch", down.URL, "-fixture", "-n", "1", "-out", path, "-dir", shopSnapshotDir(t),
	}, strings.NewReader("diff --git a/x b/x\n"), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("down patch must be incomplete, err=%v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"overall": "incomplete"`) {
		t.Fatalf("%s", raw)
	}
}

func TestRunReportRejectsPassArtifact(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "evidence.json")
	raw := []byte(`{"schema":"aquila.evidence.v1","validated":false,"result":{"overall":"pass"}}`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	err := RunReport(t.Context(), []string{path}, io.Discard)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunReportRequiresPath(t *testing.T) {
	t.Parallel()
	err := RunReport(t.Context(), nil, io.Discard)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunExperimentRecordsRun(t *testing.T) {
	t.Parallel()
	api, store := experimentAPI(t, impact.Report{
		Files:  []string{"internal/payment/handler.go"},
		Direct: []impact.Finding{{Name: "chargeProcessor"}},
	})
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeReplayJSON(w, map[string]any{"status": "ok"})
	}))
	t.Cleanup(gw.Close)
	var out strings.Builder
	err := RunExperiment(t.Context(), []string{
		"-api", api.URL, "-base", gw.URL, "-patch", gw.URL, "-fixture", "-n", "1", "-dir", shopSnapshotDir(t),
	}, strings.NewReader("diff --git a/x b/x\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "stored") || strings.Contains(got, "unrecorded") {
		t.Fatalf("%s", got)
	}
	if strings.Contains(got, "overall=pass") {
		t.Fatalf("%s", got)
	}
	listed, err := store.List(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Validated || listed[0].Overall != replay.VerdictMatch {
		t.Fatalf("%+v", listed)
	}
}

func TestRunRunsImportAndGet(t *testing.T) {
	t.Parallel()
	api, store := experimentAPI(t, impact.Report{})
	path := filepath.Join(t.TempDir(), "evidence.json")
	art := evidence.Build(evidence.Input{
		Now:      time.Date(2026, 9, 22, 20, 0, 0, 0, time.UTC),
		Baseline: "http://127.0.0.1:18180",
		Patch:    "http://127.0.0.1:18280",
		Workload: replay.Workload{Steps: []replay.Step{{Method: "GET", Path: "/healthz"}}},
		Impact:   impact.Report{Files: []string{"a.go"}},
		Result:   plan.Evidence{Overall: replay.VerdictDiffer, Notes: []string{"not validated"}},
	})
	raw, err := evidence.Marshal(art)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := RunRuns(t.Context(), []string{"-api", api.URL, "-f", path}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "stored") || strings.Contains(out.String(), "overall=pass") {
		t.Fatalf("%s", out.String())
	}
	listed, err := store.List(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("%+v", listed)
	}
	var listOut strings.Builder
	if err := RunRuns(t.Context(), []string{"-api", api.URL}, &listOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listOut.String(), listed[0].ID) || !strings.Contains(listOut.String(), "overall=differ") {
		t.Fatalf("%s", listOut.String())
	}
	var getOut strings.Builder
	if err := RunRuns(t.Context(), []string{"-api", api.URL, listed[0].ID}, &getOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(getOut.String(), "overall=differ") || strings.Contains(getOut.String(), "overall=pass") {
		t.Fatalf("%s", getOut.String())
	}
}

func TestRunAskHitsCheckout(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/graph":
			writeTestJSON(w, graph.Snapshot{
				Services: []graph.Service{{Name: "checkout"}, {Name: "payment"}},
				Edges:    []graph.Edge{{From: "checkout", To: "payment", Provenance: graph.ProvenanceObservedParent}},
				Paths:    []graph.Path{{Services: []string{"checkout", "payment"}, Provenance: graph.ProvenanceObservedParent}},
			})
		case "/v1/source":
			writeTestJSON(w, source.Snapshot{Module: "example.com/shop"})
		case "/v1/locate":
			writeTestJSON(w, locate.Snapshot{
				Bindings: []locate.Binding{{ServiceName: "payment", SourceName: "authorize", File: "internal/payment/handler.go"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	var out, errBuf strings.Builder
	err := RunAsk(t.Context(), []string{"-api", srv.URL, "why", "is", "checkout", "slow"}, strings.NewReader(""), &out, &errBuf)
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "checkout") || !strings.Contains(got, "facts only") {
		t.Fatalf("%s", got)
	}
	if strings.Contains(got, "overall=pass") {
		t.Fatalf("%s", got)
	}
}

func TestRunAskRequiresQuestion(t *testing.T) {
	t.Parallel()
	err := RunAsk(t.Context(), []string{"-dir", t.TempDir()}, strings.NewReader(""), io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "question required") {
		t.Fatalf("%v", err)
	}
}

func TestRunPatchWritesCandidate(t *testing.T) {
	t.Parallel()
	shop := filepath.Clean(filepath.Join("..", "..", "examples", "shop"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/spans" {
			writeTestJSON(w, map[string]any{"spans": []any{}})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	raw, err := os.ReadFile(filepath.Join("..", "pair", "testdata", "d1.diff"))
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	err = RunPatch(t.Context(), []string{"-api", srv.URL, "-dir", shop}, bytes.NewReader(raw), &out)
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "svcclient.Shared()") || !strings.Contains(got, "not applied") {
		t.Fatalf("%s", got)
	}
}

func TestRunPatchFromControlPlaneDir(t *testing.T) {
	t.Parallel()
	root := filepath.Clean(filepath.Join("..", ".."))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/impact":
			writeTestJSON(w, impact.Report{Files: []string{"internal/payment/handler.go"}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	raw, err := os.ReadFile(filepath.Join("..", "pair", "testdata", "d1.diff"))
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	err = RunPatch(t.Context(), []string{"-api", srv.URL, "-dir", root}, bytes.NewReader(raw), &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "svcclient.Shared()") {
		t.Fatalf("%s", out.String())
	}
}

func TestRunJobRequiresGateways(t *testing.T) {
	t.Parallel()
	err := RunJob(t.Context(), []string{"-fixture"}, strings.NewReader("diff --git a/x b/x\n"), io.Discard)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunRunsRejectsInvalidID(t *testing.T) {
	t.Parallel()
	err := RunRuns(t.Context(), []string{"-api", "http://127.0.0.1:8080", "../spans"}, io.Discard)
	if err == nil {
		t.Fatal("expected error")
	}
}

func experimentAPI(t *testing.T, rep impact.Report) (*httptest.Server, *runs.Memory) {
	t.Helper()
	store := runs.NewMemory()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/impact":
			writeTestJSON(w, rep)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/runs":
			raw, err := io.ReadAll(io.LimitReader(r.Body, int64(evidence.MaxBytes)+1))
			if err != nil {
				http.Error(w, "invalid artifact", http.StatusBadRequest)
				return
			}
			a, err := evidence.Decode(bytes.NewReader(raw))
			if err != nil {
				http.Error(w, "invalid artifact", http.StatusBadRequest)
				return
			}
			rec, err := store.Insert(r.Context(), runs.Record{Artifact: a})
			if err != nil {
				http.Error(w, "store failed", http.StatusInternalServerError)
				return
			}
			writeTestJSON(w, map[string]any{
				"id": rec.ID, "overall": rec.Overall, "validated": false,
				"artifact_digest": rec.ArtifactDigest, "baseline_sha": rec.BaselineSHA, "dirty": rec.Dirty,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/runs":
			list, err := store.List(r.Context(), runs.DefaultList)
			if err != nil {
				http.Error(w, "store failed", http.StatusInternalServerError)
				return
			}
			out := make([]map[string]any, 0, len(list))
			for _, rec := range list {
				out = append(out, map[string]any{
					"id": rec.ID, "overall": rec.Overall, "validated": false,
					"artifact_digest": rec.ArtifactDigest, "baseline_sha": rec.BaselineSHA, "dirty": rec.Dirty,
				})
			}
			writeTestJSON(w, map[string]any{"runs": out})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/runs/"):
			id := strings.TrimPrefix(r.URL.Path, "/v1/runs/")
			rec, err := store.Get(r.Context(), id)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			writeTestJSON(w, map[string]any{
				"id": rec.ID, "overall": rec.Overall, "validated": false,
				"artifact_digest": rec.ArtifactDigest, "baseline_sha": rec.BaselineSHA, "dirty": rec.Dirty,
				"artifact": rec.Artifact,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, store
}

func impactAPI(t *testing.T, rep impact.Report) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/spans":
			writeTestJSON(w, map[string]any{"spans": []any{}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/impact":
			writeTestJSON(w, rep)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}
