package action

import (
	"testing"

	"github.com/sumedhaerram/aquila/internal/gitrev"
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
	key := Key{Kind: "behavior", Scope: "job:test", N: 1}
	if err := c.Put(t.Context(), key, plan.StepResult{Verdict: "incomplete"}); err != nil {
		t.Fatal(err)
	}
	_, ok, err := c.Get(t.Context(), key)
	if err != nil || ok {
		t.Fatalf("incomplete must not hit, ok=%v err=%v", ok, err)
	}
}

func TestCacheSkipsNondeterministicKinds(t *testing.T) {
	for _, kind := range []string{plan.KindEnv, plan.KindLatency, plan.KindConcurrency} {
		t.Run(kind, func(t *testing.T) {
			c, err := Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			k := Key{V: keyVersion,
				Scope: "job:job", Kind: kind}
			if err := c.Put(t.Context(), k, plan.StepResult{Kind: kind, Verdict: plan.VerdictPrepared}); err != nil {
				t.Fatal(err)
			}
			if _, ok, err := c.Get(t.Context(), k); err != nil || ok {
				t.Fatalf("hit=%t err=%v", ok, err)
			}
		})
	}
}

func TestCacheTreeKeyed(t *testing.T) {
	t.Parallel()
	lease := jobs.Lease{
		Job: jobs.Job{
			ID: "first",
			BaselineRev: gitrev.Revision{
				Tree:   "base",
				Source: gitrev.SourceBuilt,
			},
			PatchRev: gitrev.Revision{
				Tree:   "patch",
				Source: gitrev.SourceBuilt,
			},
		},
		Task: jobs.Task{Kind: plan.KindBehavior}, N: 1,
	}
	cache, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := FromLease(lease)
	if key.Scope != "trees" {
		t.Fatalf("%+v", key)
	}
	if err := cache.Put(t.Context(), key, plan.StepResult{Kind: plan.KindBehavior, Verdict: replay.VerdictMatch}); err != nil {
		t.Fatal(err)
	}
	lease.Job.PatchRev.Tree = "different"
	if _, ok, err := cache.Get(t.Context(), FromLease(lease)); err != nil || ok {
		t.Fatalf("different patch hit=%t err=%v", ok, err)
	}
	lease.Job.PatchRev.Tree = "patch"
	lease.Job.ID = "second"
	if _, ok, err := cache.Get(t.Context(), FromLease(lease)); err != nil || !ok {
		t.Fatalf("same trees hit=%t err=%v", ok, err)
	}
}

func TestCacheDeclaredJobScoped(t *testing.T) {
	t.Parallel()
	lease := jobs.Lease{Job: jobs.Job{
		ID: "first",
		BaselineRev: gitrev.Revision{
			Tree:   "base",
			Source: gitrev.SourceDeclared,
		},
		PatchRev: gitrev.Revision{
			Tree:   "patch",
			Source: gitrev.SourceDeclared,
		},
	}, Task: jobs.Task{Kind: plan.KindBehavior}}
	first := FromLease(lease)
	if first.Scope != "job:first" || first.BaselineTree != "" || first.PatchTree != "" {
		t.Fatalf("%+v", first)
	}
	lease.Job.ID = "second"
	if Digest(first) == Digest(FromLease(lease)) {
		t.Fatal("declared jobs share cache")
	}
}
