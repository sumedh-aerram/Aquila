package plan

import (
	"context"

	"github.com/sumedhaerram/aquila/internal/replay"
)

// Evidence is executed and skipped DAG steps. Overall is never pass or validated.
type Evidence struct {
	Overall string       `json:"overall"`
	Steps   []StepResult `json:"steps"`
	Notes   []string     `json:"notes,omitempty"`
}

// StepResult is one DAG step after Execute.
type StepResult struct {
	ID      string               `json:"id"`
	Kind    string               `json:"kind"`
	Verdict string               `json:"verdict"`
	Notes   []string             `json:"notes,omitempty"`
	Latency []replay.StepLatency `json:"latency,omitempty"`
}

// Execute runs executable DAG steps against base and patch. Operator steps
// are skipped. It does not start Compose and does not inject one-sided 502
// as a patch verdict.
func Execute(ctx context.Context, dag DAG, base, patch string, w replay.Workload) (Evidence, error) {
	ev := Evidence{Notes: append([]string(nil), dag.Notes...)}
	n, needReplay := replayRepeats(dag)
	var baseRuns, patchRuns []replay.Result
	if needReplay {
		var err error
		baseRuns, err = replay.Repeat(ctx, base, w, n)
		if err != nil {
			return Evidence{}, err
		}
		patchRuns, err = replay.Repeat(ctx, patch, w, n)
		if err != nil {
			return Evidence{}, err
		}
	}
	for _, s := range dag.Steps {
		ev.Steps = append(ev.Steps, evalStep(s, baseRuns, patchRuns))
	}
	ev.Overall = overall(ev.Steps)
	return ev, nil
}

func evalStep(s Step, base, patch []replay.Result) StepResult {
	out := StepResult{ID: s.ID, Kind: s.Kind}
	if s.Operator {
		out.Verdict = VerdictSkipped
		out.Notes = []string{"operator"}
		return out
	}
	switch s.Kind {
	case KindBehavior:
		rep := compareFirst(base, patch)
		out.Verdict = rep.Verdict
		if len(rep.Steps) > 0 {
			var notes []string
			for _, d := range rep.Steps {
				notes = append(notes, d.Notes...)
			}
			out.Notes = notes
		}
	case KindLatency:
		if len(base) == 0 || len(patch) == 0 {
			out.Verdict = replay.VerdictIncomplete
			out.Notes = []string{"no samples"}
			return out
		}
		lat := replay.Latency(base, patch)
		out.Latency = lat
		if !hasSamples(lat) {
			out.Verdict = replay.VerdictIncomplete
			out.Notes = []string{"no successful samples"}
			return out
		}
		out.Verdict = VerdictSamples
	default:
		out.Verdict = replay.VerdictIncomplete
		out.Notes = []string{"unknown step"}
	}
	return out
}

func hasSamples(lat []replay.StepLatency) bool {
	for _, s := range lat {
		if s.Baseline.N > 0 || s.Patch.N > 0 {
			return true
		}
	}
	return false
}

func overall(steps []StepResult) string {
	incomplete := false
	differ := false
	executed := 0
	for _, s := range steps {
		if s.Verdict == VerdictSkipped {
			continue
		}
		executed++
		if s.Kind == KindLatency {
			if s.Verdict == replay.VerdictIncomplete {
				incomplete = true
			}
			continue
		}
		switch s.Verdict {
		case replay.VerdictIncomplete:
			incomplete = true
		case replay.VerdictDiffer:
			differ = true
		}
	}
	if executed == 0 {
		return replay.VerdictIncomplete
	}
	switch {
	case incomplete:
		return replay.VerdictIncomplete
	case differ:
		return replay.VerdictDiffer
	default:
		return replay.VerdictMatch
	}
}
