package plan

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/replay"
)

func TestExecuteMatchIsNotPass(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"status": "ok"})
	}))
	t.Cleanup(srv.Close)
	dag := mustDAG(t, impact.Report{Files: []string{"a.go"}, Direct: []impact.Finding{{Name: "F"}}})
	dag = WithLatencyN(dag, 2)
	w := replay.Workload{Steps: []replay.Step{{Method: http.MethodGet, Path: "/healthz"}}}
	ev, err := Execute(t.Context(), dag, srv.URL, srv.URL, w)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Overall != replay.VerdictMatch {
		t.Fatalf("%+v", ev)
	}
	if ev.Overall == "pass" || ev.Overall == "PASS" || ev.Overall == "validated" {
		t.Fatal("must not report pass")
	}
	if stepVerdict(ev, KindEnv) != VerdictSkipped {
		t.Fatal("env must stay operator")
	}
	if stepVerdict(ev, KindLatency) != VerdictSamples {
		t.Fatalf("latency: %+v", ev)
	}
}

func TestExecuteLocalEnvIsPreparedNotPass(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"status": "ok"})
	}))
	t.Cleanup(srv.Close)
	dag := mustDAG(t, impact.Report{Files: []string{"a.go"}, Direct: []impact.Finding{{Name: "F"}}})
	dag = WithLocalEnv(WithLatencyN(dag, 1))
	w := replay.Workload{Steps: []replay.Step{{Method: http.MethodGet, Path: "/healthz"}}}
	ev, err := Execute(t.Context(), dag, srv.URL, srv.URL, w)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Overall != replay.VerdictMatch {
		t.Fatalf("%+v", ev)
	}
	if stepVerdict(ev, KindEnv) != VerdictPrepared {
		t.Fatalf("env: %+v", ev)
	}
	if ev.Overall == "pass" || ev.Overall == "validated" {
		t.Fatal("must not report pass")
	}
}

func TestExecuteJSONDifference(t *testing.T) {
	t.Parallel()
	base := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"status": "ok"})
	}))
	t.Cleanup(base.Close)
	patch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"status": "ok", "debug": true})
	}))
	t.Cleanup(patch.Close)
	dag := mustDAG(t, impact.Report{
		Files:   []string{"a.go"},
		Direct:  []impact.Finding{{Name: "F"}},
		Runtime: []impact.Finding{{Path: "gateway -> payment", Reason: "observed_path"}},
	})
	dag = WithLatencyN(dag, 1)
	w := replay.Workload{Steps: []replay.Step{{Method: http.MethodGet, Path: "/healthz"}}}
	ev, err := Execute(t.Context(), dag, base.URL, patch.URL, w)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Overall != replay.VerdictDiffer {
		t.Fatalf("extra json field must differ: %+v", ev)
	}
	if stepVerdict(ev, KindFaultStatus) != VerdictSkipped {
		t.Fatal("fault must not be auto-executed as a patch verdict")
	}
}

func TestExecuteIncompleteWhenPatchDown(t *testing.T) {
	t.Parallel()
	base := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"status": "ok"})
	}))
	t.Cleanup(base.Close)
	down := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	down.Close()
	dag := mustDAG(t, impact.Report{Files: []string{"a.go"}, Direct: []impact.Finding{{Name: "F"}}})
	dag = WithLatencyN(dag, 1)
	w := replay.Workload{Steps: []replay.Step{{Method: http.MethodGet, Path: "/healthz"}}}
	ev, err := Execute(t.Context(), dag, base.URL, down.URL, w)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Overall != replay.VerdictIncomplete {
		t.Fatalf("down patch must be incomplete, got %+v", ev)
	}
}

func TestExecuteRejectsFileTarget(t *testing.T) {
	t.Parallel()
	dag := mustDAG(t, impact.Report{Files: []string{"a.go"}, Direct: []impact.Finding{{Name: "F"}}})
	w := replay.Workload{Steps: []replay.Step{{Method: http.MethodGet, Path: "/healthz"}}}
	_, err := Execute(t.Context(), dag, "file:///etc/passwd", "http://127.0.0.1:18180", w)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExecuteEmptyWorkload(t *testing.T) {
	t.Parallel()
	dag := mustDAG(t, impact.Report{Files: []string{"a.go"}, Direct: []impact.Finding{{Name: "F"}}})
	_, err := Execute(t.Context(), dag, "http://127.0.0.1:18180", "http://127.0.0.1:18280", replay.Workload{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFasterPatchDoesNotDiffer(t *testing.T) {
	t.Parallel()
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"status": "ok"})
	}))
	t.Cleanup(fast.Close)
	dag := mustDAG(t, impact.Report{Files: []string{"a.go"}, Direct: []impact.Finding{{Name: "F"}}})
	dag = WithLatencyN(dag, 2)
	w := replay.Workload{Steps: []replay.Step{{Method: http.MethodGet, Path: "/healthz"}}}
	ev, err := Execute(t.Context(), dag, fast.URL, fast.URL, w)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Overall != replay.VerdictMatch {
		t.Fatalf("%+v", ev)
	}
	if stepVerdict(ev, KindBehavior) == replay.VerdictDiffer {
		t.Fatal("latency must not vote as a behavior differ")
	}
}

func mustDAG(t *testing.T, rep impact.Report) DAG {
	t.Helper()
	dag, err := FromImpact(rep)
	if err != nil {
		t.Fatal(err)
	}
	return dag
}

func stepVerdict(ev Evidence, kind string) string {
	for _, s := range ev.Steps {
		if s.Kind == kind {
			return s.Verdict
		}
	}
	return ""
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
