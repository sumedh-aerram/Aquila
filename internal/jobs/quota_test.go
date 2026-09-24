package jobs

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
)

func serviceOpts(t *testing.T, service, base, patch string) CreateOpts {
	t.Helper()
	o := sampleOptsAt(t, base, patch)
	o.Service = service
	return o
}

func TestCreateRejectsServiceOverActiveQuota(t *testing.T) {
	t.Parallel()
	st := NewMemory().WithQuota(Quota{ActiveJobs: 1})
	if _, err := st.Create(t.Context(), serviceOpts(t, "ledger", "http://127.0.0.1:18180", "http://127.0.0.1:18280")); err != nil {
		t.Fatal(err)
	}
	_, err := st.Create(t.Context(), serviceOpts(t, "ledger", "http://127.0.0.1:19180", "http://127.0.0.1:19280"))
	if !errors.Is(err, ErrQuota) {
		t.Fatalf("want ErrQuota, got %v", err)
	}
	if _, err := st.Create(t.Context(), serviceOpts(t, "inbox", "http://127.0.0.1:19180", "http://127.0.0.1:19280")); err != nil {
		t.Fatalf("other tenant must not be blocked: %v", err)
	}
}

func TestCanceledJobFreesActiveQuota(t *testing.T) {
	t.Parallel()
	st := NewMemory().WithQuota(Quota{ActiveJobs: 1})
	first, err := st.Create(t.Context(), serviceOpts(t, "ledger", "http://127.0.0.1:18180", "http://127.0.0.1:18280"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Cancel(t.Context(), first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Create(t.Context(), serviceOpts(t, "ledger", "http://127.0.0.1:19180", "http://127.0.0.1:19280")); err != nil {
		t.Fatal(err)
	}
}

func TestCreateOptsJSONKeepsBodies(t *testing.T) {
	t.Parallel()
	in := sampleOpts(t)
	in.Workload = replay.Workload{Steps: []replay.Step{{Method: "POST", Path: "/checkout", Body: []byte(`{"sku":"a"}`)}}}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out CreateOpts
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if got := string(out.Workload.Steps[0].Body); got != `{"sku":"a"}` {
		t.Fatalf("body lost on the wire: %q (%s)", got, raw)
	}
}

func TestLegacyJobWithoutDeadlineExpires(t *testing.T) {
	t.Parallel()
	job := Job{
		Created: time.Now().Add(-2 * DefaultDeadline),
		Status:  StatusRunning,
		Tasks:   []Task{{ID: "t", State: StateReady}},
	}
	if !failDeadline(&job, time.Now()) || job.Status != StatusFailed || job.Tasks[0].Err != "deadline" {
		t.Fatalf("%+v", job)
	}
}

func TestLeasePrefersServiceWithFewerLeases(t *testing.T) {
	t.Parallel()
	st := NewMemory().WithQuota(Quota{LeasedTasks: 8})
	big, err := st.Create(t.Context(), serviceOpts(t, "big", "http://127.0.0.1:18180", "http://127.0.0.1:18280"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	env, err := st.Lease(t.Context(), Worker{ID: "w0"}, now)
	if err != nil || env.Job.ID != big.ID {
		t.Fatalf("lease=%+v err=%v", env, err)
	}
	res := plan.StepResult{ID: env.Task.PlanID, Kind: env.Task.Kind, Verdict: replay.VerdictMatch}
	if _, err := st.Commit(t.Context(), env.Task.ID, env.Attempt, res, ""); err != nil {
		t.Fatal(err)
	}
	small, err := st.Create(t.Context(), serviceOpts(t, "small", "http://127.0.0.1:19180", "http://127.0.0.1:19280"))
	if err != nil {
		t.Fatal(err)
	}

	first, err := st.Lease(t.Context(), Worker{ID: "w1"}, now)
	if err != nil || first.Job.ID != big.ID {
		t.Fatalf("FIFO tie must go to the older job: %+v err=%v", first.Job.ID, err)
	}
	second, err := st.Lease(t.Context(), Worker{ID: "w2"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if second.Job.ID != small.ID {
		t.Fatalf("big already holds a lease; small must go next, got job %s task %s", second.Job.ID, second.Task.Kind)
	}
}

func TestLeaseSkipsServiceAtLeasedCap(t *testing.T) {
	t.Parallel()
	st := NewMemory().WithQuota(Quota{LeasedTasks: 1})
	if _, err := st.Create(t.Context(), serviceOpts(t, "ledger", "http://127.0.0.1:18180", "http://127.0.0.1:18280")); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	env, err := st.Lease(t.Context(), Worker{ID: "w0"}, now)
	if err != nil {
		t.Fatal(err)
	}
	res := plan.StepResult{ID: env.Task.PlanID, Kind: env.Task.Kind, Verdict: replay.VerdictMatch}
	if _, err := st.Commit(t.Context(), env.Task.ID, env.Attempt, res, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Lease(t.Context(), Worker{ID: "w1"}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Lease(t.Context(), Worker{ID: "w2"}, now); !errors.Is(err, ErrNoReady) {
		t.Fatalf("ledger is at its leased cap; want ErrNoReady, got %v", err)
	}
}

func TestCreateOptsJSONKeepsImpacted(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(CreateOpts{Baseline: "http://a", Patch: "http://b", Impacted: []string{"GET /latency"}})
	if err != nil {
		t.Fatal(err)
	}
	var got CreateOpts
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Impacted) != 1 || got.Impacted[0] != "GET /latency" {
		t.Fatalf("impacted lost on the wire: %+v", got.Impacted)
	}
}
