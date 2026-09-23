package action

import (
	"testing"

	"github.com/sumedhaerram/aquila/internal/jobs"
	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
)

func TestCacheHit(t *testing.T) {
	t.Parallel()
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lease := jobs.Lease{
		Job: jobs.Job{
			Baseline: "http://127.0.0.1:18180",
			Patch:    "http://127.0.0.1:18280",
			Workload: []jobs.Step{{Method: "GET", Path: "/invoice"}},
		},
		Task: jobs.Task{Kind: plan.KindBehavior},
		N:    1,
	}
	key := FromLease(lease)
	res := plan.StepResult{ID: "behavior", Kind: plan.KindBehavior, Verdict: replay.VerdictMatch}
	if err := c.Put(t.Context(), key, res); err != nil {
		t.Fatal(err)
	}
	got, ok, err := c.Get(t.Context(), key)
	if err != nil || !ok {
		t.Fatalf("%v ok=%v", err, ok)
	}
	if got.Verdict != replay.VerdictMatch {
		t.Fatalf("%+v", got)
	}
}

func TestCacheSkipsIncomplete(t *testing.T) {
	t.Parallel()
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := Key{Kind: "behavior", Baseline: "http://127.0.0.1:1", Patch: "http://127.0.0.1:2", N: 1}
	if err := c.Put(t.Context(), key, plan.StepResult{Verdict: "incomplete"}); err != nil {
		t.Fatal(err)
	}
	_, ok, err := c.Get(t.Context(), key)
	if err != nil || ok {
		t.Fatalf("incomplete must not hit, ok=%v err=%v", ok, err)
	}
}
