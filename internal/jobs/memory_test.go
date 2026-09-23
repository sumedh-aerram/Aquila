package jobs

import (
	"net/http"
	"testing"
	"time"

	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
)

func TestCreateSkipsOperatorAndReadiesBehavior(t *testing.T) {
	t.Parallel()
	st := NewMemory()
	job, err := st.Create(t.Context(), sampleOpts(t))
	if err != nil {
		t.Fatal(err)
	}
	if job.Validated {
		t.Fatal("validated")
	}
	if job.Status != StatusRunning {
		t.Fatalf("status=%s", job.Status)
	}
	env := taskKind(job, plan.KindEnv)
	if env.State != StateSkipped || !env.Operator {
		t.Fatalf("%+v", env)
	}
	beh := taskKind(job, plan.KindBehavior)
	if beh.State != StateReady {
		t.Fatalf("behavior=%s", beh.State)
	}
}

func TestStaleAttemptCannotCommit(t *testing.T) {
	t.Parallel()
	st := NewMemory()
	if _, err := st.Create(t.Context(), sampleOpts(t)); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	a, err := st.Lease(t.Context(), "worker-a", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.RequeueExpired(t.Context(), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	b, err := st.Lease(t.Context(), "worker-b", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if a.Attempt == b.Attempt {
		t.Fatal("attempts must differ")
	}
	res := plan.StepResult{ID: a.Task.PlanID, Kind: a.Task.Kind, Verdict: replay.VerdictMatch}
	if _, err := st.Commit(t.Context(), a.Task.ID, a.Attempt, res, ""); err == nil {
		t.Fatal("stale A must be rejected")
	}
	if _, err := st.Commit(t.Context(), b.Task.ID, b.Attempt, res, ""); err != nil {
		t.Fatal(err)
	}
	job, err := st.Get(t.Context(), a.Job.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := taskKind(job, plan.KindBehavior)
	if got.State != StateSucceeded {
		t.Fatalf("%+v", got)
	}
	if job.Validated {
		t.Fatal("validated")
	}
}

func TestLeaseEmpty(t *testing.T) {
	t.Parallel()
	st := NewMemory()
	_, err := st.Lease(t.Context(), "w", time.Now())
	if err != ErrNoReady {
		t.Fatalf("%v", err)
	}
}

func sampleOpts(t *testing.T) CreateOpts {
	t.Helper()
	dag, err := plan.FromImpact(impact.Report{
		Files:  []string{"internal/payment/handler.go"},
		Direct: []impact.Finding{{Name: "chargeProcessor"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return CreateOpts{
		Baseline: "http://127.0.0.1:18180",
		Patch:    "http://127.0.0.1:18280",
		Workload: replay.Workload{Steps: []replay.Step{{Method: http.MethodGet, Path: "/healthz"}}},
		Plan:     dag,
	}
}

func taskKind(j Job, kind string) Task {
	for _, t := range j.Tasks {
		if t.Kind == kind {
			return t
		}
	}
	return Task{}
}
