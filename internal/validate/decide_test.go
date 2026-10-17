package validate

import (
	"reflect"
	"testing"

	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
)

func TestDecideRequiresCleanMatchAndFullDAG(t *testing.T) {
	t.Parallel()
	in := decisionInput()
	if !Decide(in).Validated {
		t.Fatal("expected earned")
	}
	in.Dirty = true
	if Decide(in).Validated {
		t.Fatal("dirty")
	}
	assertBlocker(t, in, CodeDirtyBaseline, "")
}

func TestDecideRejectsDifferAndSmokeN(t *testing.T) {
	t.Parallel()
	in := decisionInput()
	in.Result.Overall = replay.VerdictDiffer
	in.Result.Steps[1].Verdict = replay.VerdictDiffer
	if Decide(in).Validated {
		t.Fatal("differ")
	}
	assertBlocker(t, in, CodeNotMatch, replay.VerdictDiffer)
	in = decisionInput()
	in.Plan.Steps[2].N = 1
	in.Result.Steps[2].Latency[0].Baseline.N = 1
	in.Result.Steps[2].Latency[0].Patch.N = 1
	if Decide(in).Validated {
		t.Fatal("n=1")
	}
	assertBlocker(t, in, CodeLatencyN, "1")
}

func TestDecideRejectsSkippedEnv(t *testing.T) {
	t.Parallel()
	in := decisionInput()
	in.Result.Steps[0].Verdict = plan.VerdictSkipped
	if Decide(in).Validated {
		t.Fatal("skipped env")
	}
	assertBlocker(t, in, CodeStepFailed, plan.KindEnv)
}

func TestDecideRequiresConcurrencyWhenPlanned(t *testing.T) {
	t.Parallel()
	in := decisionInput()
	in.Result.Steps = in.Result.Steps[:4]
	in.Plan.Steps = append(in.Plan.Steps, plan.Step{ID: "concurrency", Kind: plan.KindConcurrency, Required: true, N: 8})
	if Decide(in).Validated {
		t.Fatal("missing concurrency result")
	}
	assertBlocker(t, in, CodeStepMissing, plan.KindConcurrency)
	in.Result.Steps = append(in.Result.Steps, plan.StepResult{ID: "concurrency", Kind: plan.KindConcurrency, Verdict: replay.VerdictMatch})
	if !Decide(in).Validated {
		t.Fatal("expected earned")
	}
}

func decisionInput() Input {
	lat := []replay.StepLatency{{
		Method:   "GET",
		Path:     "/healthz",
		Baseline: replay.Summary{N: 20, HasP95: true},
		Patch:    replay.Summary{N: 20, HasP95: true},
	}}
	return Input{
		Changes:     impact.Changes{Bound: []string{"f"}},
		Impacted:    []string{"GET /healthz"},
		Workload:    []replay.Step{{Method: "GET", Path: "/healthz"}},
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
				{ID: "behavior", Kind: plan.KindBehavior, Verdict: replay.VerdictMatch, Steps: []replay.Delta{{Method: "GET", Path: "/healthz", BaselineStatus: 200, PatchStatus: 200}}},
				{ID: "latency", Kind: plan.KindLatency, Verdict: plan.VerdictSamples, Latency: lat},
				{ID: "fault_status", Kind: plan.KindFaultStatus, Verdict: plan.VerdictSkipped},
				{ID: "concurrency", Kind: plan.KindConcurrency, Verdict: replay.VerdictMatch},
			},
		},
	}
}

func TestDecideBlockedByLatencyShift(t *testing.T) {
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
			in := decisionInput()
			in.Result.Steps[2].Latency[0].Baseline.MedNS = tc.base
			in.Result.Steps[2].Latency[0].Patch.MedNS = tc.patch
			if !tc.want {
				assertBlocker(t, in, CodeLatencyShift, "GET /healthz")
			}
			if got := Decide(in).Validated; got != tc.want {
				t.Fatalf("earned=%v want %v", got, tc.want)
			}
		})
	}
}

