package worker

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sumedhaerram/aquila/internal/action"
	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/jobs"
	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
)

func TestExecuteBehaviorMatch(t *testing.T) {
	t.Parallel()
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(gw.Close)
	st := jobs.NewMemory()
	job, err := st.Create(t.Context(), jobs.CreateOpts{
		Baseline: gw.URL,
		Patch:    gw.URL,
		Workload: replay.Workload{Steps: []replay.Step{{Method: http.MethodGet, Path: "/"}}},
		Plan:     mustPlan(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := st.Lease(t.Context(), jobs.Worker{ID: "w1"}, nowUTC())
	if err != nil {
		t.Fatal(err)
	}
	if lease.Task.Kind != plan.KindBehavior {
		t.Fatalf("%s", lease.Task.Kind)
	}
	res, err := Execute(t.Context(), lease)
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != replay.VerdictMatch {
		t.Fatalf("%+v", res)
	}
	if _, err := st.Commit(t.Context(), lease.Task.ID, lease.Attempt, res, ""); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Validated {
		t.Fatal("validated")
	}
}

func TestExecuteCacheHit(t *testing.T) {
	t.Parallel()
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(gw.Close)
	st := jobs.NewMemory()
	if _, err := st.Create(t.Context(), jobs.CreateOpts{
		Baseline: gw.URL,
		Patch:    gw.URL,
		Workload: replay.Workload{Steps: []replay.Step{{Method: http.MethodGet, Path: "/"}}},
		Plan:     mustPlan(t),
	}); err != nil {
		t.Fatal(err)
	}
	cache, err := action.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lease, err := st.Lease(t.Context(), jobs.Worker{ID: "w1"}, nowUTC())
	if err != nil {
		t.Fatal(err)
	}
	first, err := ExecuteWith(t.Context(), lease, cache)
	if err != nil {
		t.Fatal(err)
	}
	gw.Close()
	second, err := ExecuteWith(t.Context(), lease, cache)
	if err != nil {
		t.Fatal(err)
	}
	if first.Verdict != second.Verdict || second.Verdict != replay.VerdictMatch {
		t.Fatalf("%+v %+v", first, second)
	}
}

func TestOnceEmptyQueue(t *testing.T) {
	t.Parallel()
	err := Once(t.Context(), jobs.NewMemory(), "w1")
	if err != jobs.ErrNoReady {
		t.Fatalf("%v", err)
	}
}

func mustPlan(t *testing.T) plan.DAG {
	t.Helper()
	dag, err := plan.FromImpact(impact.Report{
		Files:  []string{"a.go"},
		Direct: []impact.Finding{{Name: "F"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan.WithLatencyN(dag, 1)
}
