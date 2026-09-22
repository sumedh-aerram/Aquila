package plan

import (
	"fmt"

	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/replay"
)

const (
	KindEnv         = "env"
	KindBehavior    = "behavior"
	KindLatency     = "latency"
	KindFaultStatus = "fault_status"

	DefaultLatencyN = 20
)

const (
	VerdictSkipped = "skipped"
	VerdictSamples = "samples"
)

// Step is one node in the minimum useful experiment DAG.
type Step struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Required  bool     `json:"required"`
	Operator  bool     `json:"operator,omitempty"`
	N         int      `json:"n,omitempty"`
	Status    int      `json:"status,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"`
	Reason    string   `json:"reason"`
}

// DAG is the smallest experiment set Aquila can currently execute or name.
type DAG struct {
	Steps []Step   `json:"steps"`
	Notes []string `json:"notes,omitempty"`
}

// FromImpact builds a DAG from impact findings. It does not invent tests,
// concurrency, or a pass threshold.
func FromImpact(rep impact.Report) (DAG, error) {
	if len(rep.Files) == 0 && len(rep.Direct) == 0 && len(rep.Likely) == 0 && len(rep.Runtime) == 0 && len(rep.Unobserved) == 0 {
		return DAG{}, fmt.Errorf("plan: empty impact")
	}
	dag := DAG{
		Steps: []Step{
			{
				ID:       "env",
				Kind:     KindEnv,
				Required: true,
				Operator: true,
				Reason:   "isolated baseline and patch trees; compose is not started",
			},
			{
				ID:        "behavior",
				Kind:      KindBehavior,
				Required:  true,
				DependsOn: []string{"env"},
				Reason:    "first-run status and json compare",
			},
			{
				ID:        "latency",
				Kind:      KindLatency,
				Required:  true,
				N:         DefaultLatencyN,
				DependsOn: []string{"env"},
				Reason:    "successful-sample median; p95 withheld below 20; no regression threshold",
			},
		},
		Notes: []string{
			"not validated. no pass threshold. no impacted tests.",
		},
	}
	if hasRuntime(rep) {
		dag.Steps = append(dag.Steps, Step{
			ID:        "fault_status",
			Kind:      KindFaultStatus,
			Required:  true,
			Operator:  true,
			Status:    502,
			DependsOn: []string{"env"},
			Reason:    "same 502 spec on both revisions is tautological with ingress inject; one-sided 502 is a probe via aquila fault, not a patch verdict",
		})
		dag.Notes = append(dag.Notes, "fault is operator; equivalent dependency inject is not available without a mesh")
	} else {
		dag.Notes = append(dag.Notes, "fault omitted: no observed runtime path")
	}
	return dag, nil
}

// WithLatencyN returns a copy with latency repeats set to n when n >= 1.
func WithLatencyN(dag DAG, n int) DAG {
	if n < 1 {
		return dag
	}
	steps := append([]Step(nil), dag.Steps...)
	for i := range steps {
		if steps[i].Kind == KindLatency {
			steps[i].N = n
		}
	}
	dag.Steps = steps
	return dag
}

func hasRuntime(rep impact.Report) bool {
	return len(rep.Runtime) > 0
}

func replayRepeats(dag DAG) (n int, need bool) {
	n = 1
	for _, s := range dag.Steps {
		if s.Operator {
			continue
		}
		switch s.Kind {
		case KindBehavior:
			need = true
		case KindLatency:
			need = true
			if s.N > n {
				n = s.N
			}
		}
	}
	return n, need
}

func compareFirst(base, patch []replay.Result) replay.Report {
	if len(base) == 0 || len(patch) == 0 {
		return replay.Report{Verdict: replay.VerdictIncomplete}
	}
	return replay.Compare(base[0], patch[0])
}
