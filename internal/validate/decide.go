package validate

import (
	"strings"

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
	return true
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
