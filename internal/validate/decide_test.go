package validate

import (
	"testing"

	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
)

func TestEarnedRequiresCleanMatchAndFullDAG(t *testing.T) {
	t.Parallel()
	in := earnedInput()
	if !Earned(in) {
		t.Fatal("expected earned")
	}
	in.Dirty = true
	if Earned(in) {
		t.Fatal("dirty")
	}
}

func TestEarnedRejectsDifferAndSmokeN(t *testing.T) {
	t.Parallel()
	in := earnedInput()
	in.Result.Overall = replay.VerdictDiffer
	in.Result.Steps[1].Verdict = replay.VerdictDiffer
	if Earned(in) {
		t.Fatal("differ")
	}
	in = earnedInput()
	in.Plan.Steps[2].N = 1
	in.Result.Steps[2].Latency[0].Baseline.N = 1
	in.Result.Steps[2].Latency[0].Patch.N = 1
	if Earned(in) {
		t.Fatal("n=1")
	}
}

func TestEarnedRejectsSkippedEnv(t *testing.T) {
	t.Parallel()
	in := earnedInput()
	in.Result.Steps[0].Verdict = plan.VerdictSkipped
	if Earned(in) {
		t.Fatal("skipped env")
	}
}

func TestEarnedRequiresConcurrencyWhenPlanned(t *testing.T) {
	t.Parallel()
	in := earnedInput()
	in.Plan.Steps = append(in.Plan.Steps, plan.Step{ID: "concurrency", Kind: plan.KindConcurrency, Required: true, N: 8})
	if Earned(in) {
		t.Fatal("missing concurrency result")
	}
	in.Result.Steps = append(in.Result.Steps, plan.StepResult{ID: "concurrency", Kind: plan.KindConcurrency, Verdict: replay.VerdictMatch})
	if !Earned(in) {
		t.Fatal("expected earned")
	}
}

func earnedInput() Input {
	lat := []replay.StepLatency{{
		Method:   "GET",
		Path:     "/healthz",
		Baseline: replay.Summary{N: 20, HasP95: true},
		Patch:    replay.Summary{N: 20, HasP95: true},
	}}
	return Input{
		BaselineSHA: "abc123",
		Baseline:    "http://127.0.0.1:18180",
		Patch:       "http://127.0.0.1:18280",
		Plan: plan.DAG{Steps: []plan.Step{
			{ID: "env", Kind: plan.KindEnv, Required: true},
			{ID: "behavior", Kind: plan.KindBehavior, Required: true},
			{ID: "latency", Kind: plan.KindLatency, Required: true, N: 20},
			{ID: "fault_status", Kind: plan.KindFaultStatus, Required: false, Operator: true},
		}},
		Result: plan.Evidence{
			Overall: replay.VerdictMatch,
			Steps: []plan.StepResult{
				{ID: "env", Kind: plan.KindEnv, Verdict: plan.VerdictPrepared},
				{ID: "behavior", Kind: plan.KindBehavior, Verdict: replay.VerdictMatch},
				{ID: "latency", Kind: plan.KindLatency, Verdict: plan.VerdictSamples, Latency: lat},
				{ID: "fault_status", Kind: plan.KindFaultStatus, Verdict: plan.VerdictSkipped},
			},
		},
	}
}

func TestEarnedBlockedByLatencyShift(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		base, patch int64
		want        bool
	}{
		{"chaos proxy +1500ms", 200_000, 1_502_000_000, false},
		{"2.5x but only +0.3ms", 200_000, 500_000, true},
		{"+6ms but only 1.5x", 12_000_000, 18_000_000, true},
		{"3x and +10ms", 5_000_000, 15_000_000, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := earnedInput()
			in.Result.Steps[2].Latency[0].Baseline.MedNS = tc.base
			in.Result.Steps[2].Latency[0].Patch.MedNS = tc.patch
			if got := Earned(in); got != tc.want {
				t.Fatalf("earned=%v want %v", got, tc.want)
			}
		})
	}
}

func TestEarnedRequiresImpactedRouteReached(t *testing.T) {
	t.Parallel()
	in := earnedInput()
	in.Impacted = []string{"GET /latency", "GET /users/{id}"}
	in.Workload = []replay.Step{{Method: "GET", Path: "/health"}}
	if Earned(in) {
		t.Fatal("earned with a workload that never touched an impacted route")
	}
	in.Workload = append(in.Workload, replay.Step{Method: "GET", Path: "/users/42?x=1"})
	if Earned(in) {
		t.Fatal("earned on a planned request that has no recorded reply")
	}
	in.Result.Steps[1].Steps = []replay.Delta{{Method: "GET", Path: "/users/42?x=1", BaselineStatus: 200, PatchStatus: 200}}
	if !Earned(in) {
		t.Fatal("templated impacted route was reached")
	}
	in.Result.Steps[1].Steps = []replay.Delta{{Method: "GET", Path: "/users/42?x=1", BaselineStatus: 401, PatchStatus: 401, Notes: []string{replay.NoteAuthRejected}}}
	if Earned(in) {
		t.Fatal("earned when the only impacted request was refused by auth")
	}
}

func TestEarnedNeedsRuntimeJoinForChangedCode(t *testing.T) {
	t.Parallel()
	in := earnedInput()
	in.Direct = 3
	if Earned(in) {
		t.Fatal("changed functions with no impacted route must not earn validated")
	}
	in.Direct = 0
	if !Earned(in) {
		t.Fatal("a diff with no changed functions has nothing to cover")
	}
}