func TestDecideRequiresImpactedRouteReached(t *testing.T) {
	t.Parallel()
	in := decisionInput()
	in.Impacted = []string{"GET /latency", "GET /users/{id}"}
	in.Workload = []replay.Step{{Method: "GET", Path: "/health"}}
	assertBlocker(t, in, CodeRouteMissed, "GET /users/{id}")
	if Decide(in).Validated {
		t.Fatal("earned with a workload that never touched an impacted route")
	}
	in.Workload = append(in.Workload, replay.Step{Method: "GET", Path: "/users/42?x=1"})
	if Decide(in).Validated {
		t.Fatal("earned on a planned request that has no recorded reply")
	}
	in.Result.Steps[1].Steps = []replay.Delta{{Method: "GET", Path: "/users/42?x=1", BaselineStatus: 200, PatchStatus: 200}}
	assertBlocker(t, in, CodeRouteMissed, "GET /latency")
	in.Result.Steps[1].Steps = append(in.Result.Steps[1].Steps, replay.Delta{Method: "GET", Path: "/latency", BaselineStatus: 200, PatchStatus: 200})
	in.Workload = append(in.Workload, replay.Step{Method: "GET", Path: "/latency"})
	if !Decide(in).Validated {
		t.Fatal("templated impacted routes were reached")
	}
	in.Result.Steps[1].Steps = []replay.Delta{{Method: "GET", Path: "/users/42?x=1", BaselineStatus: 401, PatchStatus: 401, Notes: []string{replay.NoteAuthRejected}}}
	if Decide(in).Validated {
		t.Fatal("earned when the only impacted request was refused by auth")
	}
}

func TestDecideNeedsRuntimeJoinForChangedCode(t *testing.T) {
	t.Parallel()
	in := decisionInput()
	in.Changes = impact.Changes{Bound: []string{"a", "b", "c"}}
	in.Impacted = nil
	if Decide(in).Validated {
		t.Fatal("changed functions with no impacted route must not earn validated")
	}
	assertBlocker(t, in, CodeNoRuntimeRoute, "")
	in.Changes = impact.Changes{}
	if Decide(in).Validated {
		t.Fatal("a diff with no changed functions must block")
	}
	assertBlocker(t, in, CodeNoFunctionBinding, "")
}

func assertBlocker(t *testing.T, in Input, code, subject string) {
	t.Helper()
	for _, b := range Decide(in).Blockers {
		if b.Code == code && b.Subject == subject {
			return
		}
	}
	t.Fatalf("missing %s:%s in %+v", code, subject, Decide(in))
}

func TestDecideGradesLatencyWhenNotRequired(t *testing.T) {
	t.Parallel()
	in := decisionInput()
	in.Plan.Steps[2].Required = false
	in.Result.Steps[2].Latency[0].Baseline.MedNS = 5_000_000
	in.Result.Steps[2].Latency[0].Patch.MedNS = 15_000_000
	assertBlocker(t, in, CodeLatencyShift, "GET /healthz")
}

func TestDecideLatencyNIgnoresPlanN(t *testing.T) {
	t.Parallel()
	in := decisionInput()
	in.Plan.Steps[2].N = 50
	in.Result.Steps[2].Latency[0].Baseline.N = 5
	assertBlocker(t, in, CodeLatencyN, "5")
}

func TestDecideDuplicateKinds(t *testing.T) {
	t.Parallel()
	in := decisionInput()
	clean := in.Result.Steps[2]
	clean.Latency = append([]replay.StepLatency(nil), clean.Latency...)
	in.Result.Steps[2].Latency[0].Baseline.MedNS = 5_000_000
	in.Result.Steps[2].Latency[0].Patch.MedNS = 15_000_000
	in.Result.Steps = append(in.Result.Steps, clean)
	assertBlocker(t, in, CodeDuplicateStep, plan.KindLatency)
	assertBlocker(t, in, CodeLatencyShift, "GET /healthz")
}

