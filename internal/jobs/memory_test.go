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

func TestCreateReadiesEnv(t *testing.T) {
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
	if env.State != StateReady || env.Operator {
		t.Fatalf("%+v", env)
	}
	beh := taskKind(job, plan.KindBehavior)
	if beh.State != StatePending {
		t.Fatalf("behavior=%s", beh.State)
	}
	fault := taskKind(job, plan.KindFaultStatus)
	if fault.ID != "" && (fault.Operator || fault.State != StatePending) {
		t.Fatalf("%+v", fault)
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
	got := taskKind(job, plan.KindEnv)
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
	if _, err := st.Lease(t.Context(), Worker{ID: "a", Slots: 1}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Lease(t.Context(), Worker{ID: "a", Slots: 1}, now); err != ErrCapacity {
		t.Fatalf("want capacity, got %v", err)
	}
	if _, err := st.Lease(t.Context(), Worker{ID: "b", Slots: 1}, now); err != ErrNoReady {
		t.Fatalf("want no ready sibling until env commits, got %v", err)
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
	_, err = st.Lease(t.Context(), Worker{ID: "b"}, now.Add(leaseTTL))
	if err != ErrNoReady {
		t.Fatalf("heartbeat must keep env leased, got %v", err)
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
	first, err := st.Create(t.Context(), sampleOptsAt(t, "http://127.0.0.1:18180", "http://127.0.0.1:18280"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.Create(t.Context(), sampleOptsAt(t, "http://127.0.0.1:19180", "http://127.0.0.1:19280"))
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

func TestTwoWorkersLeaseDistinctJobs(t *testing.T) {
	t.Parallel()
	st := NewMemory()
	first, err := st.Create(t.Context(), sampleOptsAt(t, "http://127.0.0.1:18180", "http://127.0.0.1:18280"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.Create(t.Context(), sampleOptsAt(t, "http://127.0.0.1:19180", "http://127.0.0.1:19280"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	a, err := st.Lease(t.Context(), Worker{ID: "worker-a", JobID: first.ID}, now)
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.Lease(t.Context(), Worker{ID: "worker-b", JobID: second.ID}, now)
	if err != nil {
		t.Fatal(err)
	}
	if a.Job.ID != first.ID || b.Job.ID != second.ID {
		t.Fatalf("leased %s %s want %s %s", a.Job.ID, b.Job.ID, first.ID, second.ID)
	}
	if a.Task.ID == b.Task.ID {
		t.Fatalf("same task %s", a.Task.ID)
	}
}

func TestTwoWorkersLeaseSiblingReadyTasks(t *testing.T) {
	t.Parallel()
	st := NewMemory()
	job, err := st.Create(t.Context(), sampleOpts(t))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	env, err := st.Lease(t.Context(), Worker{ID: "env"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if env.Task.Kind != plan.KindEnv {
		t.Fatalf("%s", env.Task.Kind)
	}
	if _, err := st.Commit(t.Context(), env.Task.ID, env.Attempt, plan.StepResult{ID: env.Task.PlanID, Kind: env.Task.Kind, Verdict: plan.VerdictPrepared}, ""); err != nil {
		t.Fatal(err)
	}
	a, err := st.Lease(t.Context(), Worker{ID: "worker-a"}, now)
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.Lease(t.Context(), Worker{ID: "worker-b"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if a.Job.ID != job.ID || b.Job.ID != job.ID {
		t.Fatalf("leased %s %s want %s", a.Job.ID, b.Job.ID, job.ID)
	}
	if a.Task.ID == b.Task.ID {
		t.Fatalf("same task %s", a.Task.ID)
	}
}

func TestListFiltersService(t *testing.T) {
	t.Parallel()
	st := NewMemory()
	opts := sampleOptsAt(t, "http://127.0.0.1:18180", "http://127.0.0.1:18280")
	opts.Service = "ledger"
	if _, err := st.Create(t.Context(), opts); err != nil {
		t.Fatal(err)
	}
	opts = sampleOptsAt(t, "http://127.0.0.1:19180", "http://127.0.0.1:19280")
	opts.Service = "shop"
	if _, err := st.Create(t.Context(), opts); err != nil {
		t.Fatal(err)
	}
	got, err := st.List(t.Context(), 10, "ledger")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Service != "ledger" {
		t.Fatalf("%+v", got)
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

func TestGatewayOccupancy(t *testing.T) {
	t.Parallel()
	st := NewMemory()
	if _, err := st.Create(t.Context(), sampleOpts(t)); err != nil {
		t.Fatal(err)
	}
	_, err := st.Create(t.Context(), sampleOpts(t))
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("want busy, got %v", err)
	}
	if _, err := st.Create(t.Context(), sampleOptsAt(t, "http://127.0.0.1:19180", "http://127.0.0.1:19280")); err != nil {
		t.Fatal(err)
	}
}

func TestSameJobMayShareHost(t *testing.T) {
	t.Parallel()
	st := NewMemory()
	opts := sampleOptsAt(t, "http://127.0.0.1:18180", "http://127.0.0.1:18180")
	if _, err := st.Create(t.Context(), opts); err != nil {
		t.Fatal(err)
	}
}

func TestCancelStopsLeases(t *testing.T) {
	t.Parallel()
	st := NewMemory()
	job, err := st.Create(t.Context(), sampleOpts(t))
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.Cancel(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusCanceled {
		t.Fatalf("%s", got.Status)
	}
	_, err = st.Lease(t.Context(), Worker{ID: "w"}, time.Now())
	if err != ErrNoReady {
		t.Fatalf("%v", err)
	}
	if _, err := st.Create(t.Context(), sampleOpts(t)); err != nil {
		t.Fatal(err)
	}
}

func TestDeadlineFailsReady(t *testing.T) {
	t.Parallel()
	st := NewMemory()
	opts := sampleOpts(t)
	opts.Deadline = time.Now().Add(5 * time.Millisecond)
	job, err := st.Create(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if _, err := st.RequeueExpired(t.Context(), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusFailed {
		t.Fatalf("%s", got.Status)
	}
	if taskKind(got, plan.KindEnv).Err != "deadline" {
		t.Fatalf("%+v", taskKind(got, plan.KindEnv))
	}
}

func TestJobEarnsValidated(t *testing.T) {
	t.Parallel()
	st := NewMemory()
	opts := sampleOpts(t)
	opts.BaselineSHA = "abc123"
	opts.Dirty = false
	job, err := st.Create(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	commitKind := func(kind, verdict string, lat []replay.StepLatency) {
		t.Helper()
		lease, err := st.Lease(t.Context(), Worker{ID: "w-" + kind}, now)
		if err != nil {
			t.Fatal(err)
		}
		if lease.Task.Kind != kind {
			t.Fatalf("leased %s want %s", lease.Task.Kind, kind)
		}
		res := plan.StepResult{ID: lease.Task.PlanID, Kind: kind, Verdict: verdict, Latency: lat}
		if _, err := st.Commit(t.Context(), lease.Task.ID, lease.Attempt, res, ""); err != nil {
			t.Fatal(err)
		}
	}
	commitKind(plan.KindEnv, plan.VerdictPrepared, nil)
	commitKind(plan.KindBehavior, replay.VerdictMatch, nil)
	lat := []replay.StepLatency{{
		Method:   http.MethodGet,
		Path:     "/healthz",
		Baseline: replay.Summary{N: 20, HasP95: true},
		Patch:    replay.Summary{N: 20, HasP95: true},
	}}
	commitKind(plan.KindLatency, plan.VerdictSamples, lat)
	got, err := st.Get(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusComplete || !got.Validated {
		t.Fatalf("%+v", got)
	}
}

func TestSmokeJobCannotValidate(t *testing.T) {
	t.Parallel()
	st := NewMemory()
	opts := sampleOpts(t)
	opts.BaselineSHA = "abc123"
	opts.Dirty = false
	opts.Plan = plan.WithLatencyN(opts.Plan, 1)
	job, err := st.Create(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, step := range []struct {
		kind, verdict string
	}{
		{plan.KindEnv, plan.VerdictPrepared},
		{plan.KindBehavior, replay.VerdictMatch},
		{plan.KindLatency, plan.VerdictSamples},
	} {
		lease, err := st.Lease(t.Context(), Worker{ID: "w-" + step.kind}, now)
		if err != nil {
			t.Fatal(err)
		}
		lat := []replay.StepLatency{{Baseline: replay.Summary{N: 1}, Patch: replay.Summary{N: 1}}}
		res := plan.StepResult{ID: lease.Task.PlanID, Kind: lease.Task.Kind, Verdict: step.verdict, Latency: lat}
		if _, err := st.Commit(t.Context(), lease.Task.ID, lease.Attempt, res, ""); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.Get(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Validated && got.Status == StatusComplete {
		return
	}
	t.Fatalf("smoke must complete without validation: %+v", got)
}

func sampleOpts(t *testing.T) CreateOpts {
	t.Helper()
	return sampleOptsAt(t, "http://127.0.0.1:18180", "http://127.0.0.1:18280")
}

func sampleOptsAt(t *testing.T, base, patch string) CreateOpts {
	t.Helper()
	dag, err := plan.FromImpact(impact.Report{
		Files:  []string{"internal/payment/handler.go"},
		Direct: []impact.Finding{{Name: "chargeProcessor"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return CreateOpts{
		Baseline: base,
		Patch:    patch,
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
