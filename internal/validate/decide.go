package validate

import (
	"strings"

	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
)

const minLatencyN = 20

// Input is the executed experiment pieces needed to decide validation.
type Input struct {
	Dirty       bool
	BaselineSHA string
	Baseline    string
	Patch       string
	Plan        plan.DAG
	Result      plan.Evidence
	// Impacted is the "METHOD /path" routes joined to the change at runtime.
	// When non-empty, some Workload step must reach one of them.
	Impacted []string
	Workload []replay.Step
	// Direct is how many functions the diff changed. Changed code with no
	// impacted route means the window never saw it run, so nothing was covered.
	Direct int
}

// Earned reports whether required executable experiments completed with
// overall match on a recorded clean revision. It is not a ship decision.
func Earned(in Input) bool {
	if in.Dirty || strings.TrimSpace(in.BaselineSHA) == "" {
		return false
	}
	if strings.TrimSpace(in.Baseline) == "" || strings.TrimSpace(in.Patch) == "" {
		return false
	}
	if strings.ToLower(strings.TrimSpace(in.Result.Overall)) != replay.VerdictMatch {
		return false
	}
	by := byKind(in.Result.Steps)
	for _, s := range in.Plan.Steps {
		if !s.Required || s.Operator {
			continue
		}
		got, ok := by[s.Kind]
		if !ok {
			return false
		}
		if !stepOK(s, got) {
			return false
		}
	}
	if _, ok := by[plan.KindEnv]; !ok {
		return false
	}
	if _, ok := by[plan.KindBehavior]; !ok {
		return false
	}
	if _, ok := by[plan.KindLatency]; !ok {
		return false
	}
	if len(in.Impacted) == 0 {
		return in.Direct == 0
	}
	return Covered(in.Impacted, in.Workload, in.Result)
}

// Covered reports whether some workload step hit an impacted route and both
// revisions answered it from a handler: a recorded status on each side that
// is not an auth refusal. A planned request that never got a reply is not
// coverage.
func Covered(impacted []string, work []replay.Step, res plan.Evidence) bool {
	answered := map[string]struct{}{}
	for _, s := range res.Steps {
		if s.Kind != plan.KindBehavior {
			continue
		}
		for _, d := range s.Steps {
			if d.BaselineStatus == 0 || d.PatchStatus == 0 || d.AuthRejected() {
				continue
			}
			answered[d.Method+" "+d.Path] = struct{}{}
		}
	}
	for _, st := range work {
		if _, ok := answered[st.Method+" "+st.Path]; !ok {
			continue
		}
		for _, route := range impacted {
			if impact.RouteMatches(route, st.Method, st.Path) {
				return true
			}
		}
	}
	return false
}

func byKind(steps []plan.StepResult) map[string]plan.StepResult {
	out := make(map[string]plan.StepResult, len(steps))
	for _, s := range steps {
		out[s.Kind] = s
	}
	return out
}

func stepOK(want plan.Step, got plan.StepResult) bool {
	v := strings.ToLower(strings.TrimSpace(got.Verdict))
	if v == "" || v == plan.VerdictSkipped || v == replay.VerdictIncomplete || v == "pass" || v == "validated" {
		return false
	}
	switch want.Kind {
	case plan.KindEnv:
		return v == plan.VerdictPrepared
	case plan.KindBehavior, plan.KindConcurrency:
		return v == replay.VerdictMatch
	case plan.KindTests:
		return v == plan.VerdictPrepared
	case plan.KindLatency:
		if v != plan.VerdictSamples {
			return false
		}
		n := want.N
		if n < 1 {
			n = plan.DefaultLatencyN
		}
		if n < minLatencyN {
			return false
		}
		for _, l := range got.Latency {
			if replay.Shifted(l) {
				return false
			}
		}
		return latencyN(got) >= minLatencyN
	default:
		return v == replay.VerdictMatch || v == plan.VerdictSamples || v == plan.VerdictPrepared
	}
}

func latencyN(got plan.StepResult) int {
	min := 0
	for i, lat := range got.Latency {
		n := lat.Baseline.N
		if lat.Patch.N < n {
			n = lat.Patch.N
		}
		if i == 0 || n < min {
			min = n
		}
	}
	return min
}
