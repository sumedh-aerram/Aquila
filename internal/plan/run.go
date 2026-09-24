package plan

import (
	"context"
	"fmt"
	"strings"

	"github.com/sumedhaerram/aquila/internal/replay"
)

// routePath drops the query so notes stay short and do not echo query values.
func routePath(p string) string {
	if i := strings.IndexByte(p, '?'); i >= 0 {
		return p[:i]
	}
	return p
}

// Evidence is executed and skipped DAG steps. Overall is match, differ, or
// incomplete — never pass.
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
	Steps   []replay.Delta       `json:"steps,omitempty"`
}

type execData struct {
	envErr     error
	base       []replay.Result
	patch      []replay.Result
	baseBurst  []replay.Result
	patchBurst []replay.Result
	tests      StepResult
	fault      map[string]StepResult
	workload   replay.Workload
	baseURL    string
	patchURL   string
}

// Execute runs executable DAG steps against base and patch. Operator steps
// are skipped. Env probes GET dag.HealthPath (default /healthz). Fault is a one-sided inject probe
// and does not vote overall.
func Execute(ctx context.Context, dag DAG, base, patch string, w replay.Workload) (Evidence, error) {
	if err := replay.CheckTarget(base); err != nil {
		return Evidence{}, err
	}
	if err := replay.CheckTarget(patch); err != nil {
		return Evidence{}, err
	}
	ev := Evidence{Notes: append([]string(nil), dag.Notes...)}
	n, needReplay := replayRepeats(dag)
	cn, needBurst := concurrencyN(dag)
	if (needReplay || needBurst) && len(w.Steps) == 0 {
		return Evidence{}, fmt.Errorf("plan: empty workload")
	}
	data := execData{workload: w, baseURL: base, patchURL: patch, fault: map[string]StepResult{}}
	data.envErr = probeEnv(ctx, dag, base, patch)
	if data.envErr == nil && needReplay {
		var err error
		data.base, err = replay.Repeat(ctx, base, w, n)
		if err != nil {
			return Evidence{}, err
		}
		data.patch, err = replay.Repeat(ctx, patch, w, n)
		if err != nil {
			return Evidence{}, err
		}
	}
	if data.envErr == nil && needBurst {
		var err error
		data.baseBurst, err = replay.Burst(ctx, base, w, cn)
		if err != nil {
			return Evidence{}, err
		}
		data.patchBurst, err = replay.Burst(ctx, patch, w, cn)
		if err != nil {
			return Evidence{}, err
		}
	}
	if hasKind(dag, KindTests) {
		data.tests = runTests(ctx, dag)
	}
	for _, s := range dag.Steps {
		if s.Kind == KindFaultStatus && !s.Operator {
			data.fault[s.ID] = probeFault(ctx, s, base, patch, w)
		}
	}
	for _, s := range dag.Steps {
		ev.Steps = append(ev.Steps, evalStep(s, data))
	}
	ev.Overall = overall(ev.Steps)
	return ev, nil
}

func probeEnv(ctx context.Context, dag DAG, base, patch string) error {
	for _, s := range dag.Steps {
		if s.Kind == KindEnv && !s.Operator {
			return replay.HealthzPath(ctx, base, patch, dag.HealthPath)
		}
	}
	return nil
}

func evalStep(s Step, data execData) StepResult {
	out := StepResult{ID: s.ID, Kind: s.Kind}
	if s.Kind == KindEnv && !s.Operator {
		if data.envErr != nil {
			out.Verdict = replay.VerdictIncomplete
			out.Notes = []string{data.envErr.Error()}
			return out
		}
		out.Verdict = VerdictPrepared
		out.Notes = []string{"healthz"}
		return out
	}
	if s.Operator {
		out.Verdict = VerdictSkipped
		out.Notes = []string{"operator"}
		return out
	}
	switch s.Kind {
	case KindBehavior:
		rep := compareFirst(data.base, data.patch)
		out.Verdict = rep.Verdict
		out.Steps = rep.Steps
		for _, d := range rep.Steps {
			if len(d.Notes) == 0 {
				continue
			}
			note := fmt.Sprintf("%s %s %d/%d %s", d.Method, routePath(d.Path), d.BaselineStatus, d.PatchStatus, strings.Join(d.Notes, ","))
			if d.AuthRejected() {
				note += ": handler did not run; check workload headers"
			}
			out.Notes = append(out.Notes, note)
		}
	case KindLatency:
		if len(data.base) == 0 || len(data.patch) == 0 {
			out.Verdict = replay.VerdictIncomplete
			out.Notes = []string{"no samples"}
			return out
		}
		lat := replay.Latency(data.base, data.patch)
		out.Latency = lat
		if !hasSamples(lat) {
			out.Verdict = replay.VerdictIncomplete
			out.Notes = []string{"no successful samples"}
			return out
		}
		out.Verdict = VerdictSamples
		for _, l := range lat {
			if replay.Shifted(l) {
				out.Notes = append(out.Notes, fmt.Sprintf("latency_shift %s %s median %.1fms -> %.1fms (> %dx and >= %dms slower); blocks validated",
					l.Method, routePath(l.Path), float64(l.Baseline.MedNS)/1e6, float64(l.Patch.MedNS)/1e6, replay.ShiftRatio, replay.ShiftMinNS/1e6))
			}
		}
	case KindConcurrency:
		got := compareBurst(data.baseBurst, data.patchBurst)
		got.ID = s.ID
		return got
	case KindTests:
		got := data.tests
		got.ID = s.ID
		got.Kind = KindTests
		return got
	case KindFaultStatus:
		if got, ok := data.fault[s.ID]; ok {
			return got
		}
		out.Verdict = replay.VerdictIncomplete
		out.Notes = []string{"fault not executed"}
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
		if s.Kind == KindEnv && s.Verdict == replay.VerdictIncomplete {
			return replay.VerdictIncomplete
		}
		if s.Verdict == VerdictSkipped {
			continue
		}
		if s.Kind == KindEnv || s.Kind == KindFaultStatus {
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

// Overall is the DAG verdict from executed steps. It is never pass.
func Overall(steps []StepResult) string {
	return overall(steps)
}
