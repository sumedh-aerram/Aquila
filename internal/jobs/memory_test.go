package jobs

import (
	"errors"
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
	a, err := st.Lease(t.Context(), Worker{ID: "worker-a"}, now)
	if err != nil {
		t.Fatal(err)
	}
	expired := now.Add(leaseTTL + time.Second)
	if _, err := st.RequeueExpired(t.Context(), expired); err != nil {
		t.Fatal(err)
	}
	b, err := st.Lease(t.Context(), Worker{ID: "worker-b"}, expired)
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
	_, err := st.Lease(t.Context(), Worker{ID: "w"}, time.Now())
	if err != ErrNoReady {
		t.Fatalf("%v", err)
	}
}

func TestTwoWorkersLeaseDistinctTasks(t *testing.T) {
	t.Parallel()
	st := NewMemory()
	if _, err := st.Create(t.Context(), sampleOpts(t)); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	a, err := st.Lease(t.Context(), Worker{ID: "a", Slots: 1}, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.Lease(t.Context(), Worker{ID: "a", Slots: 1}, now)
	if err != ErrCapacity {
		t.Fatalf("want capacity, got %v", err)
	}
	b, err := st.Lease(t.Context(), Worker{ID: "b", Slots: 1}, now)
	if err != nil {
		t.Fatal(err)
	}
	if a.Task.ID == b.Task.ID {
		t.Fatal("workers must not share a task")
	}
	if a.Attempt == b.Attempt {
		t.Fatal("attempts must differ")
	}
}

func TestHeartbeatExtendsLease(t *testing.T) {
	t.Parallel()
	st := NewMemory()
	if _, err := st.Create(t.Context(), sampleOpts(t)); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	a, err := st.Lease(t.Context(), Worker{ID: "a"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Heartbeat(t.Context(), a.Task.ID, a.Attempt, "a", now.Add(leaseTTL-time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RequeueExpired(t.Context(), now.Add(leaseTTL)); err != nil {
		t.Fatal(err)
	}
	b, err := st.Lease(t.Context(), Worker{ID: "b"}, now.Add(leaseTTL))
	if err != nil {
		t.Fatal(err)
	}
	if b.Task.ID == a.Task.ID {
		t.Fatal("heartbeat must keep worker a's task")
	}
	if _, err := st.RequeueExpired(t.Context(), now.Add(2*leaseTTL+time.Second)); err != nil {
		t.Fatal(err)
	}
	c, err := st.Lease(t.Context(), Worker{ID: "c"}, now.Add(2*leaseTTL+time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if c.Task.ID != a.Task.ID {
		t.Fatalf("want requeued task %s, got %s", a.Task.ID, c.Task.ID)
	}
	if c.Attempt == a.Attempt {
		t.Fatal("attempts must differ after expiry")
	}
	res := plan.StepResult{ID: a.Task.PlanID, Kind: a.Task.Kind, Verdict: replay.VerdictMatch}
	if _, err := st.Commit(t.Context(), a.Task.ID, a.Attempt, res, ""); err == nil {
		t.Fatal("stale A after expiry must be rejected")
	}
}

func TestControllerRestartKeepsJob(t *testing.T) {
	t.Parallel()
	st := NewMemory()
	job, err := st.Create(t.Context(), sampleOpts(t))
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.Get(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != job.ID || got.Validated {
		t.Fatalf("%+v", got)
	}
}

func TestLeaseJobIDSkipsOtherJobs(t *testing.T) {
	t.Parallel()
	st := NewMemory()
	first, err := st.Create(t.Context(), sampleOpts(t))
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.Create(t.Context(), sampleOpts(t))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	got, err := st.Lease(t.Context(), Worker{ID: "w", JobID: second.ID}, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Job.ID != second.ID {
		t.Fatalf("leased %s want %s (first=%s)", got.Job.ID, second.ID, first.ID)
	}
}

func TestLeaseInvalidJobID(t *testing.T) {
	t.Parallel()
	st := NewMemory()
	_, err := st.Lease(t.Context(), Worker{ID: "w", JobID: "not-hex"}, time.Now())
	if !errors.Is(err, ErrInvalidID) {
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