func TestDecideSameTarget(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, base, patch, subject string }{
		{"trailing slash", "http://127.0.0.1:1/", "http://127.0.0.1:1", "http://127.0.0.1:1"},
		{"localhost", "http://localhost:1", "http://127.0.0.1:1", "http://127.0.0.1:1"},
		{"ipv6 loopback", "http://[::1]:1", "http://localhost:1", "http://127.0.0.1:1"},
		{"default http port", "http://localhost:80/", "http://127.0.0.1", "http://127.0.0.1"},
		{"default https port", "https://[::1]:443/", "https://localhost", "https://127.0.0.1"},
		{"scheme and host case", "HTTP://LOCALHOST:80/", "http://127.0.0.1", "http://127.0.0.1"},
		{"remote default port", "https://EXAMPLE.com:443/a/", "https://example.com/a", "https://example.com/a"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := decisionInput()
			in.Baseline = tc.base
			in.Patch = tc.patch
			assertBlocker(t, in, CodeSameTarget, tc.subject)
		})
	}
	for _, tc := range []struct{ name, base, patch string }{
		{"different ports", "http://localhost:1", "http://127.0.0.1:2"},
		{"different schemes", "http://localhost", "https://127.0.0.1"},
		{"different paths", "http://localhost/a", "http://[::1]/b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := decisionInput()
			in.Baseline = tc.base
			in.Patch = tc.patch
			if got := Decide(in); !got.Validated {
				t.Fatalf("distinct targets blocked: %+v", got)
			}
		})
	}
}

func TestDecideSkippedCoreStepNotRequired(t *testing.T) {
	t.Parallel()
	in := decisionInput()
	in.Plan.Steps[0].Operator = true
	in.Plan.Steps[0].Required = false
	in.Result.Steps[0].Verdict = plan.VerdictSkipped
	assertBlocker(t, in, CodeStepFailed, plan.KindEnv)
}

func TestDecideUnanalyzedNonGo(t *testing.T) {
	t.Parallel()
	in := decisionInput()
	in.Changes = impact.Changes{Unanalyzed: []string{"Dockerfile"}}
	in.Impacted = nil
	assertBlocker(t, in, CodeUnanalyzedChange, "Dockerfile")
	assertBlocker(t, in, CodeNoFunctionBinding, "")
}

func TestDecideAllRoutesRequired(t *testing.T) {
	t.Parallel()
	in := decisionInput()
	in.Impacted = []string{"GET /a", "GET /b"}
	in.Workload = []replay.Step{{Method: "GET", Path: "/a"}}
	in.Result.Steps[1].Steps = []replay.Delta{{Method: "GET", Path: "/a", BaselineStatus: 200, PatchStatus: 200}}
	assertBlocker(t, in, CodeRouteMissed, "GET /b")
}

func TestDecideReportsAllBlockers(t *testing.T) {
	t.Parallel()
	in := Input{
		Dirty:    true,
		Result:   plan.Evidence{Overall: replay.VerdictDiffer},
		Changes:  impact.Changes{Unanalyzed: []string{"z", "a"}},
		Impacted: []string{"GET /z", "GET /a"},
	}
	want := []Blocker{
		{Code: CodeDirtyBaseline},
		{Code: CodeUnverifiedRevision, Subject: "baseline"},
		{Code: CodeMissingTarget, Subject: "baseline"},
		{Code: CodeMissingTarget, Subject: "patch"},
		{Code: CodeNotMatch, Subject: "differ"},
		{Code: CodeStepMissing, Subject: "behavior"},
		{Code: CodeStepMissing, Subject: "concurrency"},
		{Code: CodeStepMissing, Subject: "env"},
		{Code: CodeStepMissing, Subject: "latency"},
		{Code: CodeNoFunctionBinding},
		{Code: CodeUnanalyzedChange, Subject: "a"},
		{Code: CodeUnanalyzedChange, Subject: "z"},
		{Code: CodeRouteMissed, Subject: "GET /a"},
		{Code: CodeRouteMissed, Subject: "GET /z"},
	}
	if got := Decide(in); got.Validated || !reflect.DeepEqual(got.Blockers, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
